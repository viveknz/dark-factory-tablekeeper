package httpapi

import (
	"net/http"
	"sync"
	"testing"
)

func TestAvailabilityOptionsSinglesThenPairs(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)

	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=2", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	slots := out["slots"].([]interface{})
	first := slots[0].(map[string]interface{})
	opts := first["available_options"].([]interface{})
	if len(opts) != 3 {
		t.Fatalf("expected 2 singles + 1 pair = 3 options, got %d: %v", len(opts), opts)
	}
	o0 := opts[0].(map[string]interface{})
	o1 := opts[1].(map[string]interface{})
	o2 := opts[2].(map[string]interface{})
	if ids := o0["table_ids"].([]interface{}); len(ids) != 1 || ids[0] != "t_1" {
		t.Errorf("option0 = %v, want single t_1", o0)
	}
	if ids := o1["table_ids"].([]interface{}); len(ids) != 1 || ids[0] != "t_2" {
		t.Errorf("option1 = %v, want single t_2", o1)
	}
	ids2 := o2["table_ids"].([]interface{})
	if len(ids2) != 2 || ids2[0] != "t_1" || ids2[1] != "t_2" {
		t.Errorf("option2 = %v, want pair [t_1,t_2] in combinable order", o2)
	}
	if o2["capacity"] != float64(6) {
		t.Errorf("pair capacity = %v, want 6 (2+4)", o2["capacity"])
	}

	// available_table_ids is unchanged: singles only.
	avail := first["available_table_ids"].([]interface{})
	if len(avail) != 2 {
		t.Errorf("available_table_ids = %v, want 2 singles only", avail)
	}
}

func TestAvailabilityOptionsExcludesUndeclaredPair(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, `{
		"users": [],
		"restaurants": [{
			"id": "r_x", "name": "X", "timezone": "Europe/Berlin", "slot_minutes": 30,
			"reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
			"tables": [
				{"id": "t_1", "label": "1", "capacity": 2},
				{"id": "t_2", "label": "2", "capacity": 2},
				{"id": "t_3", "label": "3", "capacity": 2}
			],
			"combinable": [["t_1", "t_2"]]
		}],
		"reservations": []
	}`)
	// party_size 4 needs a combo; t_2+t_3 has equal capacity to t_1+t_2 but is NOT declared.
	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_x&date=2027-09-23&party_size=4", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	slots := out["slots"].([]interface{})
	first := slots[0].(map[string]interface{})
	opts := first["available_options"].([]interface{})
	for _, o := range opts {
		m := o.(map[string]interface{})
		ids := m["table_ids"].([]interface{})
		if len(ids) == 2 && ids[0] == "t_2" && ids[1] == "t_3" {
			t.Fatalf("undeclared pair t_2+t_3 must not appear: %v", opts)
		}
	}
	// the declared pair t_1+t_2 should appear.
	found := false
	for _, o := range opts {
		m := o.(map[string]interface{})
		ids := m["table_ids"].([]interface{})
		if len(ids) == 2 && ids[0] == "t_1" && ids[1] == "t_2" {
			found = true
		}
	}
	if !found {
		t.Errorf("declared pair t_1+t_2 missing from options: %v", opts)
	}
}

func TestCombinationBookingHappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	resp, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "combo1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2027-09-23T19:00", "party_size": 6})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("combo create = %d %v", resp.StatusCode, out)
	}
	ids := out["table_ids"].([]interface{})
	if len(ids) != 2 || ids[0] != "t_1" || ids[1] != "t_2" {
		t.Errorf("table_ids = %v, want [t_1,t_2]", ids)
	}
	if _, present := out["table_id"]; present {
		t.Errorf("table_id should be omitted for a 2-table set, got %v", out["table_id"])
	}

	// Both tables are now occupied: neither single nor the pair should be offered at 19:00.
	_, avail := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=2", nil, nil)
	for _, s := range avail["slots"].([]interface{}) {
		m := s.(map[string]interface{})
		if m["starts_at_local"] != "2027-09-23T19:00" {
			continue
		}
		if ta := m["available_table_ids"].([]interface{}); len(ta) != 0 {
			t.Errorf("both tables should be taken at 19:00, available_table_ids=%v", ta)
		}
	}
}

func TestCombinationBookingSingleTableIDResponseIncludesTableID(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	resp, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "single1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d %v", resp.StatusCode, out)
	}
	if out["table_id"] != "t_1" {
		t.Errorf("table_id = %v, want t_1", out["table_id"])
	}
	ids := out["table_ids"].([]interface{})
	if len(ids) != 1 || ids[0] != "t_1" {
		t.Errorf("table_ids = %v, want [t_1]", ids)
	}
}

func TestCombinationBookingBothFieldsIs422(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	resp, _ := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "both1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "table_ids": []string{"t_1"}, "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

func TestCombinationErrors(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, `{
		"users": [{"id":"u_x","email":"x@example.com","password":"correct horse","display_name":"X"}],
		"restaurants": [{
			"id": "r_x", "name": "X", "timezone": "Europe/Berlin", "slot_minutes": 30,
			"reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
			"tables": [
				{"id": "t_1", "label": "1", "capacity": 2},
				{"id": "t_2", "label": "2", "capacity": 2},
				{"id": "t_3", "label": "3", "capacity": 2}
			],
			"combinable": [["t_1", "t_2"]]
		}],
		"reservations": []
	}`)
	token := loginToken(t, srv, "x@example.com", "correct horse")

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"undeclared_pair", map[string]interface{}{"restaurant_id": "r_x", "table_ids": []string{"t_2", "t_3"}, "starts_at_local": "2027-09-23T19:00", "party_size": 4}, http.StatusUnprocessableEntity},
		{"three_tables", map[string]interface{}{"restaurant_id": "r_x", "table_ids": []string{"t_1", "t_2", "t_3"}, "starts_at_local": "2027-09-23T19:00", "party_size": 4}, http.StatusUnprocessableEntity},
		{"duplicate_table", map[string]interface{}{"restaurant_id": "r_x", "table_ids": []string{"t_1", "t_1"}, "starts_at_local": "2027-09-23T19:00", "party_size": 2}, http.StatusUnprocessableEntity},
		{"party_exceeds_combo_capacity", map[string]interface{}{"restaurant_id": "r_x", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2027-09-23T19:00", "party_size": 5}, http.StatusUnprocessableEntity},
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

	// Pair unavailable because one member is already individually booked.
	resp1, out1 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "pre1"},
		map[string]interface{}{"restaurant_id": "r_x", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("pre-booking t_1 = %d %v", resp1.StatusCode, out1)
	}
	resp2, _ := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "combo-conflict"},
		map[string]interface{}{"restaurant_id": "r_x", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2027-09-23T19:00", "party_size": 4})
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("combo with one member taken = %d, want 409", resp2.StatusCode)
	}
}

func TestCancelComboFreesBothTables(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "combo-cxl"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2027-09-23T19:00", "party_size": 6})
	ref := out["reference"].(string)

	resp, outc := doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp.StatusCode != http.StatusOK || outc["status"] != "cancelled" {
		t.Fatalf("cancel = %d %v", resp.StatusCode, outc)
	}

	_, avail := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=6", nil, nil)
	found := false
	for _, s := range avail["slots"].([]interface{}) {
		m := s.(map[string]interface{})
		if m["starts_at_local"] != "2027-09-23T19:00" {
			continue
		}
		for _, o := range m["available_options"].([]interface{}) {
			om := o.(map[string]interface{})
			ids := om["table_ids"].([]interface{})
			if len(ids) == 2 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("combo pair should be bookable again after cancel")
	}
}

func TestAmendBetweenSingleAndCombo(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "amend-combo1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	ref := out["reference"].(string)

	resp, out2 := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref,
		map[string]string{"Authorization": "Bearer " + token},
		map[string]interface{}{"table_ids": []string{"t_1", "t_2"}, "party_size": 6})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("amend to combo = %d %v", resp.StatusCode, out2)
	}
	ids := out2["table_ids"].([]interface{})
	if len(ids) != 2 {
		t.Errorf("table_ids after amend = %v, want 2 tables", ids)
	}
}

func TestReservationMovesWithComboLeg(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, `{
		"users": [{"id":"u_x","email":"x@example.com","password":"correct horse","display_name":"X"}],
		"restaurants": [{
			"id": "r_x", "name": "X", "timezone": "Europe/Berlin", "slot_minutes": 30,
			"reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
			"tables": [
				{"id": "t_1", "label": "1", "capacity": 2},
				{"id": "t_2", "label": "2", "capacity": 2},
				{"id": "t_3", "label": "3", "capacity": 4}
			],
			"combinable": [["t_1", "t_2"]]
		}],
		"reservations": []
	}`)
	token := loginToken(t, srv, "x@example.com", "correct horse")

	_, out1 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mvc1"},
		map[string]interface{}{"restaurant_id": "r_x", "table_id": "t_3", "starts_at_local": "2027-09-23T19:00", "party_size": 4})
	ref1 := out1["reference"].(string)

	resp, out := doJSON(t, "POST", srv.URL+"/reservation-moves", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mvc-move"},
		map[string]interface{}{"moves": []map[string]interface{}{
			{"reference": ref1, "table_ids": []string{"t_1", "t_2"}},
		}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("move to combo = %d %v", resp.StatusCode, out)
	}
	list := out["reservations"].([]interface{})
	moved := list[0].(map[string]interface{})
	ids := moved["table_ids"].([]interface{})
	if len(ids) != 2 || ids[0] != "t_1" || ids[1] != "t_2" {
		t.Errorf("moved table_ids = %v, want [t_1,t_2]", ids)
	}
}

func TestConcurrentComboBookingOnlyOneWins(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	const n = 10
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := doJSON(t, "POST", srv.URL+"/reservations",
				map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "combo-race-" + itoa(i)},
				map[string]interface{}{"restaurant_id": "r_anker", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2027-09-23T19:00", "party_size": 6})
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

// TestConcurrentSingleVsComboOverlapOnlyOneWins proves a single-table booking on t_1 and a
// combo booking on [t_1,t_2] racing for the same slot cannot both succeed, since they share
// table t_1 — exactly one of the two families should win across many trials.
func TestConcurrentSingleVsComboOverlapOnlyOneWins(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	const n = 10
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var body map[string]interface{}
			if i%2 == 0 {
				body = map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2}
			} else {
				body = map[string]interface{}{"restaurant_id": "r_anker", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2027-09-23T19:00", "party_size": 6}
			}
			resp, _ := doJSON(t, "POST", srv.URL+"/reservations",
				map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "mix-race-" + itoa(i)},
				body)
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, s := range statuses {
		if s == http.StatusCreated {
			wins++
		} else if s != http.StatusConflict {
			t.Errorf("unexpected status %d", s)
		}
	}
	if wins != 1 {
		t.Errorf("wins = %d, want exactly 1 since every variant touches t_1 (statuses=%v)", wins, statuses)
	}
}
