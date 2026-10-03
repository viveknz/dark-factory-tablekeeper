package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"tablekeeper/internal/idgen"
	"tablekeeper/internal/store"
)

const maxSeriesCount = 12
const minSeriesCount = 2
const maxIntervalWeeks = 4
const minIntervalWeeks = 1

type seriesOccurrence struct {
	Index       int                 `json:"index"`
	Reference   string              `json:"reference"`
	Exception   bool                `json:"exception"`
	Reservation reservationResponse `json:"reservation"`
}

type seriesResponse struct {
	SeriesID      string             `json:"series_id"`
	Revision      int                `json:"revision"`
	IntervalWeeks int                `json:"interval_weeks"`
	Occurrences   []seriesOccurrence `json:"occurrences"`
}

func toSeriesResponse(s *store.Store, sr *store.Series) seriesResponse {
	out := seriesResponse{SeriesID: sr.ID, Revision: sr.Revision, IntervalWeeks: sr.IntervalWeeks}
	for i, resID := range sr.ReservationIDs {
		resv := s.Reservations[resID]
		out.Occurrences = append(out.Occurrences, seriesOccurrence{
			Index:       i,
			Reference:   resv.Reference,
			Exception:   resv.IsException,
			Reservation: toResponse(resv),
		})
	}
	return out
}

// onRealAmendment marks resv's series occurrence as a permanent exception and bumps that
// series' revision once, if resv belongs to a series. Called only after an actual (non-no-op)
// change has already been applied to resv.
func (a *API) onRealAmendment(resv *store.Reservation) {
	if resv.SeriesID == "" {
		return
	}
	if sr, ok := a.Store.SeriesByID(resv.SeriesID); ok {
		resv.IsException = true
		sr.Revision++
	}
}

// onCancelledOccurrence bumps resv's series revision once, if resv belongs to a series,
// without marking it an exception (cancellation is not an "exception" per spec). Called only
// after resv has actually just transitioned to cancelled.
func (a *API) onCancelledOccurrence(resv *store.Reservation) {
	if resv.SeriesID == "" {
		return
	}
	if sr, ok := a.Store.SeriesByID(resv.SeriesID); ok {
		sr.Revision++
	}
}

func (a *API) handleCreateSeries(w http.ResponseWriter, r *http.Request) {
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

	const path = "/series"
	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, path, key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	anchorRef, presentA, ea := fieldString(raw, "anchor_reference")
	if ea != nil {
		writeError(w, ea)
		return
	}
	if !presentA || anchorRef == "" {
		writeError(w, errValidationFailed("anchor_reference is required"))
		return
	}
	count, presentC, ec := fieldStrictInt(raw, "count", minSeriesCount, maxSeriesCount)
	if ec != nil {
		writeError(w, ec)
		return
	}
	if !presentC {
		writeError(w, errValidationFailed("count is required"))
		return
	}
	intervalWeeks, presentI, ei := fieldStrictInt(raw, "interval_weeks", minIntervalWeeks, maxIntervalWeeks)
	if ei != nil {
		writeError(w, ei)
		return
	}
	if !presentI {
		writeError(w, errValidationFailed("interval_weeks is required"))
		return
	}

	anchor, ok := a.Store.ReservationByReference(anchorRef)
	if !ok || anchor.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	if anchor.Status == "cancelled" {
		writeError(w, errReservationCancelled)
		return
	}
	if anchor.SeriesID != "" {
		writeError(w, errAlreadyInSeries)
		return
	}
	cutoff := time.Duration(anchor.AcceptedTerms.CancellationCutoffMinutes) * time.Minute
	if !time.Now().UTC().Before(anchor.StartsAt.Add(-cutoff)) {
		writeError(w, errCutoffPassed)
		return
	}

	rest := a.Store.Restaurants[anchor.RestaurantID]
	loc, lerr := time.LoadLocation(rest.Timezone)
	if lerr != nil {
		writeError(w, errValidationFailed("restaurant has an invalid timezone"))
		return
	}
	anchorY, anchorM, anchorD, anchorH, anchorMi, _ := splitLocal(anchor.StartsAtLocal)

	// Resolve every occurrence (1..count-1) first, all-or-nothing: the first failing
	// occurrence in index order determines the single error returned, and nothing is created
	// or claimed until every occurrence has resolved successfully.
	type resolved struct {
		startsAtLocal string
		tables        []*store.Table
		startsAt      time.Time
		endsAt        time.Time
		terms         store.AcceptedTerms
	}
	occurrences := make([]resolved, count)
	occurrences[0] = resolved{startsAtLocal: anchor.StartsAtLocal, tables: nil, startsAt: anchor.StartsAt, endsAt: anchor.EndsAt, terms: anchor.AcceptedTerms}

	for i := 1; i < count; i++ {
		days := i * intervalWeeks * 7
		occDate := time.Date(anchorY, time.Month(anchorM), anchorD, 0, 0, 0, 0, loc).AddDate(0, 0, days)
		localStr := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d", occDate.Year(), int(occDate.Month()), occDate.Day(), anchorH, anchorMi)

		_, tables, startsAt, endsAt, terms, verr := a.resolveBookingFields(anchor.RestaurantID, anchor.TableIDs, localStr, anchor.PartySize)
		if verr != nil {
			// A nonexistent local time (spring-forward) for any occurrence rejects the whole
			// adoption as invalid_local_time; resolveBookingFields already returns exactly
			// that error for this case, so forwarding verr as-is satisfies spec here too.
			writeError(w, verr)
			return
		}
		for _, t := range tables {
			if !a.Store.TableAvailable(anchor.RestaurantID, t.ID, startsAt, endsAt, "") {
				writeError(w, errTableUnavailable)
				return
			}
		}
		occurrences[i] = resolved{startsAtLocal: localStr, tables: tables, startsAt: startsAt, endsAt: endsAt, terms: terms}
	}
	// Overlap among the newly-generated occurrences themselves (distinct dates make this rare,
	// but interval_weeks=1 with a short duration and a table reused elsewhere in the batch is
	// still possible in principle at the boundaries).
	for i := 1; i < count; i++ {
		for j := i + 1; j < count; j++ {
			if overlaps(occurrences[i].startsAt, occurrences[i].endsAt, occurrences[j].startsAt, occurrences[j].endsAt) {
				writeError(w, errTableUnavailable)
				return
			}
		}
	}

	seriesID := idgen.New("ser")
	now := time.Now().UTC()
	sr := &store.Series{ID: seriesID, UserID: user.ID, RestaurantID: anchor.RestaurantID, Revision: 1, IntervalWeeks: intervalWeeks}
	sr.ReservationIDs = append(sr.ReservationIDs, anchor.ID)
	anchor.SeriesID = seriesID
	anchor.SeriesIndex = 0

	for i := 1; i < count; i++ {
		occ := occurrences[i]
		resv := &store.Reservation{
			ID:            idgen.New("res"),
			Reference:     uniqueReference(a.Store),
			RestaurantID:  anchor.RestaurantID,
			TableIDs:      tableIDsOf(occ.tables),
			UserID:        user.ID,
			PartySize:     anchor.PartySize,
			Status:        "confirmed",
			StartsAtLocal: occ.startsAtLocal,
			StartsAt:      occ.startsAt,
			EndsAt:        occ.endsAt,
			CreatedAt:     now,
			Revision:      1,
			AcceptedTerms: occ.terms,
			SeriesID:      seriesID,
			SeriesIndex:   i,
		}
		resv.RecordCreated(now)
		a.Store.AddReservation(resv)
		sr.ReservationIDs = append(sr.ReservationIDs, resv.ID)
	}
	a.Store.AddSeries(sr)
	rest.Revision++

	resp := toSeriesResponse(a.Store, sr)
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, path, key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleGetSeries(w http.ResponseWriter, r *http.Request) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		writeError(w, errNotFound) // owner-only 404, not 401 -- same carve-out as history.
		return
	}
	id := r.PathValue("series_id")
	a.Store.Lock()
	defer a.Store.Unlock()

	sr, ok := a.Store.SeriesByID(id)
	if !ok || sr.UserID != user.ID {
		writeError(w, errNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toSeriesResponse(a.Store, sr))
}
