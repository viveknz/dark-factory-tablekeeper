package httpapi

import (
	"net/http"
	"testing"
)

func TestAvailabilityGridAndClosedDay(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)

	// Thursday 2027-09-23: open 18:00-23:00, 90-minute reservations, 30-minute grid.
	// Last valid start is 21:30 (21:30+90=23:00).
	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=2", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("availability = %d %v", resp.StatusCode, out)
	}
	slots := out["slots"].([]interface{})
	first := slots[0].(map[string]interface{})
	last := slots[len(slots)-1].(map[string]interface{})
	if first["starts_at_local"] != "2027-09-23T18:00" {
		t.Errorf("first slot = %v, want 18:00", first["starts_at_local"])
	}
	if last["starts_at_local"] != "2027-09-23T21:30" {
		t.Errorf("last slot = %v, want 21:30", last["starts_at_local"])
	}
	for _, s := range slots {
		m := s.(map[string]interface{})
		tables := m["available_table_ids"].([]interface{})
		if len(tables) != 2 {
			t.Errorf("slot %v: expected both tables free, got %v", m["starts_at_local"], tables)
		}
	}

	// Saturday 2027-09-25 has no opening_hours entry: closed day.
	resp2, out2 := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-25&party_size=2", nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("availability closed day = %d", resp2.StatusCode)
	}
	slots2 := out2["slots"].([]interface{})
	if len(slots2) != 0 {
		t.Errorf("closed day slots = %v, want empty", slots2)
	}
}

func TestAvailabilityPartySizeFiltersCapacity(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	// party_size 3 excludes t_1 (capacity 2), includes t_2 (capacity 4).
	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=3", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	slots := out["slots"].([]interface{})
	first := slots[0].(map[string]interface{})
	tables := first["available_table_ids"].([]interface{})
	if len(tables) != 1 || tables[0] != "t_2" {
		t.Errorf("tables = %v, want [t_2]", tables)
	}
}

func TestAvailabilityMissingParamsIs422(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	for _, q := range []string{
		"date=2027-09-23&party_size=2",
		"restaurant_id=r_anker&party_size=2",
		"restaurant_id=r_anker&date=2027-09-23",
	} {
		resp, _ := doJSON(t, "GET", srv.URL+"/availability?"+q, nil, nil)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("q=%s status=%d, want 422", q, resp.StatusCode)
		}
	}
}

func TestAvailabilityQueryIntStrictness(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	for _, ps := range []string{"1e9", "4.0", "+4", "-1", "abc"} {
		resp, _ := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size="+ps, nil, nil)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("party_size=%s status=%d, want 422", ps, resp.StatusCode)
		}
	}
}

func TestAvailabilityRejectsImpossibleCalendarDate(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	for _, d := range []string{"2026-02-30", "not-a-date", "24-09-2026", "2026-13-01"} {
		resp, _ := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date="+d+"&party_size=2", nil, nil)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("date=%s status=%d, want 422", d, resp.StatusCode)
		}
	}
}

func TestAvailabilityUnknownRestaurantIs404(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	resp, _ := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=nope&date=2027-09-23&party_size=2", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
