package httpapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// decodeBody parses the request body as a JSON object (map of raw fields). An empty body is
// treated as an empty object so "all fields missing" validation still runs normally.
func decodeBody(data []byte) (map[string]json.RawMessage, *apiError) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&raw); err != nil {
		return nil, errMalformedRequest("body must be a JSON object")
	}
	return raw, nil
}

// fieldString extracts a string field. ok=false with err=nil means the field was absent.
// A present-but-wrong-JSON-type value is 400 malformed_request.
func fieldString(raw map[string]json.RawMessage, name string) (value string, present bool, err *apiError) {
	rv, exists := raw[name]
	if !exists {
		return "", false, nil
	}
	if err := json.Unmarshal(rv, &value); err != nil {
		return "", true, errMalformedRequest(name + " must be a string")
	}
	return value, true, nil
}

var localTimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$`)
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var queryIntRe = regexp.MustCompile(`^[0-9]+$`)

// fieldStartsAtLocal extracts starts_at_local with its endpoint-specific rule: any JSON type
// other than string is 400 malformed_request (falls under the general wrong-type rule); a
// string that is not a bare local YYYY-MM-DDTHH:MM is 422 validation_failed per spec §5.
func fieldStartsAtLocal(raw map[string]json.RawMessage, name string) (value string, present bool, err *apiError) {
	rv, exists := raw[name]
	if !exists {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(rv, &s); err != nil {
		return "", true, errMalformedRequest(name + " must be a string")
	}
	if !localTimeRe.MatchString(s) {
		return "", true, errValidationFailed(name + " must be a bare local YYYY-MM-DDTHH:MM")
	}
	return s, true, nil
}

// fieldPartySize extracts party_size with its endpoint-specific rule: any invalid value,
// including the wrong JSON type (string, bool, object, array, float, null), is 422
// validation_failed, never 400.
var jsonNumberLiteralRe = regexp.MustCompile(`^-?\d+(\.\d+)?([eE][+-]?\d+)?$`)

func fieldPartySize(raw map[string]json.RawMessage, name string) (value int, present bool, err *apiError) {
	rv, exists := raw[name]
	if !exists {
		return 0, false, nil
	}
	// json.Number happily accepts a quoted JSON string too (its underlying kind is string),
	// which would let party_size:"2" slip past as valid. Require the raw token itself to look
	// like a bare JSON number literal first, so strings/bools/objects/arrays/null are all 422.
	trimmed := strings.TrimSpace(string(rv))
	if !jsonNumberLiteralRe.MatchString(trimmed) {
		return 0, true, errValidationFailed(name + " must be a positive integer")
	}
	var n json.Number
	if err := json.Unmarshal(rv, &n); err != nil {
		return 0, true, errValidationFailed(name + " must be a positive integer")
	}
	i, convErr := n.Int64()
	if convErr != nil {
		return 0, true, errValidationFailed(name + " must be a positive integer")
	}
	// Reject values like "4.0" that round-trip through float: re-check the raw text has no '.' or 'e'.
	text := n.String()
	if strings.ContainsAny(text, ".eE") {
		return 0, true, errValidationFailed(name + " must be a positive integer")
	}
	if i < 1 {
		return 0, true, errValidationFailed(name + " must be at least 1")
	}
	return int(i), true, nil
}

// fieldStrictInt extracts an integer field that must be a bare JSON integer literal (no
// strings, bools, floats) within [min,max], reporting validation_failed (422) on any
// violation. Used for policy fields, expected_revision, and series count/interval_weeks, all
// of which spec requires to reject non-integers -- including booleans -- this way.
func fieldStrictInt(raw map[string]json.RawMessage, name string, min, max int) (value int, present bool, err *apiError) {
	rv, exists := raw[name]
	if !exists {
		return 0, false, nil
	}
	trimmed := strings.TrimSpace(string(rv))
	if !jsonNumberLiteralRe.MatchString(trimmed) {
		return 0, true, errValidationFailed(name + " must be an integer")
	}
	var n json.Number
	if err := json.Unmarshal(rv, &n); err != nil {
		return 0, true, errValidationFailed(name + " must be an integer")
	}
	i, convErr := n.Int64()
	if convErr != nil {
		return 0, true, errValidationFailed(name + " must be an integer")
	}
	text := n.String()
	if strings.ContainsAny(text, ".eE") {
		return 0, true, errValidationFailed(name + " must be an integer")
	}
	if i < int64(min) || i > int64(max) {
		return 0, true, errValidationFailed(fmt.Sprintf("%s must be between %d and %d", name, min, max))
	}
	return int(i), true, nil
}

// extractTableIDs reads table_id and/or table_ids from a reservation write body. Sending both
// is 422 validation_failed. table_id (a string) is treated as a one-element set. present=false
// with no error means neither field was given (callers decide what that means: required for
// create, "keep current" for patch/moves).
func extractTableIDs(raw map[string]json.RawMessage) (ids []string, present bool, err *apiError) {
	tableID, presentSingle, e := fieldString(raw, "table_id")
	if e != nil {
		return nil, true, e
	}
	rawIDs, existsPlural := raw["table_ids"]
	if presentSingle && existsPlural {
		return nil, true, errValidationFailed("table_id and table_ids must not both be provided")
	}
	if existsPlural {
		var list []json.RawMessage
		if err := json.Unmarshal(rawIDs, &list); err != nil {
			return nil, true, errMalformedRequest("table_ids must be an array of strings")
		}
		ids = make([]string, 0, len(list))
		for _, item := range list {
			var s string
			if err := json.Unmarshal(item, &s); err != nil {
				return nil, true, errMalformedRequest("table_ids must be an array of strings")
			}
			ids = append(ids, s)
		}
		return ids, true, nil
	}
	if presentSingle {
		return []string{tableID}, true, nil
	}
	return nil, false, nil
}

// sameTableSet reports whether a and b name the same tables regardless of order -- used to
// detect a true no-op amendment (a reversed-but-same-set combo pair is not a change).
func sameTableSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, y := range b {
		if !seen[y] {
			return false
		}
	}
	return true
}

// hasDuplicate reports whether ids contains the same string twice.
func hasDuplicate(ids []string) bool {
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

func validEmail(email string) bool {
	if strings.ContainsAny(email, " \t\n") {
		return false
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false
	}
	local, domain := parts[0], parts[1]
	if local == "" || domain == "" || strings.Contains(domain, "@") {
		return false
	}
	return true
}

// queryInt validates and parses an integer query parameter written as plain decimal digits
// only: "1e9", "4.0" and "+4" are rejected regardless of numeric value.
func queryInt(s string) (int, bool) {
	if !queryIntRe.MatchString(s) {
		return 0, false
	}
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
		if n > 1<<31 {
			break
		}
	}
	return n, true
}
