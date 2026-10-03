package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"time"

	"tablekeeper/internal/idgen"
	"tablekeeper/internal/store"
)

const (
	maxReplanTables    = 6
	maxReplanPairs     = 4
	maxReplanBookings  = 6
)

// replanOption is one single-table or declared-pair seating choice, in the canonical rank
// order spec requires: every single table in fixture order, then every declared pair in
// combinable order, ranks starting at 0.
type replanOption struct {
	tableIDs []string
	rank     int
}

func buildReplanOptions(rest *store.Restaurant) []replanOption {
	opts := make([]replanOption, 0, len(rest.Tables)+len(rest.Combinable))
	for i, t := range rest.Tables {
		opts = append(opts, replanOption{tableIDs: []string{t.ID}, rank: i})
	}
	base := len(rest.Tables)
	for i, pair := range rest.Combinable {
		if len(pair) != 2 {
			continue
		}
		opts = append(opts, replanOption{tableIDs: append([]string{}, pair...), rank: base + i})
	}
	return opts
}

func validDeclaredPairCount(rest *store.Restaurant) int {
	n := 0
	for _, p := range rest.Combinable {
		if len(p) == 2 {
			n++
		}
	}
	return n
}

// replanCandidate is one eligible option for one considered booking, with its capacity under
// that booking's own accepted terms.
type replanCandidate struct {
	opt      replanOption
	capacity int
}

// fixedConflict reports whether tableIDs/[start,end) conflicts with the proposed new closure,
// any already-applied closure, or any confirmed reservation that is NOT itself one of the
// considered bookings (those are optimized jointly, checked separately).
func fixedConflict(rest *store.Restaurant, s *store.Store, consideredIDs map[string]bool, tableIDs []string, start, end time.Time, newClosure store.Closure) bool {
	for _, tid := range tableIDs {
		if tid == newClosure.TableID && newClosure.Overlaps(start, end) {
			return true
		}
		for _, c := range rest.Closures {
			if tid == c.TableID && c.Overlaps(start, end) {
				return true
			}
		}
	}
	for _, r := range s.Reservations {
		if r.RestaurantID != rest.ID || r.Status != "confirmed" || consideredIDs[r.ID] {
			continue
		}
		if !shareAnyTable(r.TableIDs, tableIDs) {
			continue
		}
		if r.Overlaps(start, end) {
			return true
		}
	}
	return false
}

type replanCost struct {
	moved  int
	unused int
	ranks  []int
}

func (a replanCost) less(b replanCost) bool {
	if a.moved != b.moved {
		return a.moved < b.moved
	}
	if a.unused != b.unused {
		return a.unused < b.unused
	}
	for i := range a.ranks {
		if a.ranks[i] != b.ranks[i] {
			return a.ranks[i] < b.ranks[i]
		}
	}
	return false
}

// solveReplan searches every feasible complete assignment of bookings (sorted by reference, so
// the rank-vector tie-break compares in the spec's required order) to candidate options,
// returning the one minimizing (moved count, unused seats, rank vector) in that priority order.
// Exhaustive but bounded tightly by the spec's own limits (<=6 bookings, <=10 options each).
func solveReplan(bookings []*store.Reservation, candidates [][]replanCandidate) ([]replanOption, bool) {
	best := make([]replanOption, len(bookings))
	chosen := make([]replanOption, len(bookings))
	var bestCost replanCost
	found := false

	var recurse func(i int)
	recurse = func(i int) {
		if i == len(bookings) {
			moved := 0
			unused := 0
			ranks := make([]int, len(bookings))
			for j, b := range bookings {
				if !sameTableSet(chosen[j].tableIDs, b.TableIDs) {
					moved++
				}
				cap := 0
				for _, c := range candidates[j] {
					if c.opt.rank == chosen[j].rank {
						cap = c.capacity
						break
					}
				}
				unused += cap - b.PartySize
				ranks[j] = chosen[j].rank
			}
			cost := replanCost{moved: moved, unused: unused, ranks: ranks}
			if !found || cost.less(bestCost) {
				found = true
				bestCost = cost
				copy(best, chosen)
			}
			return
		}
		for _, c := range candidates[i] {
			conflict := false
			for k := 0; k < i; k++ {
				if shareAnyTable(chosen[k].tableIDs, c.opt.tableIDs) &&
					overlaps(bookings[k].StartsAt, bookings[k].EndsAt, bookings[i].StartsAt, bookings[i].EndsAt) {
					conflict = true
					break
				}
			}
			if conflict {
				continue
			}
			chosen[i] = c.opt
			recurse(i + 1)
		}
	}
	recurse(0)
	return best, found
}

type planAssignmentResponse struct {
	Reference string   `json:"reference"`
	TableIDs  []string `json:"table_ids"`
	Changed   bool     `json:"changed"`
}

type closureResponse struct {
	TableID string `json:"table_id"`
	From    string `json:"from"`
	To      string `json:"to"`
}

type previewResponse struct {
	PlanID            string                    `json:"plan_id"`
	RestaurantRevision int                      `json:"restaurant_revision"`
	Closure           closureResponse           `json:"closure"`
	Assignments       []planAssignmentResponse  `json:"assignments"`
	MovedCount        int                       `json:"moved_count"`
	UnusedSeats       int                       `json:"unused_seats"`
}

// requireManager authenticates, looks up the restaurant (404 if unknown), and checks manager
// permission (403 if authenticated but not a manager). Shared by replans preview/apply and
// (already, separately) policy publication.
func (a *API) requireManager(r *http.Request, restID string) (user *store.User, rest *store.Restaurant, apiErr *apiError) {
	user, aerr := a.authenticate(r)
	if aerr != nil {
		return nil, nil, aerr
	}
	rest, ok := a.Store.Restaurants[restID]
	if !ok {
		return nil, nil, errNotFound
	}
	if !rest.IsManager(user.ID) {
		return nil, nil, errForbidden
	}
	return user, rest, nil
}

func (a *API) handlePreviewReplan(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	raw, berr := decodeBody(body)
	if berr != nil {
		writeError(w, berr)
		return
	}
	restID := r.PathValue("id")

	a.Store.Lock()
	defer a.Store.Unlock()

	user, rest, perr := a.requireManager(r, restID)
	if perr != nil {
		writeError(w, perr)
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, errMissingIdempotency)
		return
	}
	if len(key) > 255 {
		writeError(w, errValidationFailed("Idempotency-Key must be at most 255 characters"))
		return
	}
	canonical := canonicalBody(raw)
	path := "/restaurants/" + restID + "/replans"
	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, path, key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	tableID, presentT, eT := fieldString(raw, "table_id")
	if eT != nil {
		writeError(w, eT)
		return
	}
	fromStr, presentF, eF := fieldString(raw, "from")
	if eF != nil {
		writeError(w, eF)
		return
	}
	toStr, presentTo, eTo := fieldString(raw, "to")
	if eTo != nil {
		writeError(w, eTo)
		return
	}
	if !presentT || !presentF || !presentTo {
		writeError(w, errValidationFailed("table_id, from and to are all required"))
		return
	}
	from, errFrom := time.Parse(time.RFC3339, fromStr)
	to, errTo := time.Parse(time.RFC3339, toStr)
	if errFrom != nil || errTo != nil || !from.Before(to) {
		writeError(w, errValidationFailed("from and to must be explicit-offset instants with from < to"))
		return
	}
	if _, found := rest.FindTable(tableID); !found {
		writeError(w, errNotFound)
		return
	}

	if len(rest.Tables) > maxReplanTables || validDeclaredPairCount(rest) > maxReplanPairs {
		writeError(w, errPlanningLimit)
		return
	}

	newClosure := store.Closure{TableID: tableID, From: from, To: to}

	var considered []*store.Reservation
	for _, resv := range a.Store.Reservations {
		if resv.RestaurantID != restID || resv.Status != "confirmed" {
			continue
		}
		if resv.Overlaps(from, to) {
			considered = append(considered, resv)
		}
	}
	sort.Slice(considered, func(i, j int) bool { return considered[i].Reference < considered[j].Reference })

	if len(considered) > maxReplanBookings {
		writeError(w, errPlanningLimit)
		return
	}

	consideredIDs := map[string]bool{}
	for _, b := range considered {
		consideredIDs[b.ID] = true
	}

	options := buildReplanOptions(rest)
	candidates := make([][]replanCandidate, len(considered))
	for i, b := range considered {
		for _, opt := range options {
			if len(opt.tableIDs) == 1 && opt.tableIDs[0] == tableID {
				continue // the table being closed is never a valid assignment for a considered booking
			}
			if len(opt.tableIDs) == 2 && (opt.tableIDs[0] == tableID || opt.tableIDs[1] == tableID) {
				continue
			}
			cap := 0
			for _, tid := range opt.tableIDs {
				cap += b.AcceptedTerms.Capacities[tid]
			}
			if cap < b.PartySize {
				continue
			}
			if fixedConflict(rest, a.Store, consideredIDs, opt.tableIDs, b.StartsAt, b.EndsAt, newClosure) {
				continue
			}
			candidates[i] = append(candidates[i], replanCandidate{opt: opt, capacity: cap})
		}
	}

	best, found := solveReplan(considered, candidates)
	if !found {
		writeError(w, errNoFeasiblePlan)
		return
	}

	planID := idgen.New("plan")
	assignments := make([]store.PlanAssignment, len(considered))
	respAssignments := make([]planAssignmentResponse, len(considered))
	movedCount := 0
	unusedSeats := 0
	for i, b := range considered {
		changed := !sameTableSet(best[i].tableIDs, b.TableIDs)
		if changed {
			movedCount++
		}
		cap := 0
		for _, c := range candidates[i] {
			if c.opt.rank == best[i].rank {
				cap = c.capacity
				break
			}
		}
		unusedSeats += cap - b.PartySize
		assignments[i] = store.PlanAssignment{ReservationID: b.ID, TableIDs: best[i].tableIDs, Changed: changed}
		respAssignments[i] = planAssignmentResponse{Reference: b.Reference, TableIDs: best[i].tableIDs, Changed: changed}
	}

	plan := &store.Plan{
		ID: planID, RestaurantID: restID, Closure: newClosure, Assignments: assignments,
		MovedCount: movedCount, UnusedSeats: unusedSeats, RevisionAtPreview: rest.Revision,
	}
	a.Store.AddPlan(plan)

	resp := previewResponse{
		PlanID: planID, RestaurantRevision: rest.Revision,
		Closure:     closureResponse{TableID: tableID, From: formatRFC3339(from), To: formatRFC3339(to)},
		Assignments: respAssignments, MovedCount: movedCount, UnusedSeats: unusedSeats,
	}
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, path, key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}

type applyResponse struct {
	PlanID             string                `json:"plan_id"`
	RestaurantRevision int                   `json:"restaurant_revision"`
	Reservations       []reservationResponse `json:"reservations"`
}

func (a *API) handleApplyReplan(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	raw, berr := decodeBody(body)
	if berr != nil {
		writeError(w, berr)
		return
	}
	restID := r.PathValue("id")
	planID := r.PathValue("plan_id")

	a.Store.Lock()
	defer a.Store.Unlock()

	user, rest, perr := a.requireManager(r, restID)
	if perr != nil {
		writeError(w, perr)
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, errMissingIdempotency)
		return
	}
	if len(key) > 255 {
		writeError(w, errValidationFailed("Idempotency-Key must be at most 255 characters"))
		return
	}
	canonical := canonicalBody(raw)
	path := "/restaurants/" + restID + "/replans/" + planID + "/apply"
	if rec, ok := a.Store.IdempotencyGet(user.ID, http.MethodPost, path, key); ok {
		if rec.BodyHash == canonical {
			writeRaw(w, http.StatusOK, rec.ResponseBody)
			return
		}
		writeError(w, errIdempotencyKeyReuse)
		return
	}

	plan, ok := a.Store.PlanByID(planID)
	if !ok || plan.RestaurantID != restID {
		writeError(w, errNotFound)
		return
	}
	if plan.Applied {
		writeError(w, errPlanAlreadyApplied)
		return
	}
	if plan.RevisionAtPreview != rest.Revision {
		writeError(w, errStalePlan)
		return
	}

	now := time.Now().UTC()
	touchedSeries := map[string]bool{}
	reservations := make([]reservationResponse, len(plan.Assignments))
	for i, asg := range plan.Assignments {
		resv := a.Store.Reservations[asg.ReservationID]
		if asg.Changed {
			oldTableIDs := append([]string{}, resv.TableIDs...)
			resv.TableIDs = asg.TableIDs
			resv.Revision++
			resv.RecordReassigned(now, oldTableIDs, plan.ID)
			// A replan move preserves the occurrence's exception flag, scheduled date,
			// identity and accepted terms untouched -- it must NOT call onRealAmendment
			// (which marks a permanent exception; that's reserved for individual PATCH/moves).
			// Its series revision still bumps, but only once for the whole plan.
			if resv.SeriesID != "" {
				touchedSeries[resv.SeriesID] = true
			}
		}
		reservations[i] = toResponse(resv)
	}
	for sid := range touchedSeries {
		if sr, ok := a.Store.SeriesByID(sid); ok {
			sr.Revision++
		}
	}
	rest.Closures = append(rest.Closures, plan.Closure)
	rest.Revision++
	plan.Applied = true
	plan.AppliedByKey = key

	resp := applyResponse{PlanID: plan.ID, RestaurantRevision: rest.Revision, Reservations: reservations}
	respBytes, _ := json.Marshal(resp)
	a.Store.IdempotencyPut(user.ID, http.MethodPost, path, key, &store.IdempotencyRecord{
		BodyHash:     canonical,
		Status:       http.StatusCreated,
		ResponseBody: string(respBytes),
	})
	writeJSON(w, http.StatusCreated, resp)
}
