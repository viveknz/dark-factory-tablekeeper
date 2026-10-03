package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

const explainFixture = `{
	"users": [{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}],
	"restaurants": [
		{
			"id": "r_e", "name": "Explain Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "mon", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}]
		},
		{
			"id": "r_closed", "name": "Closed Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "tue", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}]
		}
	],
	"reservations": [
		{"id": "res_1", "reference": "TAKEN001", "user_id": "u_ada", "restaurant_id": "r_e", "table_id": "t_1",
		 "party_size": 2, "starts_at_local": "2026-06-08T19:00"}
	]
}`

func TestExplainOnlyAcceptsLiteralTrue(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, explainFixture)
	for _, bad := range []string{"false", "1", "", "TRUE", "yes"} {
		resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_e&date=2026-06-08&party_size=2&explain="+bad, nil, nil)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("explain=%q status = %d %v, want 422", bad, resp.StatusCode, out)
		}
	}
}

func TestAvailabilityWithoutExplainIsByteIdenticalToStage2Shape(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, explainFixture)
	resp, err := http.Get(srv.URL + "/availability?restaurant_id=r_e&date=2026-06-08&party_size=2")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("availability without explain: err=%v status=%v", err, resp)
	}
	var out map[string]interface{}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	slots := out["slots"].([]interface{})
	if len(slots) == 0 {
		t.Fatalf("expected slots")
	}
	for _, s := range slots {
		slot := s.(map[string]interface{})
		if _, has := slot["explain"]; has {
			t.Errorf("slot has an 'explain' field when explain was omitted: %v", slot)
		}
		for _, want := range []string{"starts_at_local", "starts_at", "available_table_ids", "available_options"} {
			if _, has := slot[want]; !has {
				t.Errorf("slot missing %q: %v", want, slot)
			}
		}
	}
}

func TestExplainEveryTableEveryRuleInFixtureOrder(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, explainFixture)
	// party_size 3: t_1 (capacity 2) fails capacity; t_2 (capacity 4) is free. At 19:00 t_1 is
	// taken by res_1 (party 2 <= cap 2, but overlap fails too) -- use party_size 3 so t_1 fails
	// capacity regardless, and check 19:00 specifically where t_1 also fails no_overlap.
	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_e&date=2026-06-08&party_size=3&explain=true", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("availability explain = %d %v", resp.StatusCode, out)
	}
	slots := out["slots"].([]interface{})
	var slot1900 map[string]interface{}
	for _, s := range slots {
		sl := s.(map[string]interface{})
		if sl["starts_at_local"] == "2026-06-08T19:00" {
			slot1900 = sl
		}
	}
	if slot1900 == nil {
		t.Fatalf("no 19:00 slot found")
	}
	explain := slot1900["explain"].([]interface{})
	if len(explain) != 2 {
		t.Fatalf("explain length = %d, want 2 (one per table, fixture order)", len(explain))
	}
	t1 := explain[0].(map[string]interface{})
	if t1["table_id"] != "t_1" {
		t.Errorf("explain[0].table_id = %v, want t_1 (fixture order)", t1["table_id"])
	}
	rules1 := t1["rules"].([]interface{})
	if len(rules1) != 2 {
		t.Fatalf("t_1 rules length = %d, want 2", len(rules1))
	}
	if rules1[0].(map[string]interface{})["rule"] != "capacity" || rules1[1].(map[string]interface{})["rule"] != "no_overlap" {
		t.Errorf("t_1 rule order = %v, want [capacity, no_overlap]", rules1)
	}
	if rules1[0].(map[string]interface{})["holds"] != false {
		t.Errorf("t_1 capacity should fail (party 3 > capacity 2): %v", rules1[0])
	}
	if rules1[1].(map[string]interface{})["holds"] != false {
		t.Errorf("t_1 no_overlap should fail (taken by res_1): %v", rules1[1])
	}
	if t1["available"] != false {
		t.Errorf("t_1 available = %v, want false", t1["available"])
	}

	t2 := explain[1].(map[string]interface{})
	if t2["table_id"] != "t_2" {
		t.Errorf("explain[1].table_id = %v, want t_2", t2["table_id"])
	}
	rules2 := t2["rules"].([]interface{})
	if rules2[0].(map[string]interface{})["holds"] != true || rules2[1].(map[string]interface{})["holds"] != true {
		t.Errorf("t_2 rules = %v, want both true", rules2)
	}
	if t2["available"] != true {
		t.Errorf("t_2 available = %v, want true", t2["available"])
	}

	// available_table_ids must exactly match the true-available table_ids in the same order.
	avail := slot1900["available_table_ids"].([]interface{})
	if len(avail) != 1 || avail[0] != "t_2" {
		t.Errorf("available_table_ids = %v, want [t_2]", avail)
	}
}

func TestExplainClosedDayStillEmptySlots(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, explainFixture)
	// 2026-06-08 is a Monday; r_closed is only open Tuesdays.
	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_closed&date=2026-06-08&party_size=1&explain=true", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("availability = %d %v", resp.StatusCode, out)
	}
	slots := out["slots"].([]interface{})
	if len(slots) != 0 {
		t.Errorf("closed day slots = %v, want []", slots)
	}
}

func TestExplainPolicyVersionOnEachTable(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, policyFixture)
	mgrToken := loginToken(t, srv, "mgr@example.com", "correct horse")
	doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "pv1"}, validPolicyBody("2026-01-01"))

	resp, out := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_p&date=2026-06-08&party_size=1&explain=true", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("availability = %d %v", resp.StatusCode, out)
	}
	slots := out["slots"].([]interface{})
	if len(slots) == 0 {
		t.Fatalf("expected slots")
	}
	explain := slots[0].(map[string]interface{})["explain"].([]interface{})
	for _, e := range explain {
		if e.(map[string]interface{})["policy_version"].(float64) != 1 {
			t.Errorf("explain policy_version = %v, want 1", e.(map[string]interface{})["policy_version"])
		}
	}
}
