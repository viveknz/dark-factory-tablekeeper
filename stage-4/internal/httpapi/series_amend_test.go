package httpapi

import (
	"net/http"
	"sync"
	"testing"
)

const amendFixture = `{
	"users": [{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}],
	"restaurants": [
		{
			"id": "r_am", "name": "Amend Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 0,
			"opening_hours": [{"weekday": "mon", "opens": "17:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}]
		}
	],
	"reservations": []
}`

func setupAmendSeries(t *testing.T, srvURL, token string, count int) (seriesID string, refs []string, revision float64) {
	t.Helper()
	_, anchorOut := doJSON(t, "POST", srvURL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_am", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	anchorRef := anchorOut["reference"].(string)
	_, seriesOut := doJSON(t, "POST", srvURL+"/series", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "s1"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": count, "interval_weeks": 1})
	occs := seriesOut["occurrences"].([]interface{})
	refs = make([]string, len(occs))
	for i, o := range occs {
		refs[i] = o.(map[string]interface{})["reference"].(string)
	}
	return seriesOut["series_id"].(string), refs, seriesOut["revision"].(float64)
}

func TestSeriesAmendValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, amendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	seriesID, _, rev := setupAmendSeries(t, srv.URL, ada, 3)

	// No token -> 401.
	resp, _ := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend", map[string]string{"Idempotency-Key": "k0"},
		map[string]interface{}{"expected_revision": rev, "from_index": 0, "local_time": "20:00"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", resp.StatusCode)
	}

	hdr := map[string]string{"Authorization": "Bearer " + ada}
	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"revision_bool", map[string]interface{}{"expected_revision": true, "from_index": 0, "local_time": "20:00"}},
		{"revision_zero", map[string]interface{}{"expected_revision": 0, "from_index": 0, "local_time": "20:00"}},
		{"from_index_negative", map[string]interface{}{"expected_revision": rev, "from_index": -1, "local_time": "20:00"}},
		{"from_index_too_big", map[string]interface{}{"expected_revision": rev, "from_index": 99, "local_time": "20:00"}},
		{"from_index_bool", map[string]interface{}{"expected_revision": rev, "from_index": false, "local_time": "20:00"}},
		{"local_time_bad_format", map[string]interface{}{"expected_revision": rev, "from_index": 0, "local_time": "8:00"}},
		{"local_time_out_of_range", map[string]interface{}{"expected_revision": rev, "from_index": 0, "local_time": "24:00"}},
		{"local_time_missing", map[string]interface{}{"expected_revision": rev, "from_index": 0}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "val-" + c.name + string(rune('a'+i))}
			resp, out := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend", h, c.body)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("status = %d %v, want 422", resp.StatusCode, out)
			}
		})
	}

	// Mismatched revision -> 409 stale_revision.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "stale"},
		map[string]interface{}{"expected_revision": rev + 99, "from_index": 0, "local_time": "20:00"})
	if resp2.StatusCode != http.StatusConflict || out2["error"].(map[string]interface{})["code"] != "stale_revision" {
		t.Errorf("mismatched revision = %d %v, want 409 stale_revision", resp2.StatusCode, out2)
	}

	// Unknown series / other owner -> 404.
	hdr["Idempotency-Key"] = "unknown-series"
	resp3, _ := doJSON(t, "POST", srv.URL+"/series/ser_nope/amend", hdr, map[string]interface{}{"expected_revision": 1, "from_index": 0, "local_time": "20:00"})
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("unknown series = %d, want 404", resp3.StatusCode)
	}
}

func TestSeriesAmendShiftsFromIndexExcludesCancelledAndException(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, amendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	seriesID, refs, rev := setupAmendSeries(t, srv.URL, ada, 4)

	// Cancel occurrence 1; mark occurrence 2 an exception via a real individual PATCH.
	doJSON(t, "POST", srv.URL+"/reservations/"+refs[1]+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)
	doJSON(t, "PATCH", srv.URL+"/reservations/"+refs[2], map[string]string{"Authorization": "Bearer " + ada}, map[string]interface{}{"party_size": 1})

	resp, out := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "amend1"},
		map[string]interface{}{"expected_revision": rev + 2, "from_index": 0, "local_time": "20:00"})
	// The cancel and the PATCH-to-exception each already bumped the series revision once (rev+2).
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("amend = %d %v", resp.StatusCode, out)
	}
	occs := out["occurrences"].([]interface{})
	// Occurrence 0: shifted to 20:00.
	if occs[0].(map[string]interface{})["reservation"].(map[string]interface{})["starts_at_local"] != "2030-01-07T20:00" {
		t.Errorf("occurrence 0 = %v, want shifted to 20:00", occs[0])
	}
	// Occurrence 1: cancelled, excluded from the shift, still shows its own (unshifted) time implicitly via cancelled status.
	if occs[1].(map[string]interface{})["reservation"].(map[string]interface{})["status"] != "cancelled" {
		t.Errorf("occurrence 1 should remain cancelled: %v", occs[1])
	}
	// Occurrence 2: exception, excluded from the shift -- still at 19:00, still an exception.
	occ2 := occs[2].(map[string]interface{})
	if occ2["reservation"].(map[string]interface{})["starts_at_local"] != "2030-01-21T19:00" {
		t.Errorf("occurrence 2 (exception, excluded) = %v, want unshifted 19:00", occ2)
	}
	if occ2["exception"] != true {
		t.Errorf("occurrence 2 exception flag = %v, want still true", occ2["exception"])
	}
	// Occurrence 3: shifted to 20:00, and amend itself never marks an exception.
	occ3 := occs[3].(map[string]interface{})
	if occ3["reservation"].(map[string]interface{})["starts_at_local"] != "2030-01-28T20:00" {
		t.Errorf("occurrence 3 = %v, want shifted to 20:00", occ3)
	}
	if occ3["exception"] != false {
		t.Errorf("occurrence 3 exception flag after a series amend = %v, want false (amend never marks exception)", occ3["exception"])
	}
}

func TestSeriesAmendAllNoOpChangesNoRevisions(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, amendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	seriesID, _, rev := setupAmendSeries(t, srv.URL, ada, 2)

	// local_time identical to the existing 19:00 -> every occurrence is a no-op.
	resp, out := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "noop-amend"},
		map[string]interface{}{"expected_revision": rev, "from_index": 0, "local_time": "19:00"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("all-no-op amend = %d %v", resp.StatusCode, out)
	}
	if out["revision"].(float64) != rev {
		t.Errorf("series revision after all-no-op amend = %v, want unchanged %v", out["revision"], rev)
	}

	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s2["revision"].(float64) != rev {
		t.Errorf("live series revision after all-no-op amend = %v, want unchanged %v", s2["revision"], rev)
	}
}

func TestSeriesAmendEmptyEligibleSetSucceeds(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, amendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	seriesID, refs, rev := setupAmendSeries(t, srv.URL, ada, 2)
	doJSON(t, "POST", srv.URL+"/reservations/"+refs[1]+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)

	resp, out := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "empty-eligible"},
		map[string]interface{}{"expected_revision": rev + 1, "from_index": 1, "local_time": "20:00"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("amend with an empty eligible set (only occurrence cancelled) = %d %v", resp.StatusCode, out)
	}
	if out["revision"].(float64) != rev+1 {
		t.Errorf("series revision = %v, want unchanged at %v", out["revision"], rev+1)
	}
}

func TestSeriesAmendReplayReturnsOriginalForever(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, amendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	seriesID, refs, rev := setupAmendSeries(t, srv.URL, ada, 2)

	body := map[string]interface{}{"expected_revision": rev, "from_index": 0, "local_time": "20:00"}
	_, out1 := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "replay-key"}, body)
	if out1["revision"].(float64) != rev+1 {
		t.Fatalf("first amend revision = %v", out1["revision"])
	}

	// Cancel an occurrence afterward.
	doJSON(t, "POST", srv.URL+"/reservations/"+refs[1]+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)

	resp2, out2 := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "replay-key"}, body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("replay = %d %v", resp2.StatusCode, out2)
	}
	if out2["revision"].(float64) != rev+1 {
		t.Errorf("replay revision = %v, want original %v", out2["revision"], rev+1)
	}
}

func TestConcurrentSeriesAmendsSameRevisionOnlyOneRealChange(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, amendFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	seriesID, _, rev := setupAmendSeries(t, srv.URL, ada, 2)

	const n = 10
	var wg sync.WaitGroup
	results := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := doJSON(t, "POST", srv.URL+"/series/"+seriesID+"/amend",
				map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "concurrent-amend"},
				map[string]interface{}{"expected_revision": int(rev), "from_index": 0, "local_time": "20:00"})
			results[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, s := range results {
		if s == http.StatusCreated {
			wins++
		} else if s != http.StatusOK {
			t.Errorf("unexpected status %d", s)
		}
	}
	if wins != 1 {
		t.Errorf("concurrent amends sharing one revision: %d returned 201, want exactly 1 real change", wins)
	}
	_, s2 := doJSON(t, "GET", srv.URL+"/series/"+seriesID, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if s2["revision"].(float64) != rev+1 {
		t.Errorf("series revision after concurrent amends = %v, want exactly %v", s2["revision"], rev+1)
	}
}
