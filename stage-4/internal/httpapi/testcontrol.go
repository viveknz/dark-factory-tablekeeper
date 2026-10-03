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

	if apiErr := seedFixture(a.Store, fixture); apiErr != nil {
		writeError(w, apiErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// seedFixture validates and applies a fixture to s exactly as POST /_test/reset does. It is
// shared with the optional DEMO_SEED startup path (see SeedDemoFixture) so both go through the
// same validation and atomic replace.
func seedFixture(s *store.Store, fixture store.Fixture) *apiError {
	if verr := validateFixture(fixture); verr != nil {
		return verr
	}
	err := s.ResetAndSeed(fixture, func(fr store.FixtureReservation) (*store.Reservation, error) {
		return buildSeedReservation(s, fr)
	})
	if err != nil {
		return errMalformedRequest(err.Error())
	}
	return nil
}

// SeedDemoFixture applies fixture outside of an HTTP request, for the optional DEMO_SEED=1
// startup path (cmd/server/main.go). It returns a plain error since there is no HTTP response
// to write.
func SeedDemoFixture(s *store.Store, fixture store.Fixture) error {
	if apiErr := seedFixture(s, fixture); apiErr != nil {
		return apiErr
	}
	return nil
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
		tableIDs := map[string]bool{}
		for _, t := range rest.Tables {
			if e := checkID(t.ID, "table"); e != nil {
				return e
			}
			tableIDs[t.ID] = true
		}
		for _, pair := range rest.Combinable {
			if len(pair) != 2 {
				return errValidationFailed("combinable entries must be pairs of exactly two table ids")
			}
			if pair[0] == pair[1] || !tableIDs[pair[0]] || !tableIDs[pair[1]] {
				return errValidationFailed("combinable entries must name two distinct tables of the same restaurant")
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
		ids := res.ResolvedTableIDs()
		if hasDuplicate(ids) {
			return errValidationFailed("seeded reservation table ids must not repeat")
		}
	}
	return nil
}

// buildSeedReservation resolves a fixture reservation's local time against its restaurant's
// timezone. Called while the store lock is already held by ResetAndSeed.
func buildSeedReservation(s *store.Store, fr store.FixtureReservation) (*store.Reservation, error) {
	rest, ok := s.Restaurants[fr.RestaurantID]
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
	status := fr.Status
	if status == "" {
		status = "confirmed"
	}
	now := time.Now().UTC()
	resv := &store.Reservation{
		ID:            fr.ID,
		Reference:     ref,
		RestaurantID:  fr.RestaurantID,
		TableIDs:      fr.ResolvedTableIDs(),
		UserID:        fr.UserID,
		PartySize:     fr.PartySize,
		Status:        "confirmed", // RecordCreated below assumes a fresh confirmed booking.
		StartsAtLocal: fr.StartsAtLocal,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		CreatedAt:     now,
		Revision:      1,
		AcceptedTerms: rest.Policy0(), // seeded bookings start at revision 1 under policy 0
	}
	resv.RecordCreated(now)
	if status == "cancelled" {
		resv.Status = "cancelled"
		resv.Revision++
		resv.RecordCancelled(now)
	}
	return resv, nil
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
