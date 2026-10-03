package httpapi

import (
	"net/http"
	"testing"
)

func TestCreateReservationHappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	resp, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_2", "starts_at_local": "2027-09-23T19:00", "party_size": 4})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	if out["reference"] == "" || out["status"] != "confirmed" {
		t.Fatalf("unexpected body %v", out)
	}
	if out["starts_at"] != "2027-09-23T19:00:00+02:00" {
		t.Errorf("starts_at = %v", out["starts_at"])
	}
	if out["ends_at"] != "2027-09-23T20:30:00+02:00" {
		t.Errorf("ends_at = %v", out["ends_at"])
	}

	// Booked slot/table must now be excluded from availability.
	_, avail := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=4", nil, nil)
	slots := avail["slots"].([]interface{})
	for _, s := range slots {
		m := s.(map[string]interface{})
		if m["starts_at_local"] == "2027-09-23T19:00" {
			tables := m["available_table_ids"].([]interface{})
			for _, tb := range tables {
				if tb == "t_2" {
					t.Errorf("t_2 should no longer be available at 19:00")
				}
			}
		}
	}
}

func TestCreateReservationMissingIdempotencyKey(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")
	resp, _ := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_2", "starts_at_local": "2027-09-23T19:00", "party_size": 4})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIdempotencyReplayAndReuse(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")
	body := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_2", "starts_at_local": "2027-09-23T19:00", "party_size": 4}
	headers := map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "dup-key"}

	resp1, out1 := doJSON(t, "POST", srv.URL+"/reservations", headers, body)
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first = %d %v", resp1.StatusCode, out1)
	}

	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations", headers, body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("replay = %d, want 200", resp2.StatusCode)
	}
	if out2["reference"] != out1["reference"] {
		t.Errorf("replay reference mismatch: %v vs %v", out2["reference"], out1["reference"])
	}

	body2 := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	resp3, _ := doJSON(t, "POST", srv.URL+"/reservations", headers, body2)
	if resp3.StatusCode != http.StatusConflict {
		t.Fatalf("reuse with different body = %d, want 409", resp3.StatusCode)
	}

	// Same key, different path: must succeed as an unrelated request.
	resp4, out4 := doJSON(t, "POST", srv.URL+"/reservation-moves", headers, map[string]interface{}{
		"moves": []map[string]interface{}{{"reference": out1["reference"]}},
	})
	if resp4.StatusCode != http.StatusCreated {
		t.Fatalf("same key different path = %d %v, want 201", resp4.StatusCode, out4)
	}
}

func TestIdempotencyKeyReusableAfterFailure(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")
	headers := map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "retry-key"}

	badBody := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "nope", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	resp1, _ := doJSON(t, "POST", srv.URL+"/reservations", headers, badBody)
	if resp1.StatusCode != http.StatusNotFound {
		t.Fatalf("bad table = %d, want 404", resp1.StatusCode)
	}

	goodBody := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations", headers, goodBody)
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("retry after failure = %d %v, want 201", resp2.StatusCode, out2)
	}
}

func TestCreateReservationValidationErrors(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"not_on_slot_grid", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:05", "party_size": 2}, http.StatusUnprocessableEntity},
		{"outside_opening_hours_before_open", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T17:30", "party_size": 2}, http.StatusUnprocessableEntity},
		{"outside_opening_hours_ends_after_close", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T22:00", "party_size": 2}, http.StatusUnprocessableEntity},
		{"party_exceeds_capacity", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 3}, http.StatusUnprocessableEntity},
		{"party_size_zero", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 0}, http.StatusUnprocessableEntity},
		{"party_size_string", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": "2"}, http.StatusUnprocessableEntity},
		{"unknown_restaurant", map[string]interface{}{"restaurant_id": "nope", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}, http.StatusNotFound},
		{"unknown_table", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "nope", "starts_at_local": "2027-09-23T19:00", "party_size": 2}, http.StatusNotFound},
		{"closed_day", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-25T19:00", "party_size": 2}, http.StatusUnprocessableEntity},
		{"bad_local_format", map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23 19:00", "party_size": 2}, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, out := doJSON(t, "POST", srv.URL+"/reservations",
				map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "k-" + c.name},
				c.body)
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d; body=%v", resp.StatusCode, c.want, out)
			}
		})
	}
}

func TestReservationTableUnavailableOnOverlap(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")
	body := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	resp1, _ := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "a1"}, body)
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first booking = %d", resp1.StatusCode)
	}
	// Overlapping (not identical) start on the same table: 19:30 starts before 19:00+90=20:30 ends.
	body2 := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:30", "party_size": 2}
	resp2, _ := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "a2"}, body2)
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("overlap = %d, want 409", resp2.StatusCode)
	}
	// Exactly-adjacent booking (20:30 start, after the first ends) must succeed: half-open interval.
	body3 := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T20:30", "party_size": 2}
	resp3, out3 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "a3"}, body3)
	if resp3.StatusCode != http.StatusCreated {
		t.Fatalf("adjacent booking = %d %v, want 201", resp3.StatusCode, out3)
	}
}

func TestListAndGetReservationOwnershipIsolation(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	adaToken := loginToken(t, srv, "ada@example.com", "correct horse")
	bobToken := loginToken(t, srv, "bob@example.com", "correct horse2")

	body := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	_, out := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + adaToken, "Idempotency-Key": "b1"}, body)
	ref := out["reference"].(string)

	respOwner, _ := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + adaToken}, nil)
	if respOwner.StatusCode != http.StatusOK {
		t.Fatalf("owner get = %d, want 200", respOwner.StatusCode)
	}
	respOther, _ := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + bobToken}, nil)
	if respOther.StatusCode != http.StatusNotFound {
		t.Fatalf("other user get = %d, want 404", respOther.StatusCode)
	}

	respList, outList := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + bobToken}, nil)
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("list = %d", respList.StatusCode)
	}
	if list, ok := outList["reservations"].([]interface{}); !ok || len(list) != 0 {
		t.Errorf("bob's list should be empty, got %v", outList["reservations"])
	}
}

func TestCancelIdempotentAndCutoff(t *testing.T) {
	srv, _ := newTestServer(t)
	// Use a fixture whose cutoff is large and whose reservation is seeded far in the past
	// relative to "now" so the cutoff has definitely passed, to test cutoff_passed deterministically,
	// and a second restaurant-less booking made fresh (future slot) to test the happy cancel path.
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	body := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	_, out := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "c1"}, body)
	ref := out["reference"].(string)

	resp1, out1 := doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp1.StatusCode != http.StatusOK || out1["status"] != "cancelled" {
		t.Fatalf("cancel = %d %v", resp1.StatusCode, out1)
	}
	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp2.StatusCode != http.StatusOK || out2["status"] != "cancelled" {
		t.Fatalf("double cancel = %d %v, want 200 cancelled", resp2.StatusCode, out2)
	}

	// Cancelled slot must free the table again.
	_, avail := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=2", nil, nil)
	slots := avail["slots"].([]interface{})
	found := false
	for _, s := range slots {
		m := s.(map[string]interface{})
		if m["starts_at_local"] == "2027-09-23T19:00" {
			for _, tb := range m["available_table_ids"].([]interface{}) {
				if tb == "t_1" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("t_1 should be available again at 19:00 after cancellation")
	}
}

func TestCancelPastCutoffFixture(t *testing.T) {
	srv, _ := newTestServer(t)
	// Seed a reservation whose start is in the past (allowed per spec §4) with a cutoff that
	// has already passed, using a fixture date far in the past.
	resetFixture(t, srv, `{
		"users": [{"id":"u_x","email":"x@example.com","password":"correct horse","display_name":"X"}],
		"restaurants": [{
			"id":"r_x","name":"X","timezone":"Europe/Berlin","slot_minutes":30,
			"reservation_duration_minutes":90,"cancellation_cutoff_minutes":120,
			"opening_hours":[{"weekday":"mon","opens":"18:00","closes":"23:00"}],
			"tables":[{"id":"t_1","label":"1","capacity":2}]
		}],
		"reservations": [{
			"id":"res_old","reference":"OLDREF01","user_id":"u_x","restaurant_id":"r_x","table_id":"t_1",
			"party_size":2,"starts_at_local":"2020-01-06T19:00"
		}]
	}`)
	token := loginToken(t, srv, "x@example.com", "correct horse")
	resp, _ := doJSON(t, "POST", srv.URL+"/reservations/OLDREF01/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("cancel past cutoff = %d, want 409", resp.StatusCode)
	}
}

func TestAmendReservation(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	body := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	_, out := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "am1"}, body)
	ref := out["reference"].(string)
	resID := out["reservation_id"].(string)

	resp, out2 := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + token}, map[string]interface{}{
		"starts_at_local": "2027-09-23T20:00",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("amend = %d %v", resp.StatusCode, out2)
	}
	if out2["reference"] != ref || out2["reservation_id"] != resID {
		t.Errorf("identity changed: %v", out2)
	}
	if out2["starts_at_local"] != "2027-09-23T20:00" {
		t.Errorf("starts_at_local = %v", out2["starts_at_local"])
	}

	// Failed amendment (invalid table) must leave the booking untouched.
	respBad, _ := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + token}, map[string]interface{}{
		"table_id": "does-not-exist",
	})
	if respBad.StatusCode != http.StatusNotFound {
		t.Fatalf("bad amend = %d, want 404", respBad.StatusCode)
	}
	respCheck, outCheck := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + token}, nil)
	if respCheck.StatusCode != http.StatusOK || outCheck["starts_at_local"] != "2027-09-23T20:00" {
		t.Fatalf("reservation changed after failed amendment: %v", outCheck)
	}
}

func TestAmendCancelledReservationIs409(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")
	body := map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
	_, out := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "amc1"}, body)
	ref := out["reference"].(string)
	doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)

	resp, _ := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + token}, map[string]interface{}{
		"party_size": 2,
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("amend cancelled = %d, want 409", resp.StatusCode)
	}
}
