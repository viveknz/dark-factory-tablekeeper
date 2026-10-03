package store

import (
	"encoding/json"
	"time"
)

// OpeningHour is one weekday's opening window, local time, at a restaurant.
type OpeningHour struct {
	Weekday string `json:"weekday"`
	Opens   string `json:"opens"`
	Closes  string `json:"closes"`
}

// Table is one bookable table at a restaurant.
type Table struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Capacity int    `json:"capacity"`
}

// Restaurant is a seeded restaurant and its tables.
type Restaurant struct {
	ID                         string        `json:"id"`
	Name                       string        `json:"name"`
	Timezone                   string        `json:"timezone"`
	SlotMinutes                int           `json:"slot_minutes"`
	ReservationDurationMinutes int           `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int           `json:"cancellation_cutoff_minutes"`
	OpeningHours               []OpeningHour `json:"opening_hours"`
	Tables                     []Table       `json:"tables"`
	// Combinable lists declared pairs of table ids that may be booked together as one
	// reservation. Each entry has exactly two ids; combining is not transitive.
	Combinable [][]string `json:"combinable"`
	// ManagerUserIDs may publish policies for this restaurant. Absent/omitted on import
	// (stage-1/stage-2 exports) defaults to empty via the zero value, per spec.
	ManagerUserIDs []string `json:"manager_user_ids"`
	// Policies are published policies in publication order (not effective-date order);
	// PolicyVersion starts at 1 and is never reused. Policy 0 (the original fixture rules) is
	// implicit and never stored here -- see Policy0().
	Policies []Policy `json:"policies"`
	// Revision is a counter distinct from any reservation's own revision: it tracks
	// restaurant-level operations (new booking, real amendment, cancellation, policy
	// publication, series adoption, a changed reservation-moves batch, plan application) that
	// bump it exactly once per successful operation. Surfaced as "restaurant_revision" in
	// stage-4's replan/apply responses; not exposed anywhere else (stage 3 tracked it purely
	// internally, since no response field existed for it yet).
	Revision int `json:"revision"`
	// Closures are applied (not merely previewed) table closures: a half-open local interval
	// during which that table is excluded from availability and new bookings, exactly like an
	// occupying reservation.
	Closures []Closure `json:"closures"`
}

// Closure is one applied seating-plan closure of a single table for a half-open interval.
type Closure struct {
	TableID string    `json:"table_id"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
}

// Overlaps reports whether this closure's interval overlaps [start,end).
func (c Closure) Overlaps(start, end time.Time) bool {
	return start.Before(c.To) && c.From.Before(end)
}

// Policy is a published, immutable set of booking rules effective from a given date.
type Policy struct {
	PolicyVersion              int            `json:"policy_version"`
	EffectiveFrom              string         `json:"effective_from"`
	SlotMinutes                int            `json:"slot_minutes"`
	ReservationDurationMinutes int            `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int            `json:"cancellation_cutoff_minutes"`
	OpeningHours               []OpeningHour  `json:"opening_hours"`
	Capacities                 map[string]int `json:"capacities"`
}

// AcceptedTerms is a snapshot of the policy selected for a reservation at creation/amendment
// time: the entire selected policy, excluding effective_from (policies are immutable and a
// reservation keeps its own snapshot forever, so effective_from is meaningless here).
type AcceptedTerms struct {
	PolicyVersion              int            `json:"policy_version"`
	SlotMinutes                int            `json:"slot_minutes"`
	ReservationDurationMinutes int            `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int            `json:"cancellation_cutoff_minutes"`
	OpeningHours               []OpeningHour  `json:"opening_hours"`
	Capacities                 map[string]int `json:"capacities"`
}

// Policy0 returns the implicit policy 0: the restaurant's original fixture rules, with
// capacities derived from each table's seeded capacity.
func (r *Restaurant) Policy0() AcceptedTerms {
	caps := map[string]int{}
	for _, t := range r.Tables {
		caps[t.ID] = t.Capacity
	}
	return AcceptedTerms{
		PolicyVersion:              0,
		SlotMinutes:                r.SlotMinutes,
		ReservationDurationMinutes: r.ReservationDurationMinutes,
		CancellationCutoffMinutes:  r.CancellationCutoffMinutes,
		OpeningHours:               r.OpeningHours,
		Capacities:                 caps,
	}
}

// SelectPolicy returns the terms applicable to a booking whose local start date is dateStr
// (YYYY-MM-DD): the published policy with the greatest effective_from not later than dateStr,
// ties broken by the greatest policy_version; policy 0 if no published policy applies yet.
// YYYY-MM-DD strings compare correctly with ordinary string comparison.
func (r *Restaurant) SelectPolicy(dateStr string) AcceptedTerms {
	best := r.Policy0()
	bestFrom := "" // policy 0 has no effective_from; any real policy's date sorts after "".
	for _, p := range r.Policies {
		if p.EffectiveFrom > dateStr {
			continue
		}
		if p.EffectiveFrom > bestFrom || (p.EffectiveFrom == bestFrom && p.PolicyVersion > best.PolicyVersion) {
			bestFrom = p.EffectiveFrom
			best = AcceptedTerms{
				PolicyVersion:              p.PolicyVersion,
				SlotMinutes:                p.SlotMinutes,
				ReservationDurationMinutes: p.ReservationDurationMinutes,
				CancellationCutoffMinutes:  p.CancellationCutoffMinutes,
				OpeningHours:               p.OpeningHours,
				Capacities:                 p.Capacities,
			}
		}
	}
	return best
}

// IsManager reports whether userID may publish policies for this restaurant.
func (r *Restaurant) IsManager(userID string) bool {
	for _, id := range r.ManagerUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// FindTable returns the table with the given id, if any.
func (r *Restaurant) FindTable(id string) (*Table, bool) {
	for i := range r.Tables {
		if r.Tables[i].ID == id {
			return &r.Tables[i], true
		}
	}
	return nil, false
}

// CombinablePair reports whether ids (in any order) are a declared combinable pair, and
// returns them in the declared (combinable-list) order if so.
func (r *Restaurant) CombinablePair(a, b string) ([]string, bool) {
	for _, pair := range r.Combinable {
		if len(pair) != 2 {
			continue
		}
		if (pair[0] == a && pair[1] == b) || (pair[0] == b && pair[1] == a) {
			return []string{pair[0], pair[1]}, true
		}
	}
	return nil, false
}

// User is a registered diner.
type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash []byte `json:"password_hash"`
	DisplayName  string `json:"display_name"`
}

// Reservation is a booking, confirmed or cancelled, occupying one or two tables (a declared
// combination) for its full duration.
type Reservation struct {
	ID            string        `json:"id"`
	Reference     string        `json:"reference"`
	RestaurantID  string        `json:"restaurant_id"`
	TableIDs      []string      `json:"table_ids"`
	UserID        string        `json:"user_id"`
	PartySize     int           `json:"party_size"`
	Status        string        `json:"status"` // "confirmed" | "cancelled"
	StartsAtLocal string        `json:"starts_at_local"`
	StartsAt      time.Time     `json:"starts_at"`
	EndsAt        time.Time     `json:"ends_at"`
	CreatedAt     time.Time     `json:"created_at"`
	Revision      int           `json:"revision"`
	AcceptedTerms AcceptedTerms `json:"accepted_terms"`
	History       []HistoryEntry `json:"history"`
	// SeriesID is non-empty once this reservation is an occurrence of a recurring series
	// (including occurrence 0, the anchor). SeriesIndex is its position within that series.
	SeriesID     string `json:"series_id,omitempty"`
	SeriesIndex  int    `json:"series_index"`
	IsException  bool   `json:"is_exception"`
}

// Change is one field's before/after value in a history entry. From/To hold a string,
// []string (table_ids), int (party_size) or nil, so they marshal directly as the right JSON
// type without a wrapper.
type Change struct {
	Field string `json:"field"`
	From  interface{} `json:"from"`
	To    interface{} `json:"to"`
}

// HistoryEntry is one immutable record in a reservation's history.
type HistoryEntry struct {
	Seq           int           `json:"seq"`
	At            time.Time     `json:"at"`
	Event         string        `json:"event"` // "created" | "changed" | "cancelled" | "reassigned"
	Changes       []Change      `json:"changes"`
	Revision      int           `json:"revision"`
	AcceptedTerms AcceptedTerms `json:"accepted_terms"`
	PlanID        string        `json:"plan_id,omitempty"` // set only on a "reassigned" entry
}

// tableIDField returns the history change field name and value for a table-id set: "table_id"
// with a bare string for a single table (nil if empty, for a "created" from-value), or
// "table_ids" with the full list for a pair.
func tableIDField(ids []string) (field string, value interface{}) {
	if len(ids) == 1 {
		return "table_id", ids[0]
	}
	if len(ids) == 0 {
		return "table_id", nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return "table_ids", out
}

// sameTableSet reports whether a and b name the same tables regardless of order -- a
// reversed-but-same-set combo is not a change (spec: "not an amendment on its own").
func sameTableSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, x := range a {
		seen[x] = true
	}
	for _, y := range b {
		if !seen[y] {
			return false
		}
	}
	return true
}

// RecordCreated appends the initial "created" history entry (seq 1) at the reservation's
// current fields/revision/terms.
func (r *Reservation) RecordCreated(at time.Time) {
	field, val := tableIDField(r.TableIDs)
	r.History = append(r.History, HistoryEntry{
		Seq:   len(r.History) + 1,
		At:    at,
		Event: "created",
		Changes: []Change{
			{Field: field, From: nil, To: val},
			{Field: "starts_at_local", From: nil, To: r.StartsAtLocal},
			{Field: "party_size", From: nil, To: r.PartySize},
		},
		Revision:      r.Revision,
		AcceptedTerms: r.AcceptedTerms,
	})
}

// RecordChanged appends a "changed" entry naming only the fields that actually differ from
// (oldTableIDs, oldStartsAtLocal, oldPartySize) to the reservation's current values, in the
// fixed order table_id/table_ids, starts_at_local, party_size. Appends nothing (a true no-op)
// if nothing differs; the table-id comparison is set-based so a reversed-but-same-set pair
// does not count as a change.
func (r *Reservation) RecordChanged(at time.Time, oldTableIDs []string, oldStartsAtLocal string, oldPartySize int) {
	var changes []Change
	if !sameTableSet(oldTableIDs, r.TableIDs) {
		// If the set is the same (reversed) we must not emit a table change -- but if it
		// differs, report using the NEW field name/shape (table_id vs table_ids), with the
		// old value reshaped to match (e.g. a single table becoming a pair: table_ids
		// from/to, with "from" as a one-element list).
		field, newVal := tableIDField(r.TableIDs)
		var oldVal interface{}
		if field == "table_ids" {
			oldList := make([]string, len(oldTableIDs))
			copy(oldList, oldTableIDs)
			oldVal = oldList
		} else if len(oldTableIDs) > 0 {
			oldVal = oldTableIDs[0]
		}
		changes = append(changes, Change{Field: field, From: oldVal, To: newVal})
	}
	if oldStartsAtLocal != r.StartsAtLocal {
		changes = append(changes, Change{Field: "starts_at_local", From: oldStartsAtLocal, To: r.StartsAtLocal})
	}
	if oldPartySize != r.PartySize {
		changes = append(changes, Change{Field: "party_size", From: oldPartySize, To: r.PartySize})
	}
	if len(changes) == 0 {
		return
	}
	r.History = append(r.History, HistoryEntry{
		Seq:           len(r.History) + 1,
		At:            at,
		Event:         "changed",
		Changes:       changes,
		Revision:      r.Revision,
		AcceptedTerms: r.AcceptedTerms,
	})
}

// RecordReassigned appends a "reassigned" entry: a plan application moved this reservation to
// a new table set, with times/party/accepted-terms left byte-identical.
func (r *Reservation) RecordReassigned(at time.Time, oldTableIDs []string, planID string) {
	field, newVal := tableIDField(r.TableIDs)
	var oldVal interface{}
	if field == "table_ids" {
		oldList := make([]string, len(oldTableIDs))
		copy(oldList, oldTableIDs)
		oldVal = oldList
	} else if len(oldTableIDs) > 0 {
		oldVal = oldTableIDs[0]
	}
	r.History = append(r.History, HistoryEntry{
		Seq:           len(r.History) + 1,
		At:            at,
		Event:         "reassigned",
		Changes:       []Change{{Field: field, From: oldVal, To: newVal}},
		Revision:      r.Revision,
		AcceptedTerms: r.AcceptedTerms,
		PlanID:        planID,
	})
}

// RecordCancelled appends the terminal "cancelled" entry.
func (r *Reservation) RecordCancelled(at time.Time) {
	r.History = append(r.History, HistoryEntry{
		Seq:           len(r.History) + 1,
		At:            at,
		Event:         "cancelled",
		Changes:       []Change{},
		Revision:      r.Revision,
		AcceptedTerms: r.AcceptedTerms,
	})
}

// Series is a recurring-reservation agreement: occurrence 0 is the anchor reservation itself.
type Series struct {
	ID            string   `json:"id"`
	UserID        string   `json:"user_id"`
	RestaurantID  string   `json:"restaurant_id"`
	Revision      int      `json:"revision"`
	IntervalWeeks int      `json:"interval_weeks"`
	// ReservationIDs are this series' occurrences' reservation IDs, in index order.
	ReservationIDs []string `json:"reservation_ids"`
}

// PlanAssignment is one considered booking's chosen table assignment within a seating-change
// plan, computed at preview time and replayed unchanged at apply time (apply never
// re-optimizes; it only re-checks that nothing relevant has changed via the restaurant
// revision stamped at preview time).
type PlanAssignment struct {
	ReservationID string   `json:"reservation_id"`
	TableIDs      []string `json:"table_ids"`
	Changed       bool     `json:"changed"`
}

// Plan is a previewed (and possibly later applied) seating-change plan.
type Plan struct {
	ID                string
	RestaurantID      string
	Closure           Closure
	Assignments       []PlanAssignment
	MovedCount        int
	UnusedSeats       int
	RevisionAtPreview int // the restaurant's Revision when this plan was computed
	Applied           bool
	AppliedByKey      string // the idempotency key that successfully applied it, if any
}
func (r *Reservation) Overlaps(start, end time.Time) bool {
	return r.StartsAt.Before(end) && start.Before(r.EndsAt)
}

// HasTable reports whether id is one of this reservation's occupied tables.
func (r *Reservation) HasTable(id string) bool {
	for _, t := range r.TableIDs {
		if t == id {
			return true
		}
	}
	return false
}

// UnmarshalJSON accepts both the stage-2 shape (table_ids) and a stage-1-shaped export
// (singular table_id), so a stage-1 export remains importable after the upgrade (spec
// "Existing clients after an upgrade").
func (r *Reservation) UnmarshalJSON(data []byte) error {
	type alias struct {
		ID            string         `json:"id"`
		Reference     string         `json:"reference"`
		RestaurantID  string         `json:"restaurant_id"`
		TableIDs      []string       `json:"table_ids"`
		TableID       *string        `json:"table_id"`
		UserID        string         `json:"user_id"`
		PartySize     int            `json:"party_size"`
		Status        string         `json:"status"`
		StartsAtLocal string         `json:"starts_at_local"`
		StartsAt      time.Time      `json:"starts_at"`
		EndsAt        time.Time      `json:"ends_at"`
		CreatedAt     time.Time      `json:"created_at"`
		Revision      int            `json:"revision"`
		AcceptedTerms AcceptedTerms  `json:"accepted_terms"`
		History       []HistoryEntry `json:"history"`
		SeriesID      string         `json:"series_id"`
		SeriesIndex   int            `json:"series_index"`
		IsException   bool           `json:"is_exception"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = Reservation{
		ID:            a.ID,
		Reference:     a.Reference,
		RestaurantID:  a.RestaurantID,
		UserID:        a.UserID,
		PartySize:     a.PartySize,
		Status:        a.Status,
		StartsAtLocal: a.StartsAtLocal,
		StartsAt:      a.StartsAt,
		EndsAt:        a.EndsAt,
		CreatedAt:     a.CreatedAt,
		Revision:      a.Revision,
		AcceptedTerms: a.AcceptedTerms,
		History:       a.History,
		SeriesID:      a.SeriesID,
		SeriesIndex:   a.SeriesIndex,
		IsException:   a.IsException,
	}
	if len(a.TableIDs) > 0 {
		r.TableIDs = a.TableIDs
	} else if a.TableID != nil {
		r.TableIDs = []string{*a.TableID}
	}
	return nil
}

// IdempotencyRecord remembers the first outcome of a key use, scoped to one user/method/path.
type IdempotencyRecord struct {
	BodyHash     string `json:"body_hash"`
	Status       int    `json:"status"`
	ResponseBody string `json:"response_body"` // raw JSON text of the stored response
}
