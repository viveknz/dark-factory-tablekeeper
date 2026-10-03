package httpapi

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"tablekeeper/internal/store"
)

// TestSolveReplanTieBreakRespectsReferenceOrder directly exercises the optimizer core with a
// hand-built adversarial scenario, not an HTTP fixture that could satisfy the spec "by
// accident": two bookings, AAAA0001 and BBBB0002, must both move off the closing table, their
// only two remaining candidate tables (rank 2 and rank 3) tie exactly on moved-count (1 each)
// and unused seats (1 each regardless of which gets which table) -- so only criterion 3, the
// rank vector compared in ascending REFERENCE order, can break the tie. The two full solutions
// are ranks=[2,3] (AAAA0001->rank2, BBBB0002->rank3) and ranks=[3,2] (swapped); [2,3] is
// lexicographically smaller, so a correct optimizer must pick it -- an implementation that
// assigns greedily in some other order (e.g. map iteration, or by party size) could easily
// produce [3,2] instead and still "look" feasible.
func TestSolveReplanTieBreakRespectsReferenceOrder(t *testing.T) {
	start := time.Date(2030, 1, 7, 19, 0, 0, 0, time.UTC)
	end := start.Add(60 * time.Minute)
	b1 := &store.Reservation{ID: "r1", Reference: "AAAA0001", TableIDs: []string{"t_1"}, PartySize: 3, StartsAt: start, EndsAt: end}
	b2 := &store.Reservation{ID: "r2", Reference: "BBBB0002", TableIDs: []string{"t_1"}, PartySize: 3, StartsAt: start, EndsAt: end}
	bookings := []*store.Reservation{b1, b2}

	optT3 := replanOption{tableIDs: []string{"t_3"}, rank: 2}
	optT4 := replanOption{tableIDs: []string{"t_4"}, rank: 3}
	candidates := [][]replanCandidate{
		{{opt: optT3, capacity: 4}, {opt: optT4, capacity: 4}},
		{{opt: optT3, capacity: 4}, {opt: optT4, capacity: 4}},
	}

	best, found := solveReplan(bookings, candidates)
	if !found {
		t.Fatalf("expected a feasible solution")
	}
	if best[0].rank != 2 || best[1].rank != 3 {
		t.Errorf("ranks = [%d,%d], want [2,3] (reference-ascending tie-break, AAAA0001 gets the lower rank)", best[0].rank, best[1].rank)
	}
}

const replanFixture = `{
	"users": [
		{"id": "u_mgr", "email": "mgr@example.com", "password": "correct horse", "display_name": "Mgr"},
		{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"},
		{"id": "u_bob", "email": "bob@example.com", "password": "correct horse2", "display_name": "Bob"}
	],
	"restaurants": [
		{
			"id": "r_plan", "name": "Plan Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 999999,
			"opening_hours": [{"weekday": "mon", "opens": "17:00", "closes": "23:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 2},
			           {"id": "t_3", "label": "3", "capacity": 4}],
			"combinable": [["t_1", "t_2"]],
			"manager_user_ids": ["u_mgr"]
		},
		{
			"id": "r_other", "name": "Other Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 60, "cancellation_cutoff_minutes": 60,
			"opening_hours": [{"weekday": "mon", "opens": "17:00", "closes": "23:00"}],
			"tables": [{"id": "o_1", "label": "1", "capacity": 2}],
			"manager_user_ids": ["u_mgr"]
		}
	],
	"reservations": []
}`

func TestReplanPreviewPermissionsAndValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	body := map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"}

	// No token -> 401.
	resp, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans", map[string]string{"Idempotency-Key": "k0"}, body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", resp.StatusCode)
	}
	// Non-manager -> 403.
	resp2, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans", map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"}, body)
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("non-manager = %d, want 403", resp2.StatusCode)
	}
	// Unknown restaurant -> 404.
	resp3, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_nope/replans", map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "k2"}, body)
	if resp3.StatusCode != http.StatusNotFound {
		t.Errorf("unknown restaurant = %d, want 404", resp3.StatusCode)
	}
	// Unknown table -> 404.
	badTable := map[string]interface{}{"table_id": "t_nope", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"}
	resp4, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans", map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "k3"}, badTable)
	if resp4.StatusCode != http.StatusNotFound {
		t.Errorf("unknown table = %d, want 404", resp4.StatusCode)
	}
	// from >= to -> 422.
	badInterval := map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T22:00:00+01:00", "to": "2030-01-07T18:00:00+01:00"}
	resp5, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans", map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "k4"}, badInterval)
	if resp5.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("from>=to = %d, want 422", resp5.StatusCode)
	}
	// Missing idempotency key -> 400.
	resp6, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans", map[string]string{"Authorization": "Bearer " + mgr}, body)
	if resp6.StatusCode != http.StatusBadRequest {
		t.Errorf("missing idempotency key = %d, want 400", resp6.StatusCode)
	}
}

func TestReplanPreviewMovesOneBookingAndDoesNotMutateState(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	_, bookOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := bookOut["reference"].(string)
	revBefore := bookOut["revision"].(float64)

	resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview1"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("preview = %d %v", resp.StatusCode, out)
	}
	if out["moved_count"].(float64) != 1 {
		t.Errorf("moved_count = %v, want 1", out["moved_count"])
	}
	assignments := out["assignments"].([]interface{})
	if len(assignments) != 1 {
		t.Fatalf("assignments = %v, want 1", assignments)
	}
	a0 := assignments[0].(map[string]interface{})
	if a0["reference"] != ref || a0["changed"] != true {
		t.Errorf("assignment = %v, want reference=%v changed=true", a0, ref)
	}
	ids := a0["table_ids"].([]interface{})
	if len(ids) != 1 || ids[0] == "t_1" {
		t.Errorf("assignment table_ids = %v, want a table other than t_1", ids)
	}

	// Preview must NOT mutate anything: the live reservation is untouched (still t_1, same revision).
	_, getOut := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if getOut["table_id"] != "t_1" {
		t.Errorf("reservation table_id after preview = %v, want still t_1", getOut["table_id"])
	}
	if getOut["revision"].(float64) != revBefore {
		t.Errorf("revision after preview = %v, want unchanged %v", getOut["revision"], revBefore)
	}
}

func TestReplanNoFeasiblePlanLeavesNothingChanged(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	bob := loginToken(t, srv, "bob@example.com", "correct horse2")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	// Fill every other table so the t_1 booking has nowhere to go.
	_, b1 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref1 := b1["reference"].(string)
	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + bob, "Idempotency-Key": "k2"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_2", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + bob, "Idempotency-Key": "k3"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_3", "starts_at_local": "2030-01-07T19:00", "party_size": 2})

	resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview-infeasible"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	if resp.StatusCode != http.StatusConflict || out["error"].(map[string]interface{})["code"] != "no_feasible_plan" {
		t.Fatalf("infeasible preview = %d %v, want 409 no_feasible_plan", resp.StatusCode, out)
	}

	_, getOut := doJSON(t, "GET", srv.URL+"/reservations/"+ref1, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if getOut["table_id"] != "t_1" {
		t.Errorf("booking changed after an infeasible plan attempt: %v", getOut)
	}
}

func TestReplanApplyHappyPathClosureAndHistory(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	_, bookOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := bookOut["reference"].(string)

	_, preview := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview1"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	planID := preview["plan_id"].(string)
	revAtPreview := preview["restaurant_revision"].(float64)

	resp, applyOut := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply1"}, map[string]interface{}{})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("apply = %d %v", resp.StatusCode, applyOut)
	}
	if applyOut["restaurant_revision"].(float64) != revAtPreview+1 {
		t.Errorf("restaurant_revision after apply = %v, want %v", applyOut["restaurant_revision"], revAtPreview+1)
	}
	reservations := applyOut["reservations"].([]interface{})
	if len(reservations) != 1 || reservations[0].(map[string]interface{})["reference"] != ref {
		t.Fatalf("reservations = %v", reservations)
	}

	_, getOut := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if getOut["table_id"] == "t_1" {
		t.Errorf("booking still on t_1 after apply: %v", getOut)
	}
	if getOut["revision"].(float64) != 2 {
		t.Errorf("revision after apply = %v, want 2", getOut["revision"])
	}

	_, hist := doJSON(t, "GET", srv.URL+"/reservations/"+ref+"/history", map[string]string{"Authorization": "Bearer " + ada}, nil)
	entries := hist["entries"].([]interface{})
	last := entries[len(entries)-1].(map[string]interface{})
	if last["event"] != "reassigned" {
		t.Errorf("last history event = %v, want reassigned", last["event"])
	}
	if last["plan_id"] != planID {
		t.Errorf("reassigned entry plan_id = %v, want %v", last["plan_id"], planID)
	}

	// Closure now blocks new bookings on t_1 for the closure window.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "blocked"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	if resp2.StatusCode != http.StatusConflict || out2["error"].(map[string]interface{})["code"] != "table_unavailable" {
		t.Errorf("booking a closed table = %d %v, want 409 table_unavailable", resp2.StatusCode, out2)
	}

	// explain=true reports no_overlap:false for the closed table during the closure window.
	_, explainOut := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_plan&date=2030-01-07&party_size=1&explain=true", nil, nil)
	slots := explainOut["slots"].([]interface{})
	var found bool
	for _, s := range slots {
		sl := s.(map[string]interface{})
		if sl["starts_at_local"] != "2030-01-07T19:00" {
			continue
		}
		found = true
		for _, e := range sl["explain"].([]interface{}) {
			et := e.(map[string]interface{})
			if et["table_id"] == "t_1" {
				rules := et["rules"].([]interface{})
				for _, ru := range rules {
					rm := ru.(map[string]interface{})
					if rm["rule"] == "no_overlap" && rm["holds"] != false {
						t.Errorf("t_1 no_overlap during closure = %v, want false", rm["holds"])
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected a 19:00 slot in explain output")
	}
}

func TestReplanApplyStaleAndAlreadyApplied(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})

	_, preview := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview1"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	planID := preview["plan_id"].(string)

	// Something else bumps the restaurant revision before apply: a new unrelated booking.
	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "unrelated"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_3", "starts_at_local": "2030-01-07T20:00", "party_size": 2})

	resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply-stale"}, map[string]interface{}{})
	if resp.StatusCode != http.StatusConflict || out["error"].(map[string]interface{})["code"] != "stale_plan" {
		t.Fatalf("apply after an intervening change = %d %v, want 409 stale_plan", resp.StatusCode, out)
	}

	// A fresh preview, then apply, then apply-again-under-a-different-key -> plan_already_applied.
	_, preview2 := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview2"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	planID2 := preview2["plan_id"].(string)

	resp2, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID2+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply-first"}, map[string]interface{}{})
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("first apply = %d", resp2.StatusCode)
	}

	resp3, out3 := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID2+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply-second-different-key"}, map[string]interface{}{})
	if resp3.StatusCode != http.StatusConflict || out3["error"].(map[string]interface{})["code"] != "plan_already_applied" {
		t.Fatalf("second apply (different key) = %d %v, want 409 plan_already_applied", resp3.StatusCode, out3)
	}

	// Replay of the original successful key -> original response, 200.
	resp4, out4 := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID2+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply-first"}, map[string]interface{}{})
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("replay of the successful apply key = %d %v, want 200", resp4.StatusCode, out4)
	}
	if out4["plan_id"] != planID2 {
		t.Errorf("replay plan_id = %v, want %v", out4["plan_id"], planID2)
	}
}

func TestReplanClosureAtAnotherRestaurantDoesNotInvalidate(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	_, bookOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	_ = bookOut

	_, preview := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview1"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	planID := preview["plan_id"].(string)

	// A booking (and hence a restaurant-revision bump) at a DIFFERENT restaurant must not
	// invalidate this plan.
	doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "other-restaurant"},
		map[string]interface{}{"restaurant_id": "r_other", "table_id": "o_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})

	resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID+"/apply",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "apply1"}, map[string]interface{}{})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("apply after an unrelated restaurant's change = %d %v, want 201", resp.StatusCode, out)
	}
}

func TestConcurrentReplanApplicationsOnlyOneSucceeds(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	_, bookOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "k1"},
		map[string]interface{}{"restaurant_id": "r_plan", "table_id": "t_1", "starts_at_local": "2030-01-07T19:00", "party_size": 2})
	ref := bookOut["reference"].(string)

	_, preview := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview1"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T18:00:00+01:00", "to": "2030-01-07T22:00:00+01:00"})
	planID := preview["plan_id"].(string)

	const n = 10
	var wg sync.WaitGroup
	results := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, _ := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans/"+planID+"/apply",
				map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "concurrent-apply"}, map[string]interface{}{})
			results[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	wins, replays := 0, 0
	for _, s := range results {
		switch s {
		case http.StatusCreated:
			wins++
		case http.StatusOK:
			replays++
		default:
			t.Errorf("unexpected status %d (want 201 or 200; all share one idempotency key so no 409 should occur)", s)
		}
	}
	if wins != 1 {
		t.Errorf("concurrent applications of the same plan+key: %d returned 201, want exactly 1 (rest should be 200 replays)", wins)
	}
	if wins+replays != n {
		t.Errorf("wins+replays = %d, want %d", wins+replays, n)
	}

	// Exactly one reassignment happened: the booking moved off t_1 exactly once (revision 2,
	// not partially applied or double-applied).
	_, getOut := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + ada}, nil)
	if getOut["revision"].(float64) != 2 {
		t.Errorf("revision after concurrent applications = %v, want exactly 2", getOut["revision"])
	}
}

func TestReplanPlanningLimitTooManyConsideredBookings(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, replanFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")
	bob := loginToken(t, srv, "bob@example.com", "correct horse2")
	mgr := loginToken(t, srv, "mgr@example.com", "correct horse")

	// 7 confirmed bookings overlapping the closure window exceeds the 6-booking limit. Spread
	// across the 3 tables at distinct non-overlapping times so each booking itself is valid.
	times := []string{"17:00", "18:00", "19:00", "20:00", "21:00", "17:00", "18:00"}
	tables := []string{"t_1", "t_1", "t_1", "t_1", "t_1", "t_2", "t_2"}
	users := []string{ada, bob, ada, bob, ada, bob, ada}
	for i := 0; i < 7; i++ {
		resp, out := doJSON(t, "POST", srv.URL+"/reservations",
			map[string]string{"Authorization": "Bearer " + users[i], "Idempotency-Key": "limit-" + string(rune('a'+i))},
			map[string]interface{}{"restaurant_id": "r_plan", "table_id": tables[i], "starts_at_local": "2030-01-07T" + times[i], "party_size": 2})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed booking %d = %d %v", i, resp.StatusCode, out)
		}
	}

	resp, out := doJSON(t, "POST", srv.URL+"/restaurants/r_plan/replans",
		map[string]string{"Authorization": "Bearer " + mgr, "Idempotency-Key": "preview-limit"},
		map[string]interface{}{"table_id": "t_1", "from": "2030-01-07T00:00:00+01:00", "to": "2030-01-08T00:00:00+01:00"})
	if resp.StatusCode != http.StatusUnprocessableEntity || out["error"].(map[string]interface{})["code"] != "planning_limit" {
		t.Fatalf("7 considered bookings = %d %v, want 422 planning_limit", resp.StatusCode, out)
	}
}
