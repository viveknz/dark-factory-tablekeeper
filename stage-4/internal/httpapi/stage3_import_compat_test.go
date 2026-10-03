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

// TestImportAcceptsStage3ShapedExportAndSupportsReplanAndAmend builds a hand-built stage-3
// shaped export (revision/accepted_terms/history/series present, but no Closures field on the
// restaurant and no Plans map -- both new in stage 4) containing a series with one occurrence
// already cancelled before the export was taken, imports it into stage-4, then exercises BOTH
// new stage-4 operations (series amend, replan) on that imported series -- per spec, these must
// work correctly on imported series state, including an already-cancelled occurrence.
func TestImportAcceptsStage3ShapedExportAndSupportsReplanAndAmend(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)

	hash, err := store.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	passwordHashB64 := base64.StdEncoding.EncodeToString(hash)

	loc, _ := time.LoadLocation("Europe/Berlin")
	mkTime := func(y, m, d, hh, mi int) time.Time {
		return time.Date(y, time.Month(m), d, hh, mi, 0, 0, loc)
	}
	terms := map[string]interface{}{
		"policy_version": 0, "slot_minutes": 30, "reservation_duration_minutes": 60,
		"cancellation_cutoff_minutes": 0,
		"opening_hours":               []map[string]string{{"weekday": "mon", "opens": "17:00", "closes": "23:00"}},
		"capacities":                  map[string]int{"t_1": 2, "t_2": 2},
	}

	mkReservation := func(id, ref, startsLocal string, start time.Time, status string, seriesID string, idx int) map[string]interface{} {
		end := start.Add(60 * time.Minute)
		return map[string]interface{}{
			"id": id, "reference": ref, "restaurant_id": "r_s3", "table_ids": []string{"t_1"},
			"user_id": "u_s3", "party_size": 2, "status": status,
			"starts_at_local": startsLocal,
			"starts_at":       start.Format("2006-01-02T15:04:05Z07:00"),
			"ends_at":         end.Format("2006-01-02T15:04:05Z07:00"),
			"created_at":      time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
			"revision":        1, "accepted_terms": terms,
			"series_id": seriesID, "series_index": idx, "is_exception": false,
		}
	}

	stateObj := map[string]interface{}{
		"users": map[string]interface{}{
			"u_s3": map[string]interface{}{"id": "u_s3", "email": "s3@example.com", "password_hash": passwordHashB64, "display_name": "S3"},
		},
		"tokens": map[string]interface{}{"s3-token": "u_s3"},
		"restaurants": map[string]interface{}{
			"r_s3": map[string]interface{}{
				"id": "r_s3", "name": "Stage3 Place", "timezone": "Europe/Berlin",
				"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 0,
				"opening_hours": []map[string]string{{"weekday": "mon", "opens": "17:00", "closes": "23:00"}},
				"tables":        []map[string]interface{}{{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 2}},
				"manager_user_ids": []string{"u_s3"},
				"revision":      0,
				// no "closures" field: stage-3 shape.
			},
		},
		"restaurant_order": []string{"r_s3"},
		"reservations": map[string]interface{}{
			"res_anchor": mkReservation("res_anchor", "S3ANCHOR", "2030-01-07T19:00", mkTime(2030, 1, 7, 19, 0), "confirmed", "ser_s3", 0),
			"res_occ1":   mkReservation("res_occ1", "S3OCC0001", "2030-01-14T19:00", mkTime(2030, 1, 14, 19, 0), "cancelled", "ser_s3", 1),
			"res_occ2":   mkReservation("res_occ2", "S3OCC0002", "2030-01-21T19:00", mkTime(2030, 1, 21, 19, 0), "confirmed", "ser_s3", 2),
		},
		"series": map[string]interface{}{
			"ser_s3": map[string]interface{}{
				"id": "ser_s3", "user_id": "u_s3", "restaurant_id": "r_s3", "revision": 2, "interval_weeks": 1,
				"reservation_ids": []string{"res_anchor", "res_occ1", "res_occ2"},
			},
		},
		"idempotency": map[string]interface{}{},
		// no "plans" field: stage-3 shape.
	}
	stateBytes, err := json.Marshal(stateObj)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	envelope := map[string]interface{}{"track": "tablekeeper", "format_version": 1, "state": json.RawMessage(stateBytes)}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	resp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("import status = %d, want 204", resp.StatusCode)
	}

	token := "s3-token"

	// Series amend on the imported series, from index 0: occurrence 1 is already cancelled and
	// must be skipped (not erred on, not un-cancelled).
	respAmend, outAmend := doJSON(t, "POST", srv.URL+"/series/ser_s3/amend",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "amend-imported"},
		map[string]interface{}{"expected_revision": 2, "from_index": 0, "local_time": "20:00"})
	if respAmend.StatusCode != http.StatusCreated {
		t.Fatalf("amend on imported series = %d %v", respAmend.StatusCode, outAmend)
	}
	occs := outAmend["occurrences"].([]interface{})
	if occs[0].(map[string]interface{})["reservation"].(map[string]interface{})["starts_at_local"] != "2030-01-07T20:00" {
		t.Errorf("occurrence 0 after amend = %v, want shifted to 20:00", occs[0])
	}
	if occs[1].(map[string]interface{})["reservation"].(map[string]interface{})["status"] != "cancelled" {
		t.Errorf("occurrence 1 (already cancelled before export) = %v, want still cancelled, untouched", occs[1])
	}
	if occs[2].(map[string]interface{})["reservation"].(map[string]interface{})["starts_at_local"] != "2030-01-21T20:00" {
		t.Errorf("occurrence 2 after amend = %v, want shifted to 20:00", occs[2])
	}

	// Replan on the imported restaurant: close t_1 for the (now-shifted) anchor's window, move
	// it to t_2.
	respPreview, outPreview := doJSON(t, "POST", srv.URL+"/restaurants/r_s3/replans",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "preview-imported"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	if respPreview.StatusCode != http.StatusCreated {
		t.Fatalf("preview on imported restaurant = %d %v", respPreview.StatusCode, outPreview)
	}
	if outPreview["moved_count"].(float64) != 1 {
		t.Errorf("moved_count = %v, want 1 (the shifted anchor occurrence moves off t_1)", outPreview["moved_count"])
	}
}
