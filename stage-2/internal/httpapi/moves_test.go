package httpapi

import (
	"net/http"
	"testing"
)

func TestReservationMovesSwapTablesAtomically(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out1 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "m1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	_, out2 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "m2"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_2", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	ref1 := out1["reference"].(string)
	ref2 := out2["reference"].(string)

	resp, out := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "move1"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": ref1, "table_id": "t_2"},
			{"reference": ref2, "table_id": "t_1"},
		}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("moves = %d %v", resp.StatusCode, out)
	}
	list := out["reservations"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("expected 2 reservations in response, got %d", len(list))
	}
	first := list[0].(map[string]interface{})
	second := list[1].(map[string]interface{})
	if first["reference"] != ref1 || first["table_id"] != "t_2" {
		t.Errorf("first move wrong: %v", first)
	}
	if second["reference"] != ref2 || second["table_id"] != "t_1" {
		t.Errorf("second move wrong: %v", second)
	}

	// Replay with the same key must return 200 with the same result.
	resp2, out2b := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "move1"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": ref1, "table_id": "t_2"},
			{"reference": ref2, "table_id": "t_1"},
		}})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("replay moves = %d, want 200", resp2.StatusCode)
	}
	_ = out2b
}

func TestReservationMovesValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")
	bobToken := loginToken(t, srv, "bob@example.com", "correct horse2")

	_, out1 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mv1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	ref1 := out1["reference"].(string)

	// Duplicate references in one request.
	resp, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mvdup"},
		map[string]interface{}{"moves": []map[string]interface{}{{"reference": ref1}, {"reference": ref1}}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("duplicate refs = %d, want 422", resp.StatusCode)
	}

	// Unknown reference.
	resp2, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mvunk"},
		map[string]interface{}{"moves": []map[string]interface{}{{"reference": "NOSUCH01"}}})
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("unknown ref = %d, want 404", resp2.StatusCode)
	}

	// Another user's reference.
	resp3, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + bobToken, "Idempotency-Key": "mvother"},
		map[string]interface{}{"moves": []map[string]interface{}{{"reference": ref1}}})
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("other owner ref = %d, want 404", resp3.StatusCode)
	}

	// No token.
	resp4, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Idempotency-Key": "mvnoauth"},
		map[string]interface{}{"moves": []map[string]interface{}{{"reference": ref1}}})
	if resp4.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", resp4.StatusCode)
	}

	// Empty moves array.
	resp5, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mvempty"},
		map[string]interface{}{"moves": []map[string]interface{}{}})
	if resp5.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("empty moves = %d, want 422", resp5.StatusCode)
	}
}

func TestReservationMovesCancelledIs409(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out1 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "cxl1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	ref1 := out1["reference"].(string)
	doJSON(t, "POST", srv.URL+"/reservations/"+ref1+"/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)

	resp, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "cxlmv"},
		map[string]interface{}{"moves": []map[string]interface{}{{"reference": ref1, "table_id": "t_2"}}})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("moves cancelled = %d, want 409", resp.StatusCode)
	}
}

func TestReservationMovesCutoffPassed(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, `{
		"users": [{"id":"u_x","email":"x@example.com","password":"correct horse","display_name":"X"}],
		"restaurants": [{
			"id":"r_x","name":"X","timezone":"Europe/Berlin","slot_minutes":30,
			"reservation_duration_minutes":90,"cancellation_cutoff_minutes":120,
			"opening_hours":[{"weekday":"mon","opens":"18:00","closes":"23:00"}],
			"tables":[{"id":"t_1","label":"1","capacity":2},{"id":"t_2","label":"2","capacity":2}]
		}],
		"reservations": [{
			"id":"res_old","reference":"OLDREF02","user_id":"u_x","restaurant_id":"r_x","table_id":"t_1",
			"party_size":2,"starts_at_local":"2020-01-06T19:00"
		}]
	}`)
	token := loginToken(t, srv, "x@example.com", "correct horse")

	// The booking's start is years in the past: its cutoff has long since passed, so a move
	// touching it must be rejected with cutoff_passed, before any field-level validation runs.
	resp, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "cutoffmv"},
		map[string]interface{}{"moves": []map[string]interface{}{{"reference": "OLDREF02", "table_id": "t_2"}}})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("moves cutoff = %d, want 409", resp.StatusCode)
	}
}

func TestReservationMovesOverlapRejectsWholeBatch(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out1 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "ov1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	_, out2 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "ov2"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_2", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	ref1 := out1["reference"].(string)
	ref2 := out2["reference"].(string)

	// Moving ref2 onto t_1 at the same time as ref1 (unlisted... actually ref1 IS listed here but
	// unchanged) must conflict since ref1 keeps t_1 at 19:00 and ref2 would also claim t_1 at 19:00.
	resp, _ := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "ovmove"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": ref1},
			{"reference": ref2, "table_id": "t_1"},
		}})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("overlapping move = %d, want 409", resp.StatusCode)
	}

	// Confirm nothing changed: ref2 still on t_2.
	respCheck, outCheck := doJSON(t, "GET", srv.URL+"/reservations/"+ref2, map[string]string{"Authorization": "Bearer " + token}, nil)
	if respCheck.StatusCode != http.StatusOK || outCheck["table_id"] != "t_2" {
		t.Fatalf("ref2 should remain on t_2 after failed batch: %v", outCheck)
	}
}
