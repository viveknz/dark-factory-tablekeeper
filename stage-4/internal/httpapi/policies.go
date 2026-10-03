package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"tablekeeper/internal/store"
)

type policyResponse struct {
	PolicyVersion              int               `json:"policy_version"`
	EffectiveFrom              string            `json:"effective_from"`
	SlotMinutes                int               `json:"slot_minutes"`
	ReservationDurationMinutes int               `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int               `json:"cancellation_cutoff_minutes"`
	OpeningHours               []store.OpeningHour `json:"opening_hours"`
	Capacities                 map[string]int    `json:"capacities"`
}

func toPolicyResponse(p store.Policy) policyResponse {
	return policyResponse{
		PolicyVersion:              p.PolicyVersion,
		EffectiveFrom:              p.EffectiveFrom,
		SlotMinutes:                p.SlotMinutes,
		ReservationDurationMinutes: p.ReservationDurationMinutes,
		CancellationCutoffMinutes:  p.CancellationCutoffMinutes,
		OpeningHours:               p.OpeningHours,
		Capacities:                 p.Capacities,
	}
}

var validWeekdays = map[string]bool{"mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true}

// validatePolicyOpeningHours checks each entry has a recognized weekday and parseable
// HH:MM times, and that no weekday repeats.
func validatePolicyOpeningHours(hours []store.OpeningHour) *apiError {
	seen := map[string]bool{}
	for _, oh := range hours {
		if !validWeekdays[oh.Weekday] {
			return errValidationFailed("opening_hours has an invalid weekday")
		}
		if seen[oh.Weekday] {
			return errValidationFailed("opening_hours must not repeat a weekday")
		}
		seen[oh.Weekday] = true
		if _, _, ok := parseHHMM(oh.Opens); !ok {
			return errValidationFailed("opening_hours.opens must be HH:MM")
		}
		if _, _, ok := parseHHMM(oh.Closes); !ok {
			return errValidationFailed("opening_hours.closes must be HH:MM")
		}
	}
	return nil
}

// validatePolicyBody validates a complete policy body against rest's table ids. Every field
// is required; any violation is 422 validation_failed with no partial effect.
func validatePolicyBody(raw map[string]json.RawMessage, rest *store.Restaurant) (store.Policy, *apiError) {
	effectiveFrom, presentEF, e := fieldString(raw, "effective_from")
	if e != nil {
		return store.Policy{}, e
	}
	if !presentEF || !isValidCalendarDateString(effectiveFrom) {
		return store.Policy{}, errValidationFailed("effective_from must be a YYYY-MM-DD date")
	}

	slotMinutes, presentSM, eSM := fieldStrictInt(raw, "slot_minutes", 1, 1440)
	if eSM != nil {
		return store.Policy{}, eSM
	}
	if !presentSM {
		return store.Policy{}, errValidationFailed("slot_minutes is required")
	}

	durationMinutes, presentDM, eDM := fieldStrictInt(raw, "reservation_duration_minutes", 1, 1440)
	if eDM != nil {
		return store.Policy{}, eDM
	}
	if !presentDM {
		return store.Policy{}, errValidationFailed("reservation_duration_minutes is required")
	}

	cutoffMinutes, presentCM, eCM := fieldStrictInt(raw, "cancellation_cutoff_minutes", 0, 10080)
	if eCM != nil {
		return store.Policy{}, eCM
	}
	if !presentCM {
		return store.Policy{}, errValidationFailed("cancellation_cutoff_minutes is required")
	}

	rawHours, existsHours := raw["opening_hours"]
	if !existsHours {
		return store.Policy{}, errValidationFailed("opening_hours is required")
	}
	var hours []store.OpeningHour
	if err := json.Unmarshal(rawHours, &hours); err != nil {
		return store.Policy{}, errMalformedRequest("opening_hours must be an array of {weekday,opens,closes}")
	}
	if verr := validatePolicyOpeningHours(hours); verr != nil {
		return store.Policy{}, verr
	}

	rawCaps, existsCaps := raw["capacities"]
	if !existsCaps {
		return store.Policy{}, errValidationFailed("capacities is required")
	}
	var capsRaw map[string]json.RawMessage
	if err := json.Unmarshal(rawCaps, &capsRaw); err != nil {
		return store.Policy{}, errMalformedRequest("capacities must be an object of table_id -> capacity")
	}
	if len(capsRaw) != len(rest.Tables) {
		return store.Policy{}, errValidationFailed("capacities must name exactly the restaurant's table ids")
	}
	caps := map[string]int{}
	for _, t := range rest.Tables {
		capRaw, ok := capsRaw[t.ID]
		if !ok {
			return store.Policy{}, errValidationFailed("capacities must name exactly the restaurant's table ids")
		}
		trimmed := string(capRaw)
		if !jsonNumberLiteralRe.MatchString(trimmed) {
			return store.Policy{}, errValidationFailed("capacities values must be integers")
		}
		var n json.Number
		if err := json.Unmarshal(capRaw, &n); err != nil {
			return store.Policy{}, errValidationFailed("capacities values must be integers")
		}
		i, convErr := n.Int64()
		if convErr != nil || containsFloatMarker(n.String()) {
			return store.Policy{}, errValidationFailed("capacities values must be integers")
		}
		if i < 1 || i > 100 {
			return store.Policy{}, errValidationFailed("capacities values must be 1..100")
		}
		caps[t.ID] = int(i)
	}

	return store.Policy{
		EffectiveFrom:              effectiveFrom,
		SlotMinutes:                slotMinutes,
		ReservationDurationMinutes: durationMinutes,
		CancellationCutoffMinutes:  cutoffMinutes,
		OpeningHours:               hours,
		Capacities:                 caps,
	}, nil
}

func containsFloatMarker(s string) bool {
	for _, c := range s {
		if c == '.' || c == 'e' || c == 'E' {
			return true
		}
	}
	return false
}

func (a *API) handlePublishPolicy(w http.ResponseWriter, r *http.Request) {
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
	restID := r.PathValue("id")

	a.Store.Lock()
	defer a.Store.Unlock()

	rest, ok := a.Store.Restaurants[restID]
	if !ok {
		writeError(w, errNotFound)
		return
	}
	if !rest.IsManager(user.ID) {
		writeError(w, errForbidden)
		return
	}

	path := "/restaurants/" + restID + "/policies"
	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, path, key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	policy, verr := validatePolicyBody(raw, rest)
	if verr != nil {
		writeError(w, verr)
		return
	}

	version := rest.PublishPolicy(policy)
	policy.PolicyVersion = version
	rest.Revision++

	resp := toPolicyResponse(policy)
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, path, key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}

func (a *API) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	restID := r.PathValue("id")
	a.Store.Lock()
	defer a.Store.Unlock()

	rest, ok := a.Store.Restaurants[restID]
	if !ok {
		writeError(w, errNotFound)
		return
	}
	published := rest.PublishedPolicies()
	out := make([]policyResponse, 0, len(published))
	for _, p := range published {
		out = append(out, toPolicyResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"policies": out})
}
