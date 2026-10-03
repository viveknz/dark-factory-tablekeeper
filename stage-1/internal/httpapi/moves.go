package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"tablekeeper/internal/store"
)

type resolvedMove struct {
	resv          *store.Reservation
	tableID       string
	startsAtLocal string
	partySize     int
	startsAt      time.Time
	endsAt        time.Time
}

func overlaps(aStart, aEnd, bStart, bEnd time.Time) bool {
	return aStart.Before(bEnd) && bStart.Before(aEnd)
}

func (a *API) handleReservationMoves(w http.ResponseWriter, r *http.Request) {
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

	const path = "/reservation-moves"
	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, path, key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	movesRawVal, exists := raw["moves"]
	if !exists {
		writeError(w, errValidationFailed("moves is required"))
		return
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(movesRawVal, &items); err != nil {
		writeError(w, errValidationFailed("moves must be an array of objects"))
		return
	}
	if len(items) < 1 || len(items) > 8 {
		writeError(w, errValidationFailed("moves must contain 1 to 8 items"))
		return
	}
	references := make([]string, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		ref, present, e := fieldString(item, "reference")
		if e != nil || !present || ref == "" {
			writeError(w, errValidationFailed("each move requires a non-empty string reference"))
			return
		}
		if seen[ref] {
			writeError(w, errValidationFailed("duplicate reference in moves"))
			return
		}
		seen[ref] = true
		references[i] = ref
	}

	resolved := make([]resolvedMove, len(items))
	var batchRestaurantID string
	for i, item := range items {
		resv, ok := a.Store.ReservationByReference(references[i])
		if !ok || resv.UserID != user.ID {
			writeError(w, errNotFound)
			return
		}
		if i == 0 {
			batchRestaurantID = resv.RestaurantID
		} else if resv.RestaurantID != batchRestaurantID {
			writeError(w, errValidationFailed("all moves must be bookings at the same restaurant"))
			return
		}
		if resv.Status == "cancelled" {
			writeError(w, errReservationCancelled)
			return
		}
		rest := a.Store.Restaurants[resv.RestaurantID]
		cutoff := minutesToDuration(rest.CancellationCutoffMinutes)
		if !nowUTC().Before(resv.StartsAt.Add(-cutoff)) {
			writeError(w, errCutoffPassed)
			return
		}

		tableID, presentT, e := fieldString(item, "table_id")
		if e != nil {
			writeError(w, e)
			return
		}
		startsAtLocal, presentS, e2 := fieldStartsAtLocal(item, "starts_at_local")
		if e2 != nil {
			writeError(w, e2)
			return
		}
		partySize, presentP, e3 := fieldPartySize(item, "party_size")
		if e3 != nil {
			writeError(w, e3)
			return
		}
		if !presentT {
			tableID = resv.TableID
		}
		if !presentS {
			startsAtLocal = resv.StartsAtLocal
		}
		if !presentP {
			partySize = resv.PartySize
		}

		_, tbl, startsAt, endsAt, verr := a.resolveBookingFields(resv.RestaurantID, tableID, startsAtLocal, partySize)
		if verr != nil {
			writeError(w, verr)
			return
		}

		resolved[i] = resolvedMove{
			resv:          resv,
			tableID:       tbl.ID,
			startsAtLocal: startsAtLocal,
			partySize:     partySize,
			startsAt:      startsAt,
			endsAt:        endsAt,
		}
	}

	listedIDs := map[string]bool{}
	for _, rm := range resolved {
		listedIDs[rm.resv.ID] = true
	}
	// Overlap among the resulting bookings themselves.
	for i := 0; i < len(resolved); i++ {
		for j := i + 1; j < len(resolved); j++ {
			if resolved[i].tableID != resolved[j].tableID {
				continue
			}
			if overlaps(resolved[i].startsAt, resolved[i].endsAt, resolved[j].startsAt, resolved[j].endsAt) {
				writeError(w, errTableUnavailable)
				return
			}
		}
	}
	// Overlap with any unlisted confirmed reservation.
	for _, rm := range resolved {
		for _, other := range a.Store.Reservations {
			if listedIDs[other.ID] || other.Status != "confirmed" {
				continue
			}
			if other.RestaurantID != batchRestaurantID || other.TableID != rm.tableID {
				continue
			}
			if other.Overlaps(rm.startsAt, rm.endsAt) {
				writeError(w, errTableUnavailable)
				return
			}
		}
	}

	// All validated: commit atomically.
	for _, rm := range resolved {
		rm.resv.TableID = rm.tableID
		rm.resv.StartsAtLocal = rm.startsAtLocal
		rm.resv.StartsAt = rm.startsAt
		rm.resv.EndsAt = rm.endsAt
		rm.resv.PartySize = rm.partySize
	}

	out := make([]reservationResponse, len(resolved))
	for i, rm := range resolved {
		out[i] = toResponse(rm.resv)
	}
	resp := map[string]interface{}{"reservations": out}
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, path, key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}
