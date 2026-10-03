package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"tablekeeper/internal/store"
)

var exactHHMMRe = regexp.MustCompile(`^\d{2}:\d{2}$`)

// validLocalTimeHHMM parses a strict "HH:MM" (00:00..23:59) string.
func validLocalTimeHHMM(s string) (hour, minute int, ok bool) {
	if !exactHHMMRe.MatchString(s) {
		return 0, 0, false
	}
	return parseHHMM(s)
}

type amendResolved struct {
	resv             *store.Reservation
	newLocalStr      string
	startsAt         time.Time
	endsAt           time.Time
	terms            store.AcceptedTerms
	oldStartsAtLocal string
}

func (a *API) handleSeriesAmend(w http.ResponseWriter, r *http.Request) {
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
	seriesID := r.PathValue("series_id")

	a.Store.Lock()
	defer a.Store.Unlock()

	sr, ok := a.Store.SeriesByID(seriesID)
	if !ok || sr.UserID != user.ID {
		writeError(w, errNotFound) // owner-only; unknown series is also 404 for a non-owner.
		return
	}

	path := "/series/" + seriesID + "/amend"
	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, path, key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	expectedRevision, presentER, eER := fieldStrictInt(raw, "expected_revision", 1, maxExpectedRevision)
	if eER != nil {
		writeError(w, eER)
		return
	}
	if !presentER {
		writeError(w, errValidationFailed("expected_revision is required"))
		return
	}
	fromIndex, presentFI, eFI := fieldStrictInt(raw, "from_index", 0, len(sr.ReservationIDs)-1)
	if eFI != nil {
		writeError(w, eFI)
		return
	}
	if !presentFI {
		writeError(w, errValidationFailed("from_index is required"))
		return
	}
	localTimeStr, presentLT, eLT := fieldString(raw, "local_time")
	if eLT != nil {
		writeError(w, eLT)
		return
	}
	newHour, newMin, okTime := validLocalTimeHHMM(localTimeStr)
	if !presentLT || !okTime {
		writeError(w, errValidationFailed("local_time must be exactly HH:MM, 00:00..23:59"))
		return
	}

	// A mismatched series revision is checked BEFORE any occurrence's cutoff/validation.
	if expectedRevision != sr.Revision {
		writeError(w, errStaleRevision)
		return
	}

	rest, ok := a.Store.Restaurants[sr.RestaurantID]
	if !ok {
		writeError(w, errValidationFailed("series restaurant no longer exists"))
		return
	}

	// Pass 1: resolve every eligible occurrence's non-occupancy validity, in index order,
	// stopping at the first failure -- this guarantees a non-occupancy error from a later
	// occurrence is reported even if an earlier occurrence would otherwise only fail on
	// occupancy (checked separately, after this pass fully succeeds).
	var resolved []amendResolved // only real changes; no-ops are simply skipped
	for i := fromIndex; i < len(sr.ReservationIDs); i++ {
		resv := a.Store.Reservations[sr.ReservationIDs[i]]
		if resv.Status == "cancelled" || resv.IsException {
			continue
		}
		y, m, d, _, _, ok := splitLocal(resv.StartsAtLocal)
		if !ok {
			continue
		}
		newLocalStr := formatDateHHMM(y, m, d, newHour, newMin)
		if newLocalStr == resv.StartsAtLocal {
			continue // no-op: identical resulting local time
		}

		cutoff := minutesToDuration(resv.AcceptedTerms.CancellationCutoffMinutes)
		if !nowUTC().Before(resv.StartsAt.Add(-cutoff)) {
			writeError(w, errCutoffPassed)
			return
		}

		_, _, startsAt, endsAt, terms, verr := a.resolveBookingFields(sr.RestaurantID, resv.TableIDs, newLocalStr, resv.PartySize)
		if verr != nil {
			writeError(w, verr)
			return
		}

		resolved = append(resolved, amendResolved{
			resv: resv, newLocalStr: newLocalStr, startsAt: startsAt, endsAt: endsAt, terms: terms,
			oldStartsAtLocal: resv.StartsAtLocal,
		})
	}

	// Pass 2: occupancy conflicts, in the same (index) order -- the first conflict found wins.
	for _, item := range resolved {
		for _, tid := range item.resv.TableIDs {
			if !a.Store.TableAvailable(sr.RestaurantID, tid, item.startsAt, item.endsAt, item.resv.ID) {
				writeError(w, errTableUnavailable)
				return
			}
		}
	}

	// All validated: commit atomically.
	now := time.Now().UTC()
	for _, item := range resolved {
		item.resv.StartsAtLocal = item.newLocalStr
		item.resv.StartsAt = item.startsAt
		item.resv.EndsAt = item.endsAt
		item.resv.AcceptedTerms = item.terms
		item.resv.Revision++
		// Series amendments never mark an exception -- only individual PATCH/replan moves do.
		item.resv.RecordChanged(now, item.resv.TableIDs, item.oldStartsAtLocal, item.resv.PartySize)
	}
	if len(resolved) > 0 {
		sr.Revision++
		rest.Revision++
	}

	resp := toSeriesResponse(a.Store, sr)
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, path, key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func formatDateHHMM(y, m, d, hh, mi int) string {
	return fmt.Sprintf("%04d-%02d-%02dT%s", y, m, d, formatHHMM(hh, mi))
}
