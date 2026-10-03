package httpapi

import (
	"net/http"
	"testing"
)

const movesPolicyFixture = `{
	"users": [{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}],
	"restaurants": [
		{
			"id": "r_m", "name": "Moves Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 60,
			"opening_hours": [{"weekday": "mon", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 2}]
		}
	],
	"reservations": []
}`

func TestMovesBatchRealChangeBumpsRestaurantRevisionOnceAndNoopKeepsHistory(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, movesPolicyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, r1 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_m", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref1 := r1["reference"].(string)
	_, r2 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k2"},
		map[string]interface{}{"restaurant_id": "r_m", "table_id": "t_2", "starts_at_local": "2030-01-07T20:00", "party_size": 2})
	ref2 := r2["reference"].(string)

	// Move ref1 to t_2 at a later time (real change); leave ref2 as a true no-op (resubmit its
	// current fields).
	resp, out := doJSON(t, "POST", srv.URL+"/reservation-moves",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "move1"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": ref1, "table_id": "t_1", "starts_at_local": "2030-01-07T21:00"},
			{"reference": ref2, "table_id": "t_2", "starts_at_local": "2030-01-07T20:00", "party_size": 2},
		}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("moves batch = %d %v", resp.StatusCode, out)
	}
	results := out["reservations"].([]interface{})
	rev1 := results[0].(map[string]interface{})
	rev2 := results[1].(map[string]interface{})
	if rev1["revision"].(float64) != 2 {
		t.Errorf("ref1 (real change) revision = %v, want 2", rev1["revision"])
	}
	if rev2["revision"].(float64) != 1 {
		t.Errorf("ref2 (no-op) revision = %v, want unchanged 1", rev2["revision"])
	}

	_, hist1 := doJSON(t, "GET", srv.URL+"/reservations/"+ref1+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if len(hist1["entries"].([]interface{})) != 2 {
		t.Errorf("ref1 history = %v, want 2 entries (created, changed)", hist1["entries"])
	}
	_, hist2 := doJSON(t, "GET", srv.URL+"/reservations/"+ref2+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if len(hist2["entries"].([]interface{})) != 1 {
		t.Errorf("ref2 (no-op leg) history = %v, want still 1 entry (no change recorded)", hist2["entries"])
	}
}

func TestMovesBatchTouchingSeriesOccurrenceMarksExceptionAndBumpsSeriesRevision(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, movesPolicyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_m", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)
	occ1Ref := seriesOut["occurrences"].([]interface{})[1].(map[string]interface{})["reference"].(string)

	resp, out := doJSON(t, "POST", srv.URL+"/reservation-moves",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "move-series"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": occ1Ref, "table_id": "t_2"},
		}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("moves batch touching series occurrence = %d %v", resp.StatusCode, out)
	}

	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s2["revision"].(float64) != 2 {
		t.Errorf("series revision after moves batch = %v, want 2", s2["revision"])
	}
	occs := s2["occurrences"].([]interface{})
	if occs[1].(map[string]interface{})["exception"] != true {
		t.Errorf("occurrence touched by moves batch exception = %v, want true", occs[1])
	}
}

func TestMovesBatchFailureLeavesEverythingUnchanged(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, movesPolicyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, r1 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_m", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref1 := r1["reference"].(string)
	_, r2 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k2"},
		map[string]interface{}{"restaurant_id": "r_m", "table_id": "t_2", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref2 := r2["reference"].(string)

	// Try to move ref1 onto t_2 at the same time as ref2 -- must fail the whole batch.
	resp, out := doJSON(t, "POST", srv.URL+"/reservation-moves",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "move-fail"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": ref1, "table_id": "t_2"},
		}})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting move = %d %v, want 409", resp.StatusCode, out)
	}

	_, get1 := doJSON(t, "GET", srv.URL+"/reservations/"+ref1, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if get1["table_id"] != "t_1" || get1["revision"].(float64) != 1 {
		t.Errorf("ref1 changed after a failed batch: %v", get1)
	}
	_, hist1 := doJSON(t, "GET", srv.URL+"/reservations/"+ref1+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if len(hist1["entries"].([]interface{})) != 1 {
		t.Errorf("ref1 history after a failed batch = %v, want still 1", hist1["entries"])
	}
	_ = ref2
}
