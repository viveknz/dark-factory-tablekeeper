package httpapi

import (
	"net/http"
	"testing"
)

const policyFixture = `{
	"users": [
		{"id": "u_mgr", "email": "mgr@example.com", "password": "correct horse", "display_name": "Mgr"},
		{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}
	],
	"restaurants": [
		{
			"id": "r_p", "name": "Policy Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "mon", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}],
			"manager_user_ids": ["u_mgr"]
		}
	],
	"reservations": []
}`

func validPolicyBody(effectiveFrom string) map[string]interface{} {
	return map[string]interface{}{
		"effective_from":               effectiveFrom,
		"slot_minutes":                 30,
		"reservation_duration_minutes": 120,
		"cancellation_cutoff_minutes":  60,
		"opening_hours":                []map[string]string{{"weekday": "mon", "opens": "18:00", "closes": "23:00"}},
		"capacities":                   map[string]int{"t_1": 3, "t_2": 6},
	}
}

func TestPublishPolicyPermissions(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, policyFixture)
	adaToken := loginToken(t, srv, "ada@example.com", "correct horse")

	// No token -> 401.
	resp, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Idempotency-Key": "k0"}, validPolicyBody("2026-01-01"))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token status = %d, want 401", resp.StatusCode)
	}

	// Authenticated non-manager -> 403 forbidden.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies",
		map[string]string{"Authorization": "Bearer " + adaToken, "Idempotency-Key": "k1"}, validPolicyBody("2026-01-01"))
	if resp2.StatusCode != http.StatusForbidden || out2["error"].(map[string]interface{})["code"] != "forbidden" {
		t.Errorf("non-manager status/body = %d %v, want 403 forbidden", resp2.StatusCode, out2)
	}

	// Unknown restaurant -> 404.
	mgrToken := loginToken(t, srv, "mgr@example.com", "correct horse")
	resp3, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_nope/policies",
		map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "k2"}, validPolicyBody("2026-01-01"))
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("unknown restaurant status = %d, want 404", resp3.StatusCode)
	}

	// Manager -> 201 with policy_version 1.
	resp4, out4 := doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies",
		map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "k3"}, validPolicyBody("2026-01-01"))
	if resp4.StatusCode != http.StatusCreated {
		t.Fatalf("manager publish status = %d %v, want 201", resp4.StatusCode, out4)
	}
	if v, ok := out4["policy_version"].(float64); !ok || v != 1 {
		t.Errorf("policy_version = %v, want 1", out4["policy_version"])
	}
}

func TestPublishPolicyValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, policyFixture)
	mgrToken := loginToken(t, srv, "mgr@example.com", "correct horse")

	cases := []struct {
		name string
		mut  func(m map[string]interface{})
	}{
		{"bad_date", func(m map[string]interface{}) { m["effective_from"] = "not-a-date" }},
		{"slot_minutes_zero", func(m map[string]interface{}) { m["slot_minutes"] = 0 }},
		{"slot_minutes_too_big", func(m map[string]interface{}) { m["slot_minutes"] = 1441 }},
		{"slot_minutes_bool", func(m map[string]interface{}) { m["slot_minutes"] = true }},
		{"duration_bool", func(m map[string]interface{}) { m["reservation_duration_minutes"] = false }},
		{"cutoff_negative", func(m map[string]interface{}) { m["cancellation_cutoff_minutes"] = -1 }},
		{"cutoff_too_big", func(m map[string]interface{}) { m["cancellation_cutoff_minutes"] = 10081 }},
		{"duplicate_weekday", func(m map[string]interface{}) {
			m["opening_hours"] = []map[string]string{
				{"weekday": "mon", "opens": "18:00", "closes": "20:00"},
				{"weekday": "mon", "opens": "20:00", "closes": "22:00"},
			}
		}},
		{"capacities_missing_table", func(m map[string]interface{}) { m["capacities"] = map[string]int{"t_1": 3} }},
		{"capacities_extra_table", func(m map[string]interface{}) { m["capacities"] = map[string]int{"t_1": 3, "t_2": 6, "t_x": 2} }},
		{"capacities_zero", func(m map[string]interface{}) { m["capacities"] = map[string]int{"t_1": 0, "t_2": 6} }},
		{"capacities_too_big", func(m map[string]interface{}) { m["capacities"] = map[string]int{"t_1": 101, "t_2": 6} }},
		{"missing_field", func(m map[string]interface{}) { delete(m, "opening_hours") }},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := validPolicyBody("2026-01-01")
			c.mut(body)
			resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies",
				map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "case-" + c.name + string(rune('a'+i))}, body)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("status = %d %v, want 422", resp.StatusCode, out)
			}
		})
	}

	// Confirm a failed write allocated no version: the next successful publish still gets version 1.
	resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies",
		map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "finally-good"}, validPolicyBody("2026-01-01"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("final publish = %d %v", resp.StatusCode, out)
	}
	if v, ok := out["policy_version"].(float64); !ok || v != 1 {
		t.Errorf("policy_version after failed writes = %v, want 1", out["policy_version"])
	}
}

func TestListPoliciesPublicAndOmitsPolicy0(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, policyFixture)
	mgrToken := loginToken(t, srv, "mgr@example.com", "correct horse")

	// Public: no auth required.
	resp, out := doJSON(t, "GET", srv.URL+"/restaurants/r_p/policies", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list policies (no auth) = %d", resp.StatusCode)
	}
	if list := out["policies"].([]interface{}); len(list) != 0 {
		t.Errorf("fresh restaurant policies = %v, want empty (policy 0 is implicit)", list)
	}

	doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "p1"}, validPolicyBody("2026-01-01"))
	doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "p2"}, validPolicyBody("2025-06-01"))

	resp2, out2 := doJSON(t, "GET", srv.URL+"/restaurants/r_p/policies", nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("list policies = %d", resp2.StatusCode)
	}
	list := out2["policies"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("policies = %v, want 2", list)
	}
	// Publication order (p1 then p2), even though p2's effective_from is earlier.
	if list[0].(map[string]interface{})["policy_version"].(float64) != 1 {
		t.Errorf("first published policy's version = %v, want 1", list[0])
	}
	if list[1].(map[string]interface{})["policy_version"].(float64) != 2 {
		t.Errorf("second published policy's version = %v, want 2", list[1])
	}

	// The plain restaurant detail endpoint is untouched by policies.
	resp3, out3 := doJSON(t, "GET", srv.URL+"/restaurants/r_p", nil, nil)
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("get restaurant = %d", resp3.StatusCode)
	}
	if out3["slot_minutes"].(float64) != 30 {
		t.Errorf("restaurant detail slot_minutes = %v, want the original fixture's 30, unaffected by policies", out3["slot_minutes"])
	}
}

func TestPolicySelectionGreatestEffectiveFromTieBreakVersion(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, policyFixture)
	mgrToken := loginToken(t, srv, "mgr@example.com", "correct horse")
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	// Publish out of effective-date order: v1 effective 2026-06-01, v2 effective 2026-01-01
	// (earlier date, published later), v3 same date as v1 (2026-06-01) -- the tie should pick
	// the greatest policy_version (v3) for dates on/after 2026-06-01.
	body1 := validPolicyBody("2026-06-01")
	body1["cancellation_cutoff_minutes"] = 10
	doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "v1"}, body1)

	body2 := validPolicyBody("2026-01-01")
	body2["cancellation_cutoff_minutes"] = 20
	doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "v2"}, body2)

	body3 := validPolicyBody("2026-06-01")
	body3["cancellation_cutoff_minutes"] = 30
	doJSON(t, "POST", srv.URL+"/restaurants/r_p/policies", map[string]string{"Authorization": "Bearer " + mgrToken, "Idempotency-Key": "v3"}, body3)

	// A booking on 2026-06-08 (after both dates) should pick v3 (tie-break on 2026-06-01 vs itself -> highest version among those <= date).
	resp, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "book1"},
		map[string]interface{}{"restaurant_id": "r_p", "table_id": "t_1", "starts_at_local": "2026-06-08T18:00", "party_size": 2})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("booking under v3 = %d %v", resp.StatusCode, out)
	}
	terms := out["accepted_terms"].(map[string]interface{})
	if terms["policy_version"].(float64) != 3 {
		t.Errorf("accepted_terms.policy_version = %v, want 3", terms["policy_version"])
	}
	if terms["cancellation_cutoff_minutes"].(float64) != 30 {
		t.Errorf("accepted cutoff = %v, want 30 (v3's)", terms["cancellation_cutoff_minutes"])
	}

	// A booking on 2026-03-01 (between v2's and v1/v3's effective dates) should pick v2.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "book2"},
		map[string]interface{}{"restaurant_id": "r_p", "table_id": "t_2", "starts_at_local": "2026-03-02T18:00", "party_size": 2})
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("booking under v2 = %d %v", resp2.StatusCode, out2)
	}
	terms2 := out2["accepted_terms"].(map[string]interface{})
	if terms2["policy_version"].(float64) != 2 {
		t.Errorf("accepted_terms.policy_version = %v, want 2", terms2["policy_version"])
	}

	// A booking before any policy's effective date uses policy 0.
	resp3, out3 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "book3"},
		map[string]interface{}{"restaurant_id": "r_p", "table_id": "t_1", "starts_at_local": "2025-12-01T18:00", "party_size": 2})
	if resp3.StatusCode != http.StatusCreated {
		t.Fatalf("booking under policy 0 = %d %v", resp3.StatusCode, out3)
	}
	terms3 := out3["accepted_terms"].(map[string]interface{})
	if terms3["policy_version"].(float64) != 0 {
		t.Errorf("accepted_terms.policy_version = %v, want 0", terms3["policy_version"])
	}

	// Publishing never retroactively edits the first booking's (book1's) terms.
	_, outGet := doJSON(t, "GET", srv.URL+"/reservations/"+out["reference"].(string), map[string]string{"Authorization": "Bearer " + ada}, nil)
	if outGet["accepted_terms"].(map[string]interface{})["policy_version"].(float64) != 3 {
		t.Errorf("book1's terms changed after later publishes: %v", outGet["accepted_terms"])
	}
}
