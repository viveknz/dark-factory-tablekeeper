package httpapi

import (
	"net/http"
	"testing"
)

const historyFixture = `{
	"users": [
		{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"},
		{"id": "u_bob", "email": "bob@example.com", "password": "correct horse2", "display_name": "Bob"}
	],
	"restaurants": [
		{
			"id": "r_h", "name": "History Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
			"opening_hours": [{"weekday": "mon", "opens": "18:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}],
			"combinable": [["t_1", "t_2"]]
		}
	],
	"reservations": []
}`

func TestHistoryOwnerOnly404EvenUnauthenticated(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	bob := loginToken(t, srv, "bob@example.com", "correct horse2")

	resp, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	ref := out["reference"].(string)

	// Unauthenticated -> 404, not 401.
	for _, path := range []string{"/reservations/" + ref + "/history", "/reservations/" + ref + "/decision"} {
		r2, o2 := doJSON(t, "GET", srv.URL+path, nil, nil)
		if r2.StatusCode != http.StatusNotFound {
			t.Errorf("unauthenticated %s = %d %v, want 404", path, r2.StatusCode, o2)
		}
	}
	// Another user -> 404, not 403.
	for _, path := range []string{"/reservations/" + ref + "/history", "/reservations/" + ref + "/decision"} {
		r3, o3 := doJSON(t, "GET", srv.URL+path, map[string]string{"Authorization": "Bearer " + bob}, nil)
		if r3.StatusCode != http.StatusNotFound {
			t.Errorf("other user %s = %d %v, want 404", path, r3.StatusCode, o3)
		}
	}
	// Owner -> 200.
	r4, o4 := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if r4.StatusCode != http.StatusOK {
		t.Fatalf("owner history = %d %v", r4.StatusCode, o4)
	}
}

func TestHistoryCreatedEntryNamesAllThreeFieldsFromNull(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	resp, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := out["reference"].(string)

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("entries = %v, want 1", entries)
	}
	e := entries[0].(map[string]interface{})
	if e["seq"].(float64) != 1 {
		t.Errorf("seq = %v, want 1", e["seq"])
	}
	if e["event"] != "created" {
		t.Errorf("event = %v, want created", e["event"])
	}
	changes := e["changes"].([]interface{})
	if len(changes) != 3 {
		t.Fatalf("created changes = %v, want 3", changes)
	}
	want := []string{"table_id", "starts_at_local", "party_size"}
	for i, f := range want {
		c := changes[i].(map[string]interface{})
		if c["field"] != f {
			t.Errorf("changes[%d].field = %v, want %v", i, c["field"], f)
		}
		if c["from"] != nil {
			t.Errorf("changes[%d].from = %v, want nil", i, c["from"])
		}
	}
	if e["revision"].(float64) != 1 {
		t.Errorf("entry revision = %v, want 1", e["revision"])
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
}

func TestHistoryChangedNamesOnlyChangedFieldsInFixedOrder(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := out["reference"].(string)

	// Change both table and party_size in one PATCH.
	respP, outP := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"table_id": "t_2", "party_size": 3})
	if respP.StatusCode != http.StatusOK {
		t.Fatalf("patch = %d %v", respP.StatusCode, outP)
	}

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want 2", entries)
	}
	e2 := entries[1].(map[string]interface{})
	if e2["seq"].(float64) != 2 {
		t.Errorf("seq = %v, want 2", e2["seq"])
	}
	if e2["event"] != "changed" {
		t.Errorf("event = %v, want changed", e2["event"])
	}
	changes := e2["changes"].([]interface{})
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (table_id, party_size; starts_at_local untouched)", changes)
	}
	if changes[0].(map[string]interface{})["field"] != "table_id" {
		t.Errorf("changes[0] = %v, want table_id first", changes[0])
	}
	if changes[1].(map[string]interface{})["field"] != "party_size" {
		t.Errorf("changes[1] = %v, want party_size second", changes[1])
	}
	if e2["revision"].(float64) != 2 {
		t.Errorf("entry revision = %v, want 2", e2["revision"])
	}
}

func TestHistoryNoOpPatchRecordsNothing(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := out["reference"].(string)

	resp, outP := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"table_id": "t_1", "party_size": 2, "starts_at_local": "2030-01-07T19:00"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("no-op patch = %d %v", resp.StatusCode, outP)
	}
	if outP["revision"].(float64) != 1 {
		t.Errorf("revision after no-op = %v, want unchanged 1", outP["revision"])
	}

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("entries after no-op = %v, want still 1 (no entry recorded)", entries)
	}
}

func TestHistoryCancelledTerminalWithEmptyChanges(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := out["reference"].(string)

	resp, _ := doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel = %d", resp.StatusCode)
	}
	// A repeated cancel must not bump revision or append another entry.
	doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + ada}, nil)

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want 2 (created, cancelled)", entries)
	}
	last := entries[1].(map[string]interface{})
	if last["event"] != "cancelled" {
		t.Errorf("event = %v, want cancelled", last["event"])
	}
	if changes := last["changes"].([]interface{}); len(changes) != 0 {
		t.Errorf("cancelled changes = %v, want empty", changes)
	}
	if last["revision"].(float64) != 2 {
		t.Errorf("cancelled revision = %v, want 2 (bumped once)", last["revision"])
	}

	_, decision := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/decision", map[string]string{"Authorization": "Bearer " + ada}, nil)
	if decision["revision"].(float64) != 2 {
		t.Errorf("decision after cancel = %v, want revision 2", decision)
	}
}

func TestHistoryIdempotentReplayRecordsNothing(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	body := map[string]interface{}{"restaurant_id": "r_h", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2}
	_, out := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "rk1"}, body)
	ref := out["reference"].(string)

	// Replay the identical request.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "rk1"}, body)
	if resp2.StatusCode != http.StatusOK || out2["reference"] != ref {
		t.Fatalf("replay = %d %v", resp2.StatusCode, out2)
	}

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("entries after replay = %v, want still 1", entries)
	}
}

func TestComboHistoryCreationUsesTableIDsFromNull(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2030-01-07T19:00", "party_size": 5})
	ref := out["reference"].(string)

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	changes := entries[0].(map[string]interface{})["changes"].([]interface{})
	c0 := changes[0].(map[string]interface{})
	if c0["field"] != "table_ids" {
		t.Errorf("combo creation change field = %v, want table_ids", c0["field"])
	}
	if c0["from"] != nil {
		t.Errorf("combo creation from = %v, want nil", c0["from"])
	}
	to := c0["to"].([]interface{})
	if len(to) != 2 || to[0] != "t_1" || to[1] != "t_2" {
		t.Errorf("combo creation to = %v, want [t_1 t_2] (combinable order)", to)
	}
}

func TestComboHistoryReversedPairIsNotAChange(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, historyFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_h", "table_ids": []string{"t_1", "t_2"}, "starts_at_local": "2030-01-07T19:00", "party_size": 5})
	ref := out["reference"].(string)

	// Submit the reversed order: same set, should be a true no-op.
	resp, outP := doJSON(t, "PATCH", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada},
		map[string]interface{}{"table_ids": []string{"t_2", "t_1"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reversed-pair patch = %d %v", resp.StatusCode, outP)
	}
	if outP["revision"].(float64) != 1 {
		t.Errorf("revision after reversed-pair patch = %v, want unchanged 1", outP["revision"])
	}
	ids := outP["table_ids"].([]interface{})
	if ids[0] != "t_1" || ids[1] != "t_2" {
		t.Errorf("table_ids after reversed-pair patch = %v, want still [t_1 t_2] (declared order)", ids)
	}

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("entries after reversed-pair patch = %v, want still 1 (no entry)", entries)
	}
}
