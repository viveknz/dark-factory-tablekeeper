package httpapi

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestResetRejectsIDsLongerThan64Chars(t *testing.T) {
	srv, _ := newTestServer(t)
	longID := strings.Repeat("u", 65)
	body := `{"users":[{"id":"` + longID + `","email":"a@example.com","password":"correct horse","display_name":"A"}],"restaurants":[],"reservations":[]}`
	resp, err := http.Post(srv.URL+"/_test/reset", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestResetAcceptsSeededTableIDsAndCancelledStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, `{
		"users": [{"id":"u_x","email":"x@example.com","password":"correct horse","display_name":"X"}],
		"restaurants": [{
			"id":"r_x","name":"X","timezone":"Europe/Berlin","slot_minutes":30,
			"reservation_duration_minutes":90,"cancellation_cutoff_minutes":120,
			"opening_hours":[{"weekday":"mon","opens":"18:00","closes":"23:00"}],
			"tables":[{"id":"t_1","label":"1","capacity":2},{"id":"t_2","label":"2","capacity":4}],
			"combinable":[["t_1","t_2"]]
		}],
		"reservations": [
			{"id":"res_combo","reference":"COMBOREF1","user_id":"u_x","restaurant_id":"r_x","table_ids":["t_1","t_2"],
			 "party_size":5,"starts_at_local":"2027-01-04T19:00"},
			{"id":"res_cancelled","reference":"CANCELREF","user_id":"u_x","restaurant_id":"r_x","table_id":"t_1",
			 "party_size":2,"starts_at_local":"2027-01-11T19:00","status":"cancelled"}
		]
	}`)
	token := loginToken(t, srv, "x@example.com", "correct horse")

	resp, out := doJSON(t, "GET", srv.URL+"/reservations/COMBOREF1", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get combo seeded reservation = %d", resp.StatusCode)
	}
	ids, ok := out["table_ids"].([]interface{})
	if !ok || len(ids) != 2 || ids[0] != "t_1" || ids[1] != "t_2" {
		t.Errorf("table_ids = %v, want [t_1 t_2]", out["table_ids"])
	}
	if out["status"] != "confirmed" {
		t.Errorf("status = %v, want confirmed (default)", out["status"])
	}

	resp2, out2 := doJSON(t, "GET", srv.URL+"/reservations/CANCELREF", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get cancelled seeded reservation = %d", resp2.StatusCode)
	}
	if out2["status"] != "cancelled" {
		t.Errorf("status = %v, want cancelled", out2["status"])
	}

	// A seeded-cancelled reservation's table must be free immediately: a new booking of t_1
	// alone at the same slot must succeed.
	resp3, out3 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_x", "table_id": "t_1", "starts_at_local": "2027-01-11T19:00", "party_size": 2})
	if resp3.StatusCode != http.StatusCreated {
		t.Fatalf("booking over a seeded-cancelled reservation's table = %d %v, want 201", resp3.StatusCode, out3)
	}
}

func TestResetRejectsInvalidReservationReferences(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, ref := range []string{"x", "lower01", "TOO-LONG-WITH-DASH"} {
		body := `{"users":[],"restaurants":[{"id":"r1","name":"R","timezone":"Europe/Berlin","slot_minutes":30,
			"reservation_duration_minutes":90,"cancellation_cutoff_minutes":120,
			"opening_hours":[{"weekday":"mon","opens":"18:00","closes":"23:00"}],
			"tables":[{"id":"t1","label":"1","capacity":2}]}],
			"reservations":[{"id":"res1","reference":"` + ref + `","user_id":"u1","restaurant_id":"r1","table_id":"t1","party_size":2,"starts_at_local":"2027-01-04T19:00"}]}`
		resp, err := http.Post(srv.URL+"/_test/reset", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatalf("reset: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("reference=%q status=%d, want 422", ref, resp.StatusCode)
		}
	}
}
