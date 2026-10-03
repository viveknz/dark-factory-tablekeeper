package httpapi

import (
	"encoding/json"
	"net/http"
)

// apiError is a handler-level error carrying the HTTP status and spec error code together.
type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string { return e.Message }

func newErr(status int, code, message string) *apiError {
	return &apiError{Status: status, Code: code, Message: message}
}

var (
	errMalformedRequest     = func(msg string) *apiError { return newErr(http.StatusBadRequest, "malformed_request", msg) }
	errMissingIdempotency   = newErr(http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required")
	errUnauthenticated      = newErr(http.StatusUnauthorized, "unauthenticated", "missing, malformed or unknown bearer token")
	errForbidden            = newErr(http.StatusForbidden, "forbidden", "not permitted to access this resource")
	errNotFound             = newErr(http.StatusNotFound, "not_found", "resource not found")
	errIdempotencyKeyReuse  = newErr(http.StatusConflict, "idempotency_key_reuse", "idempotency key already used with a different request body")
	errValidationFailed     = func(msg string) *apiError { return newErr(http.StatusUnprocessableEntity, "validation_failed", msg) }
	errTableUnavailable     = newErr(http.StatusConflict, "table_unavailable", "table is not available for that interval")
	errNotOnSlotGrid        = newErr(http.StatusUnprocessableEntity, "not_on_slot_grid", "starts_at_local is not on the slot grid")
	errOutsideOpeningHours  = newErr(http.StatusUnprocessableEntity, "outside_opening_hours", "slot is outside opening hours")
	errPartyExceedsCapacity = newErr(http.StatusUnprocessableEntity, "party_exceeds_capacity", "party_size exceeds the selected table(s) capacity")
	errCombinationNotAllowed = newErr(http.StatusUnprocessableEntity, "combination_not_allowed", "that combination of tables is not allowed")
	errInvalidLocalTime     = newErr(http.StatusUnprocessableEntity, "invalid_local_time", "starts_at_local does not exist in the restaurant's timezone")
	errCutoffPassed         = newErr(http.StatusConflict, "cutoff_passed", "cancellation/amendment cutoff has passed")
	errReservationCancelled = newErr(http.StatusConflict, "reservation_cancelled", "reservation is cancelled")
	errEmailTaken           = newErr(http.StatusConflict, "email_taken", "email already registered")
	errStaleRevision        = newErr(http.StatusConflict, "stale_revision", "expected_revision does not match the reservation's current revision")
	errAlreadyInSeries      = newErr(http.StatusConflict, "already_in_series", "this reservation has already been adopted by an earlier series")
)

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, e *apiError) {
	var env errorEnvelope
	env.Error.Code = e.Code
	env.Error.Message = e.Message
	writeJSON(w, e.Status, env)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
