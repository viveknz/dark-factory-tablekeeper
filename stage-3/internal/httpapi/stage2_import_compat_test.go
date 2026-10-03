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

// buildStage2ShapedExport constructs the JSON a stage-2 service would have produced: a
// restaurant with "combinable" but no "manager_user_ids"/"policies", and a reservation with
// "table_ids" but no "revision"/"accepted_terms"/"history" -- all of which are new in stage 3.
func buildStage2ShapedExport(t *testing.T) ([]byte, string) {
	t.Helper()
	hash, err := store.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	passwordHashB64 := base64.StdEncoding.EncodeToString(hash)

	loc, _ := time.LoadLocation("Europe/Berlin")
	startsAt := time.Date(2030, 1, 7, 19, 0, 0, 0, loc)
	endsAt := startsAt.Add(90 * time.Minute)

	stateObj := map[string]interface{}{
		"users": map[string]interface{}{
			"u_s2": map[string]interface{}{
				"id": "u_s2", "email": "s2@example.com", "password_hash": passwordHashB64, "display_name": "S2",
			},
		},
		"tokens": map[string]interface{}{"s2-token-xyz": "u_s2"},
		"restaurants": map[string]interface{}{
			"r_s2": map[string]interface{}{
				"id": "r_s2", "name": "Stage2 Place", "timezone": "Europe/Berlin",
				"slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 60,
				"opening_hours": []map[string]string{{"weekday": "mon", "opens": "18:00", "closes": "23:00"}},
				"tables": []map[string]interface{}{
					{"id": "t_a", "label": "A", "capacity": 2}, {"id": "t_b", "label": "B", "capacity": 2},
				},
				"combinable": [][]string{{"t_a", "t_b"}},
				// no manager_user_ids, no policies: stage-2 shape.
			},
		},
		"restaurant_order": []string{"r_s2"},
		"reservations": map[string]interface{}{
			"res_s2": map[string]interface{}{
				"id": "res_s2", "reference": "STAGE2R1", "restaurant_id": "r_s2",
				"table_ids": []string{"t_a"}, // stage-2 shape: no revision/accepted_terms/history.
				"user_id":   "u_s2", "party_size": 2, "status": "confirmed",
				"starts_at_local": "2030-01-07T19:00",
				"starts_at":       startsAt.Format("2006-01-02T15:04:05Z07:00"),
				"ends_at":         endsAt.Format("2006-01-02T15:04:05Z07:00"),
				"created_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
			},
		},
		"idempotency": map[string]interface{}{},
	}
	stateBytes, err := json.Marshal(stateObj)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	envelope := map[string]interface{}{"track": "tablekeeper", "format_version": 1, "state": json.RawMessage(stateBytes)}
	out, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return out, "s2-token-xyz"
}

func TestImportAcceptsStage2ShapedExport(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	payload, token := buildStage2ShapedExport(t)

	resp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("import status = %d, want 204", resp.StatusCode)
	}

	// The stage-2-shaped reservation must now carry revision 1 and policy-0 accepted_terms.
	resp2, out2 := doJSON(t, "GET", srv.URL+"/reservations/STAGE2R1", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get imported stage-2 reservation = %d %v", resp2.StatusCode, out2)
	}
	if out2["revision"].(float64) != 1 {
		t.Errorf("revision = %v, want 1", out2["revision"])
	}
	terms := out2["accepted_terms"].(map[string]interface{})
	if terms["policy_version"].(float64) != 0 {
		t.Errorf("accepted_terms.policy_version = %v, want 0", terms["policy_version"])
	}

	// It must also have a synthetic history.
	_, hist := doJSON(t, "GET", srv.URL+"/reservations/STAGE2R1/history", map[string]string{"Authorization": "Bearer " + token}, nil)
	if len(hist["entries"].([]interface{})) != 1 {
		t.Errorf("imported reservation history = %v, want 1 synthetic created entry", hist["entries"])
	}

	// Adoption into a series must work on this imported reservation.
	respSeries, outSeries := doJSON(t, "POST", srv.URL+"/series",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "adopt-imported"},
		map[string]interface{}{"anchor_reference": "STAGE2R1", "count": 2, "interval_weeks": 1})
	if respSeries.StatusCode != http.StatusCreated {
		t.Fatalf("adopt imported stage-2 reservation into series = %d %v", respSeries.StatusCode, outSeries)
	}
}
