package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"time"

	"tablekeeper/internal/idgen"
	"tablekeeper/internal/store"
)

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleReset(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var fixture store.Fixture
	if len(body) > 0 {
		if err := json.Unmarshal(body, &fixture); err != nil {
			writeError(w, errMalformedRequest("reset body must be a JSON object"))
			return
		}
	}

	if verr := validateFixture(fixture); verr != nil {
		writeError(w, verr)
		return
	}

	err := a.Store.ResetAndSeed(fixture, func(fr store.FixtureReservation) (*store.Reservation, error) {
		return a.buildSeedReservation(fr)
	})
	if err != nil {
		writeError(w, errMalformedRequest(err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var referenceRe = regexp.MustCompile(`^[A-Z0-9]{6,12}$`)

// validateFixture checks the opaque-ID length limit (spec §3.4, "This limit also applies to
// IDs supplied in reset fixtures") and the booking reference format (spec §8) for every ID
// and reference in the fixture, before any state is replaced.
func validateFixture(f store.Fixture) *apiError {
	checkID := func(id, what string) *apiError {
		if len(id) == 0 || len(id) > 64 {
			return errValidationFailed(what + " id must be 1 to 64 characters")
		}
		return nil
	}
	for _, u := range f.Users {
		if e := checkID(u.ID, "user"); e != nil {
			return e
		}
	}
	for _, rest := range f.Restaurants {
		if e := checkID(rest.ID, "restaurant"); e != nil {
			return e
		}
		for _, t := range rest.Tables {
			if e := checkID(t.ID, "table"); e != nil {
				return e
			}
		}
	}
	for _, res := range f.Reservations {
		if e := checkID(res.ID, "reservation"); e != nil {
			return e
		}
		if res.Reference != "" && !referenceRe.MatchString(res.Reference) {
			return errValidationFailed("reservation reference must be 6 to 12 characters of A-Z0-9")
		}
	}
	return nil
}

// buildSeedReservation resolves a fixture reservation's local time against its restaurant's
// timezone. Called while the store lock is already held by ResetAndSeed.
func (a *API) buildSeedReservation(fr store.FixtureReservation) (*store.Reservation, error) {
	rest, ok := a.Store.Restaurants[fr.RestaurantID]
	if !ok {
		return nil, errBadFixture("reservation " + fr.ID + " references unknown restaurant " + fr.RestaurantID)
	}
	loc, err := time.LoadLocation(rest.Timezone)
	if err != nil {
		return nil, errBadFixture("restaurant " + rest.ID + " has invalid timezone")
	}
	startsAt, rerr := resolveLocalString(loc, fr.StartsAtLocal)
	if rerr != nil {
		return nil, errBadFixture("reservation " + fr.ID + " has an invalid starts_at_local")
	}
	endsAt := startsAt.Add(time.Duration(rest.ReservationDurationMinutes) * time.Minute)
	ref := fr.Reference
	if ref == "" {
		ref = idgen.Reference()
	}
	return &store.Reservation{
		ID:            fr.ID,
		Reference:     ref,
		RestaurantID:  fr.RestaurantID,
		TableID:       fr.TableID,
		UserID:        fr.UserID,
		PartySize:     fr.PartySize,
		Status:        "confirmed",
		StartsAtLocal: fr.StartsAtLocal,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

type fixtureError struct{ msg string }

func (e *fixtureError) Error() string { return e.msg }
func errBadFixture(msg string) error  { return &fixtureError{msg} }

type exportResponse struct {
	Track         string          `json:"track"`
	FormatVersion int             `json:"format_version"`
	State         json.RawMessage `json:"state"`
}

func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	a.Store.Lock()
	state := a.Store.ExportLocked()
	a.Store.Unlock()

	raw, err := store.MarshalExport(state)
	if err != nil {
		writeError(w, errValidationFailed("could not export state"))
		return
	}
	writeJSON(w, http.StatusOK, exportResponse{Track: "tablekeeper", FormatVersion: 1, State: raw})
}

type importRequest struct {
	Track         string          `json:"track"`
	FormatVersion int             `json:"format_version"`
	State         json.RawMessage `json:"state"`
}

func (a *API) handleImport(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req importRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, errValidationFailed("import body must be a JSON object"))
		return
	}
	if req.Track != "tablekeeper" || req.FormatVersion != 1 || len(req.State) == 0 {
		writeError(w, errValidationFailed("import body must have track=tablekeeper, format_version=1 and state"))
		return
	}
	state, err := store.UnmarshalExport(req.State)
	if err != nil {
		writeError(w, errValidationFailed("state is not a valid exported snapshot"))
		return
	}

	a.Store.Lock()
	a.Store.ImportLocked(state)
	a.Store.Unlock()

	w.WriteHeader(http.StatusNoContent)
}
