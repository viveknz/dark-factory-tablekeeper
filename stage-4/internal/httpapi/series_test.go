package httpapi

import (
	"net/http"
	"testing"
)

const seriesFixture = `{
	"users": [
		{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"},
		{"id": "u_bob", "email": "bob@example.com", "password": "correct horse2", "display_name": "Bob"}
	],
	"restaurants": [
		{
			"id": "r_s", "name": "Series Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 60,
			"opening_hours": [{"weekday": "mon", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}]
		}
	],
	"reservations": []
}`

func TestSeriesAdoptionHappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)

	resp, out := doJSON(t, "POST", srv.URL+"/series",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 4, "interval_weeks": 1})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create series = %d %v", resp.StatusCode, out)
	}
	if out["revision"].(float64) != 1 {
		t.Errorf("series revision = %v, want 1", out["revision"])
	}
	occs := out["occurrences"].([]interface{})
	if len(occs) != 4 {
		t.Fatalf("occurrences = %v, want 4", occs)
	}
	occ0 := occs[0].(map[string]interface{})
	if occ0["reference"] != anchorRef {
		t.Errorf("occurrence 0 reference = %v, want anchor %v (unchanged)", occ0["reference"], anchorRef)
	}
	if occ0["exception"] != false {
		t.Errorf("occurrence 0 exception = %v, want false", occ0["exception"])
	}
	wantDates := []string{"2030-01-07T19:00", "2030-01-14T19:00", "2030-01-21T19:00", "2030-01-28T19:00"}
	for i, occI := range occs {
		occ := occI.(map[string]interface{})
		if occ["index"].(float64) != float64(i) {
			t.Errorf("occurrence %d index = %v", i, occ["index"])
		}
		resv := occ["reservation"].(map[string]interface{})
		if resv["starts_at_local"] != wantDates[i] {
			t.Errorf("occurrence %d starts_at_local = %v, want %v", i, resv["starts_at_local"], wantDates[i])
		}
		if resv["party_size"].(float64) != 2 {
			t.Errorf("occurrence %d party_size = %v, want 2 (anchor's)", i, resv["party_size"])
		}
	}
	// Occurrences appear in the owner's ordinary reservation list.
	_, listOut := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if len(listOut["reservations"].([]interface{})) != 4 {
		t.Errorf("reservation list = %v, want 4 entries", listOut["reservations"])
	}
	// Each occurrence has its own ordinary history.
	occ3Ref := occs[3].(map[string]interface{})["reference"].(string)
	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+occ3Ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if len(hist["entries"].([]interface{})) != 1 {
		t.Errorf("occurrence 3 history = %v, want 1 created entry", hist["entries"])
	}
}

func TestSeriesAnchorRejectionCases(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	bob := loginToken(t, srv, "bob@example.com", "correct horse2")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)

	// Unknown reference -> 404.
	r1, _ := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-unknown"},
		map[string]interface{}{"anchor_reference": "NOPE0000", "count": 2, "interval_weeks": 1})
	if r1.StatusCode != http.StatusNotFound {
		t.Errorf("unknown anchor = %d, want 404", r1.StatusCode)
	}
	// Other owner's anchor -> 404.
	r2, _ := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + bob, "Idempotency-Key": "s-otherowner"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	if r2.StatusCode != http.StatusNotFound {
		t.Errorf("other owner anchor = %d, want 404", r2.StatusCode)
	}
	// count/interval_weeks invalid -> 422.
	for _, body := range []map[string]interface{}{
		{"anchor_reference": anchorRef, "count": 1, "interval_weeks": 1},
		{"anchor_reference": anchorRef, "count": 13, "interval_weeks": 1},
		{"anchor_reference": anchorRef, "count": true, "interval_weeks": 1},
		{"anchor_reference": anchorRef, "count": 4, "interval_weeks": 0},
		{"anchor_reference": anchorRef, "count": 4, "interval_weeks": 5},
		{"anchor_reference": anchorRef, "count": 4, "interval_weeks": false},
	} {
		r3, o3 := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-badval-" + body["anchor_reference"].(string)},
			body)
		if r3.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("invalid count/interval body=%v status=%d %v, want 422", body, r3.StatusCode, o3)
		}
	}
	// No token -> 401.
	r4, _ := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Idempotency-Key": "s-noauth"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	if r4.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", r4.StatusCode)
	}

	// Cancelled anchor -> 409 reservation_cancelled.
	_, cancelOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor2"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-14T19:00", "party_size": 2})
	cancelRef := cancelOut["reference"].(string)
	doJSON(t, "POST", srv.URL+"/reservations/"+cancelRef+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)
	r5, o5 := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-cancelled"},
		map[string]interface{}{"anchor_reference": cancelRef, "count": 2, "interval_weeks": 1})
	if r5.StatusCode != http.StatusConflict || o5["error"].(map[string]interface{})["code"] != "reservation_cancelled" {
		t.Errorf("cancelled anchor = %d %v, want 409 reservation_cancelled", r5.StatusCode, o5)
	}

	// Already-in-series anchor -> 409 already_in_series.
	r6, o6 := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-first"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	if r6.StatusCode != http.StatusCreated {
		t.Fatalf("first adoption = %d %v", r6.StatusCode, o6)
	}
	r7, o7 := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-second"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	if r7.StatusCode != http.StatusConflict || o7["error"].(map[string]interface{})["code"] != "already_in_series" {
		t.Errorf("already-in-series = %d %v, want 409 already_in_series", r7.StatusCode, o7)
	}
}

func TestSeriesAllOrNothingOnMidSeriesFailure(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)

	// Pre-book occurrence index 2's date (2030-01-21) directly under another user, so the
	// adoption's third occurrence collides and the whole thing must fail with nothing created.
	bobToken := loginToken(t, srv, "bob@example.com", "correct horse2")
	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + bobToken, "Idempotency-Key": "blocker"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-21T19:00", "party_size": 2})

	resp, out := doJSON(t, "POST", srv.URL+"/series",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-fail"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 4, "interval_weeks": 1})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("mid-series failure status = %d %v, want 409 table_unavailable", resp.StatusCode, out)
	}

	// Nothing was created: anchor is still not in any series, and the owner's list still has
	// only the anchor and no stray occurrences.
	_, listOut := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if len(listOut["reservations"].([]interface{})) != 1 {
		t.Errorf("reservation list after failed adoption = %v, want only the anchor", listOut["reservations"])
	}
	// The idempotency key must not be claimed either: retrying with a fixed slot should succeed.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/series",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-retry"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("retry after failed adoption = %d %v", resp2.StatusCode, out2)
	}
}

func TestSeriesGetOwnerOnly404(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	bob := loginToken(t, srv, "bob@example.com", "correct horse2")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)

	r1, _ := doJSON(t, "GET", srv.URL+"/series/"+seriesID, nil, nil)
	if r1.StatusCode != http.StatusNotFound {
		t.Errorf("no token = %d, want 404", r1.StatusCode)
	}
	r2, _ := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + bob}, nil)
	if r2.StatusCode != http.StatusNotFound {
		t.Errorf("other user = %d, want 404", r2.StatusCode)
	}
	r3, _ := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if r3.StatusCode != http.StatusOK {
		t.Errorf("owner = %d, want 200", r3.StatusCode)
	}
}

func TestSeriesOccurrencePatchMarksExceptionAndBumpsSeriesRevision(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 3, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)
	occ1Ref := seriesOut["occurrences"].([]interface{})[1].(map[string]interface{})["reference"].(string)

	// A no-op PATCH on occurrence 1 changes nothing.
	doJSON(t, "PATCH", srv.URL+"/reservations/"+occ1Ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"party_size": 2})
	_, s1 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s1["revision"].(float64) != 1 {
		t.Errorf("series revision after no-op PATCH = %v, want unchanged 1", s1["revision"])
	}

	// A real PATCH on occurrence 1 marks it an exception and bumps the series revision once.
	resp, _ := doJSON(t, "PATCH", srv.URL+"/reservations/"+occ1Ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"party_size": 1})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("real patch = %d", resp.StatusCode)
	}
	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s2["revision"].(float64) != 2 {
		t.Errorf("series revision after real PATCH = %v, want 2", s2["revision"])
	}
	occs := s2["occurrences"].([]interface{})
	if occs[1].(map[string]interface{})["exception"] != true {
		t.Errorf("occurrence 1 exception = %v, want true", occs[1])
	}
	if occs[0].(map[string]interface{})["exception"] != false || occs[2].(map[string]interface{})["exception"] != false {
		t.Errorf("only occurrence 1 should be an exception: %v", occs)
	}
}

func TestSeriesCancelOccurrenceBumpsRevisionNotException(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 3, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)
	occ1Ref := seriesOut["occurrences"].([]interface{})[1].(map[string]interface{})["reference"].(string)

	doJSON(t, "POST", srv.URL+"/reservations/"+occ1Ref+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)
	// Repeated cancel does nothing further.
	doJSON(t, "POST", srv.URL+"/reservations/"+occ1Ref+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)

	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s2["revision"].(float64) != 2 {
		t.Errorf("series revision after cancel+repeat-cancel = %v, want 2 (bumped once)", s2["revision"])
	}
	occs := s2["occurrences"].([]interface{})
	occ1 := occs[1].(map[string]interface{})
	if occ1["exception"] != false {
		t.Errorf("cancelled occurrence exception = %v, want false", occ1["exception"])
	}
	if occ1["reservation"].(map[string]interface{})["status"] != "cancelled" {
		t.Errorf("occurrence 1 status = %v, want cancelled (retained in list)", occ1["reservation"])
	}
	if len(occs) != 3 {
		t.Errorf("occurrences = %v, want still 3 (cancelled one retained)", occs)
	}
}

func TestSeriesCancelAnchorDoesNotCancelSiblings(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 3, "interval_weeks": 1})
	seriesID := seriesOut["series_id"].(string)

	doJSON(t, "POST", srv.URL+"/reservations/"+anchorRef+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)

	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	occs := s2["occurrences"].([]interface{})
	if occs[0].(map[string]interface{})["reservation"].(map[string]interface{})["status"] != "cancelled" {
		t.Errorf("anchor should be cancelled: %v", occs[0])
	}
	for i := 1; i < 3; i++ {
		if occs[i].(map[string]interface{})["reservation"].(map[string]interface{})["status"] != "confirmed" {
			t.Errorf("occurrence %d should remain confirmed after anchor cancel: %v", i, occs[i])
		}
	}
}

func TestSeriesReplayReturnsOriginalForever(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, seriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_s", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	body := map[string]interface{}{"anchor_reference": anchorRef, "count": 3, "interval_weeks": 1}
	_, out1 := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "replaykey"}, body)
	seriesID := out1["series_id"].(string)

	// Mutate the series afterward (cancel an occurrence, bumping its revision).
	occ1Ref := out1["occurrences"].([]interface{})[1].(map[string]interface{})["reference"].(string)
	doJSON(t, "POST", srv.URL+"/reservations/"+occ1Ref+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)

	// Replay the original POST /series request: must return the ORIGINAL response (revision 1),
	// not the series' current state, and must not bump anything.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/series", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "replaykey"}, body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("replay = %d %v", resp2.StatusCode, out2)
	}
	if out2["revision"].(float64) != 1 {
		t.Errorf("replay revision = %v, want original 1", out2["revision"])
	}
	if out2["series_id"] != seriesID {
		t.Errorf("replay series_id = %v, want %v", out2["series_id"], seriesID)
	}

	_, s3 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s3["revision"].(float64) != 2 {
		t.Errorf("current series revision = %v, want 2 (replay must not touch it)", s3["revision"])
	}
}
