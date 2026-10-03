package httpapi

import (
	"net/http"
	"sync"
	"testing"
)

// TestConcurrentBookingSameTableOnlyOneWins proves, with real concurrent HTTP requests (not
// code inspection), that of two simultaneous bookings for the same table/overlapping time, at
// most one results in a confirmed reservation.
func TestConcurrentBookingSameTableOnlyOneWins(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	const n = 12
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := doJSON(t, "POST", srv.URL+"/reservations",
				map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "race-" + itoa(i)},
				map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	wins, conflicts := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			wins++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("unexpected status %d", s)
		}
	}
	if wins != 1 {
		t.Errorf("wins = %d, want exactly 1 (statuses=%v)", wins, statuses)
	}
	if wins+conflicts != n {
		t.Errorf("wins+conflicts = %d, want %d", wins+conflicts, n)
	}
}

// TestConcurrentIdenticalIdempotentRequestsExactlyOne201 proves that concurrent requests
// sharing the same unused idempotency key and body produce exactly one 201 and the rest 200
// with the identical body, and that the booking is created only once.
func TestConcurrentIdenticalIdempotentRequestsExactlyOne201(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	const n = 10
	var wg sync.WaitGroup
	statuses := make([]int, n)
	refs := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, out := doJSON(t, "POST", srv.URL+"/reservations",
				map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "shared-key"},
				map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
			statuses[i] = resp.StatusCode
			if ref, ok := out["reference"].(string); ok {
				refs[i] = ref
			}
		}(i)
	}
	wg.Wait()

	created, ok := 0, 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			created++
		} else if s == http.StatusOK {
			ok++
		} else {
			t.Errorf("unexpected status %d", s)
		}
	}
	if created != 1 {
		t.Errorf("created = %d, want 1 (statuses=%v)", created, statuses)
	}
	if created+ok != n {
		t.Errorf("created+ok = %d, want %d", created+ok, n)
	}
	first := refs[0]
	for _, r := range refs {
		if r != first {
			t.Errorf("refs differ across concurrent replays: %v", refs)
			break
		}
	}

	respList, outList := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token}, nil)
	if respList.StatusCode != http.StatusOK {
		t.Fatalf("list = %d", respList.StatusCode)
	}
	list := outList["reservations"].([]interface{})
	if len(list) != 1 {
		t.Errorf("exactly one reservation should have been created, got %d", len(list))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
