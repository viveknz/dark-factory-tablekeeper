package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"time"

	"tablekeeper/internal/idgen"
	"tablekeeper/internal/store"
)

type reservationResponse struct {
	ReservationID string   `json:"reservation_id"`
	Reference     string   `json:"reference"`
	RestaurantID  string   `json:"restaurant_id"`
	TableIDs      []string `json:"table_ids"`
	TableID       string   `json:"table_id,omitempty"`
	PartySize     int      `json:"party_size"`
	Status        string   `json:"status"`
	StartsAtLocal string   `json:"starts_at_local"`
	StartsAt      string   `json:"starts_at"`
	EndsAt        string   `json:"ends_at"`
	CreatedAt     string   `json:"created_at"`
}

func toResponse(r *store.Reservation) reservationResponse {
	resp := reservationResponse{
		ReservationID: r.ID,
		Reference:     r.Reference,
		RestaurantID:  r.RestaurantID,
		TableIDs:      r.TableIDs,
		PartySize:     r.PartySize,
		Status:        r.Status,
		StartsAtLocal: r.StartsAtLocal,
		StartsAt:      formatRFC3339(r.StartsAt),
		EndsAt:        formatRFC3339(r.EndsAt),
		CreatedAt:     formatRFC3339(r.CreatedAt),
	}
	if len(r.TableIDs) == 1 {
		resp.TableID = r.TableIDs[0]
	}
	return resp
}

func canonicalBody(raw map[string]json.RawMessage) string {
	b, _ := json.Marshal(raw)
	return string(b)
}

func uniqueReference(s *store.Store) string {
	for {
		ref := idgen.Reference()
		if !s.ReferenceTaken(ref) {
			return ref
		}
	}
}

func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// requiredReservationFields reads restaurant_id, table_id/table_ids, starts_at_local and
// party_size, all required, per POST /reservations.
func requiredReservationFields(raw map[string]json.RawMessage) (restaurantID string, tableIDs []string, startsAtLocal string, partySize int, apiErr *apiError) {
	restaurantID, presentR, e := fieldString(raw, "restaurant_id")
	if e != nil {
		return "", nil, "", 0, e
	}
	tableIDs, presentT, e2 := extractTableIDs(raw)
	if e2 != nil {
		return "", nil, "", 0, e2
	}
	startsAtLocal, presentS, e3 := fieldStartsAtLocal(raw, "starts_at_local")
	if e3 != nil {
		return "", nil, "", 0, e3
	}
	partySize, presentP, e4 := fieldPartySize(raw, "party_size")
	if e4 != nil {
		return "", nil, "", 0, e4
	}
	if !presentR || restaurantID == "" {
		return "", nil, "", 0, errValidationFailed("restaurant_id is required")
	}
	if !presentT || len(tableIDs) == 0 {
		return "", nil, "", 0, errValidationFailed("table_id or table_ids is required")
	}
	if !presentS {
		return "", nil, "", 0, errValidationFailed("starts_at_local is required")
	}
	if !presentP {
		return "", nil, "", 0, errValidationFailed("party_size is required")
	}
	return restaurantID, tableIDs, startsAtLocal, partySize, nil
}

func (a *API) handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	raw, berr := decodeBody(body)
	if berr != nil {
		writeError(w, berr)
		return
	}

	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, aerr)
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, errMissingIdempotency)
		return
	}
	if len(key) > 255 {
		writeError(w, errValidationFailed("Idempotency-Key must be at most 255 characters"))
		return
	}
	canonical := canonicalBody(raw)

	a.Store.Lock()
	defer a.Store.Unlock()

	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, "/reservations", key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	restaurantID, tableIDs, startsAtLocal, partySize, ferr := requiredReservationFields(raw)
	if ferr != nil {
		writeError(w, ferr)
		return
	}

	_, tables, startsAt, endsAt, verr := a.validateBooking(restaurantID, tableIDs, startsAtLocal, partySize, "")
	if verr != nil {
		writeError(w, verr)
		return
	}

	resv := &store.Reservation{
		ID:            idgen.New("res"),
		Reference:     uniqueReference(a.Store),
		RestaurantID:  restaurantID,
		TableIDs:      tableIDsOf(tables),
		UserID:        user.ID,
		PartySize:     partySize,
		Status:        "confirmed",
		StartsAtLocal: startsAtLocal,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		CreatedAt:     time.Now().UTC(),
	}
	a.Store.AddReservation(resv)

	resp := toResponse(resv)
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, "/reservations", key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleListReservations(w http.ResponseWriter, r *http.Request) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	a.Store.Lock()
	defer a.Store.Unlock()

	list := a.Store.ReservationsByUser(user.ID)
	sort.Slice(list, func(i, j int) bool { return list[i].StartsAt.After(list[j].StartsAt) })
	out := make([]reservationResponse, 0, len(list))
	for _, r := range list {
		out = append(out, toResponse(r))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"reservations": out})
}

func (a *API) handleGetReservation(w http.ResponseWriter, r *http.Request) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	ref := r.PathValue("reference")
	a.Store.Lock()
	defer a.Store.Unlock()

	resv, ok := a.Store.ReservationByReference(ref)
	if !ok || resv.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(resv))
}

func (a *API) handleCancelReservation(w http.ResponseWriter, r *http.Request) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	ref := r.PathValue("reference")
	a.Store.Lock()
	defer a.Store.Unlock()

	resv, ok := a.Store.ReservationByReference(ref)
	if !ok || resv.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	if resv.Status == "cancelled" {
		writeJSON(w, http.StatusOK, toResponse(resv))
		return
	}

	rest := a.Store.Restaurants[resv.RestaurantID]
	cutoff := time.Duration(rest.CancellationCutoffMinutes) * time.Minute
	if !time.Now().UTC().Before(resv.StartsAt.Add(-cutoff)) {
		writeError(w, errCutoffPassed)
		return
	}

	resv.Status = "cancelled"
	writeJSON(w, http.StatusOK, toResponse(resv))
}

func (a *API) handleAmendReservation(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	raw, berr := decodeBody(body)
	if berr != nil {
		writeError(w, berr)
		return
	}
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	ref := r.PathValue("reference")

	a.Store.Lock()
	defer a.Store.Unlock()

	resv, ok := a.Store.ReservationByReference(ref)
	if !ok || resv.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	if resv.Status == "cancelled" {
		writeError(w, errReservationCancelled)
		return
	}

	rest := a.Store.Restaurants[resv.RestaurantID]
	cutoff := time.Duration(rest.CancellationCutoffMinutes) * time.Minute
	if !time.Now().UTC().Before(resv.StartsAt.Add(-cutoff)) {
		writeError(w, errCutoffPassed)
		return
	}

	tableIDs, presentT, e := extractTableIDs(raw)
	if e != nil {
		writeError(w, e)
		return
	}
	startsAtLocal, presentS, e2 := fieldStartsAtLocal(raw, "starts_at_local")
	if e2 != nil {
		writeError(w, e2)
		return
	}
	partySize, presentP, e3 := fieldPartySize(raw, "party_size")
	if e3 != nil {
		writeError(w, e3)
		return
	}
	if !presentT {
		tableIDs = resv.TableIDs
	}
	if !presentS {
		startsAtLocal = resv.StartsAtLocal
	}
	if !presentP {
		partySize = resv.PartySize
	}

	_, tables, startsAt, endsAt, verr := a.validateBooking(resv.RestaurantID, tableIDs, startsAtLocal, partySize, resv.ID)
	if verr != nil {
		writeError(w, verr)
		return
	}

	resv.TableIDs = tableIDsOf(tables)
	resv.StartsAtLocal = startsAtLocal
	resv.StartsAt = startsAt
	resv.EndsAt = endsAt
	resv.PartySize = partySize

	writeJSON(w, http.StatusOK, toResponse(resv))
}
