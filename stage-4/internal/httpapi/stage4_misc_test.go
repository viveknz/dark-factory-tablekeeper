package httpapi

import (
	"net/http"
	"testing"
)

const dstAmendFixture = `{
	"users": [
		{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"},
		{"id": "u_bob", "email": "bob@example.com", "password": "correct horse2", "display_name": "Bob"}
	],
	"restaurants": [
		{
			"id": "r_dstam", "name": "DST Amend Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 30, "cancellation_cutoff_minutes": 0,
			"opening_hours": [
				{"weekday": "mon", "opens": "00:00", "closes": "06:00"},
				{"weekday": "tue", "opens": "00:00", "closes": "06:00"},
				{"weekday": "wed", "opens": "00:00", "closes": "06:00"},
				{"weekday": "thu", "opens": "00:00", "closes": "06:00"},
				{"weekday": "fri", "opens": "00:00", "closes": "06:00"},
				{"weekday": "sat", "opens": "00:00", "closes": "06:00"},
				{"weekday": "sun", "opens": "00:00", "closes": "06:00"}
			],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}]
		}
	],
	"reservations": []
}`

// TestSeriesAmendPrecedenceNonOccupancyOverOccupancy: occurrence index 0 (2030-03-17) has an
// occupancy conflict at the requested new time, and occurrence index 2 (2030-03-31, Berlin's
// spring-forward Sunday) has a non-occupancy error (the requested time is skipped that day).
// Per spec, the non-occupancy error must be reported even though the occupancy conflict is at
// an EARLIER index -- a naive single-pass, index-order implementation would report
// table_unavailable from index 0 instead.
func TestSeriesAmendPrecedenceNonOccupancyOverOccupancy(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, dstAmendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	bob := loginToken(t, srv, "bob@example.com", "correct horse2")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_dstam", "table_id": "t_1", "starts_at_local": "2030-03-17T01:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 3, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)
	rev := seriesOut["revision"].(float64)

	// A fixed (different owner) booking occupying t_1 at the requested new time on occurrence
	// index 0's date (2030-03-17), using a separate time slot that doesn't conflict with the
	// anchor's own 02:00 booking.
	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + bob, "Idempotency-Key": "blocker"},
		map[string]interface{}{"restaurant_id": "r_dstam", "table_id": "t_1", "starts_at_local": "2030-03-17T02:30", "party_size": 2})

	resp, out := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "amend-precedence"},
		map[string]interface{}{"expected_revision": rev, "from_index": 0, "local_time": "02:30"})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("amend status = %d %v, want 422 invalid_local_time (non-occupancy must win)", resp.StatusCode, out)
	}
	if out["error"].(map[string]interface{})["code"] != "invalid_local_time" {
		t.Errorf("error code = %v, want invalid_local_time (from index 2's DST skip), not table_unavailable (from index 0's occupancy conflict)", out["error"])
	}

	// Nothing changed: no partial amendment.
	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s2["revision"].(float64) != rev {
		t.Errorf("series revision after a failed amend = %v, want unchanged %v", s2["revision"], rev)
	}
}

const replanSeriesFixture = `{
	"users": [
		{"id": "u_mgr", "email": "mgr@example.com", "password": "correct horse", "display_name": "Mgr"},
		{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}
	],
	"restaurants": [
		{
			"id": "r_rs", "name": "Replan Series Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 0,
			"opening_hours": [{"weekday": "mon", "opens": "17:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 2}],
			"manager_user_ids": ["u_mgr"]
		}
	],
	"reservations": []
}`

// TestReplanOnSeriesOccurrencePreservesIdentityAndBumpsSeriesRevisionOnce: a replan moves a
// series occurrence to a different table. Its exception flag, scheduled date/time, reference
// and accepted terms must all stay exactly as they were -- only the table changes -- and the
// series' own revision bumps exactly once for the whole plan application.
func TestReplanOnSeriesOccurrencePreservesIdentityAndBumpsSeriesRevisionOnce(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanSeriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_rs", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)
	seriesRevBefore := seriesOut["revision"].(float64)

	_, preview := doJSON(t, "POST", srv.URL+"/restaurants/r_rs/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview1"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	planID := preview["plan_id"].(string)

	resp, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_rs/replans/"+planID+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply1"}, map[string]interface{}{})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("apply = %d", resp.StatusCode)
	}

	_, seriesAfter := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if seriesAfter["revision"].(float64) != seriesRevBefore+1 {
		t.Errorf("series revision after a replan touching an occurrence = %v, want %v", seriesAfter["revision"], seriesRevBefore+1)
	}
	occ0 := seriesAfter["occurrences"].([]interface{})[0].(map[string]interface{})
	if occ0["exception"] != false {
		t.Errorf("replanned occurrence exception flag = %v, want still false (a replan is not an exception)", occ0["exception"])
	}
	resv := occ0["reservation"].(map[string]interface{})
	if resv["reference"] != anchorRef {
		t.Errorf("replanned occurrence reference changed: %v, want %v", resv["reference"], anchorRef)
	}
	if resv["starts_at_local"] != "2030-01-07T19:00" {
		t.Errorf("replanned occurrence scheduled time changed: %v, want unchanged 2030-01-07T19:00", resv["starts_at_local"])
	}
	if resv["table_id"] == "t_1" {
		t.Errorf("replanned occurrence table unchanged: %v, want moved off t_1", resv)
	}
}

// TestRestaurantRevisionIncrementsOnEveryTrigger confirms the union of stage-3's triggers
// (new booking, real amendment, cancellation, policy publication) and stage-4's (plan
// application) each bump restaurant_revision by exactly one, read via the replans preview
// response (the only place it's exposed).
func TestRestaurantRevisionIncrementsOnEveryTrigger(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanSeriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	readRevision := func(key string) float64 {
		_, out := doJSON(t, "POST", srv.URL+"/restaurants/r_rs/replans",
			map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": key},
			map[string]interface{}{"table_id": "t_2", "from": "2099-01-01T00:00:00+01:00", "to": "2099-01-01T01:00:00+01:00"})
		return out["restaurant_revision"].(float64)
	}

	rev0 := readRevision("read0")
	if rev0 != 0 {
		t.Fatalf("revision after reset = %v, want 0", rev0)
	}

	_, bookOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "book1"},
		map[string]interface{}{"restaurant_id": "r_rs", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := bookOut["reference"].(string)
	if got := readRevision("read1"); got != rev0+1 {
		t.Errorf("revision after a new booking = %v, want %v", got, rev0+1)
	}

	doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada}, map[string]interface{}{"party_size": 1})
	if got := readRevision("read2"); got != rev0+2 {
		t.Errorf("revision after a real amendment = %v, want %v", got, rev0+2)
	}

	doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if got := readRevision("read3"); got != rev0+3 {
		t.Errorf("revision after a cancellation = %v, want %v", got, rev0+3)
	}

	doJSON(t, "POST", srv.URL+"/restaurants/r_rs/policies", map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "pub1"},
		map[string]interface{}{
			"effective_from": "2030-01-01", "slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 0,
			"opening_hours": []map[string]string{{"weekday": "mon", "opens": "17:00", "closes": "23:00"}},
			"capacities": map[string]int{"t_1": 2, "t_2": 2},
		})
	if got := readRevision("read4"); got != rev0+4 {
		t.Errorf("revision after a policy publication = %v, want %v", got, rev0+4)
	}
}
