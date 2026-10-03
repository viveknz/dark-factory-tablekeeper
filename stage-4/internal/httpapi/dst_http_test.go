package httpapi

import (
	"net/http"
	"testing"
)

// TestDSTBookingEndToEnd exercises booking through the real HTTP API across a spring-forward
// gap and a fall-back repeat, confirming the timeutil-level guarantees (already unit-tested in
// internal/timeutil) hold through the full request path including slot-grid/opening-hours
// checks and persisted occupancy.
func TestDSTBookingEndToEnd(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, `{
		"users": [{"id":"u_d","email":"d@example.com","password":"correct horse","display_name":"D"}],
		"restaurants": [{
			"id":"r_berlin","name":"Berlin","timezone":"Europe/Berlin","slot_minutes":30,
			"reservation_duration_minutes":60,"cancellation_cutoff_minutes":60,
			"opening_hours":[
				{"weekday":"sun","opens":"01:00","closes":"05:00"}
			],
			"tables":[{"id":"t_1","label":"1","capacity":2}]
		}],
		"reservations": []
	}`)
	token := loginToken(t, srv, "d@example.com", "correct horse")

	// 2026-03-29 is a Sunday: spring forward 02:00->03:00. 02:30 does not exist.
	resp, _ := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "dst1"},
		map[string]interface{}{"restaurant_id": "r_berlin", "table_id": "t_1", "starts_at_local": "2026-03-29T02:30", "party_size": 2})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("booking a skipped local time = %d, want 422", resp.StatusCode)
	}
	_, avail := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_berlin&date=2026-03-29&party_size=2", nil, nil)
	for _, s := range avail["slots"].([]interface{}) {
		m := s.(map[string]interface{})
		if m["starts_at_local"] == "2026-03-29T02:30" {
			t.Fatalf("skipped local time must never appear in availability")
		}
	}

	// 2026-10-25 is a Sunday: fall back 03:00->02:00. 02:30 occurs twice; must resolve to the
	// first (pre-transition, +02:00) occurrence and appear exactly once in availability.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "dst2"},
		map[string]interface{}{"restaurant_id": "r_berlin", "table_id": "t_1", "starts_at_local": "2026-10-25T02:30", "party_size": 2})
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("booking ambiguous local time = %d %v, want 201", resp2.StatusCode, out2)
	}
	if out2["starts_at"] != "2026-10-25T02:30:00+02:00" {
		t.Errorf("starts_at = %v, want first/pre-transition offset +02:00", out2["starts_at"])
	}
	// 60 absolute minutes after 02:30+02:00 (00:30 UTC) is 01:30 UTC, which falls after the
	// 01:00 UTC transition instant, so the post-transition +01:00 offset applies: local wall
	// clock reads 02:30 again (the fold), not 03:30 — duration is absolute time, not wall-clock.
	if out2["ends_at"] != "2026-10-25T02:30:00+01:00" {
		t.Errorf("ends_at = %v, want 2026-10-25T02:30:00+01:00 (60 real minutes later, post-transition offset)", out2["ends_at"])
	}

	_, avail2 := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_berlin&date=2026-10-25&party_size=2", nil, nil)
	count := 0
	for _, s := range avail2["slots"].([]interface{}) {
		m := s.(map[string]interface{})
		if m["starts_at_local"] == "2026-10-25T02:30" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ambiguous slot appeared %d times in availability, want exactly 1", count)
	}
}
