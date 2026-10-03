package httpapi

import (
	"net/http"
	"sync"
	"testing"
)

const revisionFixture = `{
	"users": [{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}],
	"restaurants": [
		{
			"id": "r_rev", "name": "Revision Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 60,
			"opening_hours": [{"weekday": "mon", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 2},
			           {"id": "t_3", "label": "3", "capacity": 2}, {"id": "t_4", "label": "4", "capacity": 2}]
		}
	],
	"reservations": []
}`

func TestExpectedRevisionStaleAndTypeChecks(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, revisionFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_rev", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := out["reference"].(string)

	// Mismatched positive integer -> 409 stale_revision, BEFORE cutoff/validation would even
	// be checked (party_size here is deliberately invalid too, to prove ordering).
	resp, outErr := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"expected_revision": 99, "party_size": 999})
	if resp.StatusCode != http.StatusConflict || outErr["error"].(map[string]interface{})["code"] != "stale_revision" {
		t.Errorf("mismatched expected_revision = %d %v, want 409 stale_revision", resp.StatusCode, outErr)
	}

	// Wrong type / out of range -> 422.
	for _, bad := range []interface{}{true, 0, -1, "1", 1.5} {
		r2, o2 := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
			map[string]interface{}{"expected_revision": bad, "party_size": 2})
		if r2.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected_revision=%v status = %d %v, want 422", bad, r2.StatusCode, o2)
		}
	}

	// Matching revision -> succeeds normally.
	resp3, out3 := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"expected_revision": 1, "party_size": 2, "table_id": "t_2"})
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("matching expected_revision = %d %v", resp3.StatusCode, out3)
	}
	if out3["revision"].(float64) != 2 {
		t.Errorf("revision after amendment = %v, want 2", out3["revision"])
	}

	// Omitted -> ordinary stage-1 semantics, no revision check at all (an arbitrary, now-stale
	// revision from before this amendment would have failed; omitting it just works).
	resp4, out4 := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"table_id": "t_3"})
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("omitted expected_revision = %d %v", resp4.StatusCode, out4)
	}
}

// TestConcurrentAmendmentsSameRevisionOnlyOneWins proves, with real concurrent HTTP requests
// (not a code read), that of two concurrent amendments both starting from the same revision,
// at most one real change succeeds -- the same bar stage 1 set for the booking race.
func TestConcurrentAmendmentsSameRevisionOnlyOneWins(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, revisionFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_rev", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := out["reference"].(string)
	startRevision := out["revision"].(float64)

	const n = 12
	var wg sync.WaitGroup
	results := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			table := []string{"t_2", "t_3", "t_4", "t_2"}[i%4]
			resp, _ := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
				map[string]interface{}{"expected_revision": int(startRevision), "table_id": table})
			results[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, status := range results {
		if status == http.StatusOK {
			wins++
		} else if status != http.StatusConflict {
			t.Errorf("unexpected status %d (want 200 or 409 stale_revision)", status)
		}
	}
	if wins != 1 {
		t.Errorf("concurrent amendments racing on revision %v: %d succeeded, want exactly 1", startRevision, wins)
	}

	_, final := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if final["revision"].(float64) != startRevision+1 {
		t.Errorf("final revision = %v, want %v (bumped exactly once)", final["revision"], startRevision+1)
	}
}
