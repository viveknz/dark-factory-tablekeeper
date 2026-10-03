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
