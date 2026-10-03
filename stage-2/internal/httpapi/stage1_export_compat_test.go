package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"tablekeeper/internal/store"
)

// buildStage1ShapedExport constructs the JSON a stage-1 service would have produced: no
// "combinable" on the restaurant, and a reservation with the singular "table_id" field (no
// "table_ids"). This is what stage-2 must accept per "Existing clients after an upgrade".
func buildStage1ShapedExport(t *testing.T, idempotencyBodyHash, idempotencyResponseBody string) []byte {
	t.Helper()
	hash, err := store.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	passwordHashB64 := base64.StdEncoding.EncodeToString(hash)

	loc, _ := time.LoadLocation("Europe/Berlin")
	startsAt, _ := time.Parse("2006-01-02T15:04", "2027-09-23T19:00")
	startsAt = time.Date(startsAt.Year(), startsAt.Month(), startsAt.Day(), startsAt.Hour(), startsAt.Minute(), 0, 0, loc)
	endsAt := startsAt.Add(90 * time.Minute)

	stateObj := map[string]interface{}{
		"users": map[string]interface{}{
			"u_legacy": map[string]interface{}{
				"id":            "u_legacy",
				"email":         "legacy@example.com",
				"password_hash": passwordHashB64,
				"display_name":  "Legacy",
			},
		},
		"tokens": map[string]interface{}{
			"legacy-token-abc123": "u_legacy",
		},
		"restaurants": map[string]interface{}{
			"r_legacy": map[string]interface{}{
				"id":                            "r_legacy",
				"name":                          "Legacy Place",
				"timezone":                      "Europe/Berlin",
				"slot_minutes":                  30,
				"reservation_duration_minutes":  90,
				"cancellation_cutoff_minutes":   120,
				"opening_hours": []map[string]string{
					{"weekday": "thu", "opens": "18:00", "closes": "23:00"},
				},
				"tables": []map[string]interface{}{
					{"id": "t_1", "label": "1", "capacity": 2},
				},
				// no "combinable" field at all: stage-1 shape.
			},
		},
		"restaurant_order": []string{"r_legacy"},
		"reservations": map[string]interface{}{
			"res_legacy": map[string]interface{}{
				"id":              "res_legacy",
				"reference":       "LEGACY1",
				"restaurant_id":   "r_legacy",
				"table_id":        "t_1", // singular, stage-1 shape: no table_ids
				"user_id":         "u_legacy",
				"party_size":      2,
				"status":          "confirmed",
				"starts_at_local": "2027-09-23T19:00",
				"starts_at":       startsAt.Format("2006-01-02T15:04:05Z07:00"),
				"ends_at":         endsAt.Format("2006-01-02T15:04:05Z07:00"),
				"created_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
			},
		},
		"idempotency": map[string]interface{}{
			"u_legacy\x00POST\x00/reservations\x00legacy-retry-key": map[string]interface{}{
				"body_hash":     idempotencyBodyHash,
				"status":        201,
				"response_body": idempotencyResponseBody,
			},
		},
	}
	stateBytes, err := json.Marshal(stateObj)
	if err != nil {
		t.Fatalf("marshal legacy state: %v", err)
	}
	envelope := map[string]interface{}{
		"track":          "tablekeeper",
		"format_version": 1,
		"state":          json.RawMessage(stateBytes),
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return out
}

func TestImportAcceptsStage1ShapedExport(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)

	retryBody := map[string]interface{}{"restaurant_id": "r_legacy", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	retryBodyBytes, _ := json.Marshal(retryBody)
	raw, _ := decodeBody(retryBodyBytes)
	bodyHash := canonicalBody(raw)
	legacyResponseBody := `{"reservation_id":"res_legacy","reference":"LEGACY1","restaurant_id":"r_legacy","table_id":"t_1","party_size":2,"status":"confirmed","starts_at_local":"2027-09-23T19:00","starts_at":"2027-09-23T19:00:00+02:00","ends_at":"2027-09-23T20:30:00+02:00","created_at":"2026-01-01T00:00:00Z"}`

	payload := buildStage1ShapedExport(t, bodyHash, legacyResponseBody)

	resp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("import status = %d, want 204", resp.StatusCode)
	}

	// The legacy bearer token must work.
	respMe, outMe := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer legacy-token-abc123"}, nil)
	if respMe.StatusCode != http.StatusOK {
		t.Fatalf("legacy token auth = %d %v", respMe.StatusCode, outMe)
	}
	list := outMe["reservations"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("expected the imported reservation to be visible, got %d", len(list))
	}
	imported := list[0].(map[string]interface{})
	// The stage-1-shaped reservation (singular table_id, no table_ids at import time) must be
	// readable in stage-2's current response shape too.
	if imported["reference"] != "LEGACY1" {
		t.Errorf("reference = %v, want LEGACY1", imported["reference"])
	}
	ids, ok := imported["table_ids"].([]interface{})
	if !ok || len(ids) != 1 || ids[0] != "t_1" {
		t.Errorf("table_ids after importing a legacy table_id = %v", imported["table_ids"])
	}
	if imported["table_id"] != "t_1" {
		t.Errorf("table_id after import = %v, want t_1", imported["table_id"])
	}

	// The retained reference still works through the direct lookup endpoint.
	respLookup, outLookup := doJSON(t, "GET", srv.URL+"/reservations/LEGACY1", map[string]string{"Authorization": "Bearer legacy-token-abc123"}, nil)
	if respLookup.StatusCode != http.StatusOK || outLookup["reference"] != "LEGACY1" {
		t.Fatalf("lookup by reference after import = %d %v", respLookup.StatusCode, outLookup)
	}

	// A lost-response retry: same key+body as the pre-export request completes correctly,
	// returning the ORIGINAL (legacy-shaped) response with 200, not a new booking.
	respRetry, outRetry := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer legacy-token-abc123", "Idempotency-Key": "legacy-retry-key"},
		retryBody)
	if respRetry.StatusCode != http.StatusOK {
		t.Fatalf("lost-response retry after import = %d %v, want 200", respRetry.StatusCode, outRetry)
	}
	if outRetry["reference"] != "LEGACY1" {
		t.Errorf("retry returned reference %v, want the original LEGACY1 (no new booking)", outRetry["reference"])
	}
	// The replayed body is frozen from a stage-1 response (no "table_ids"), but spec says every
	// POST /reservations response "always carries table_ids" -- the replay must be upgraded to
	// the current shape, just like a live GET on the same reservation already is.
	ids, ok = outRetry["table_ids"].([]interface{})
	if !ok || len(ids) != 1 || ids[0] != "t_1" {
		t.Errorf("retry table_ids = %v, want [t_1] (replay of a pre-migration idempotency record must still carry table_ids)", outRetry["table_ids"])
	}
	if outRetry["table_id"] != "t_1" {
		t.Errorf("retry table_id = %v, want t_1", outRetry["table_id"])
	}

	// Still exactly one reservation for this user: the retry did not create a second booking.
	_, outMe2 := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer legacy-token-abc123"}, nil)
	if len(outMe2["reservations"].([]interface{})) != 1 {
		t.Errorf("retry must not create a duplicate booking, got %d reservations", len(outMe2["reservations"].([]interface{})))
	}
}

func TestImportAcceptsStage1RestaurantWithoutCombinableField(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	payload := buildStage1ShapedExport(t, "unused", "unused")
	resp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("import status = %d, want 204", resp.StatusCode)
	}
	// A restaurant imported without "combinable" must simply have none declared, not error.
	resp2, out2 := doJSON(t, "GET", srv.URL+"/restaurants/r_legacy", nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get restaurant after legacy import = %d", resp2.StatusCode)
	}
	if combo, ok := out2["combinable"]; ok && combo != nil {
		if arr, isArr := combo.([]interface{}); isArr && len(arr) != 0 {
			t.Errorf("combinable = %v, want empty/absent for a legacy restaurant", combo)
		}
	}
}
