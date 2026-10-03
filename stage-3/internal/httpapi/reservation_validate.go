package httpapi

import (
	"fmt"
	"time"

	"tablekeeper/internal/store"
	"tablekeeper/internal/timeutil"
)

// validateBooking checks a prospective (or amended) booking's restaurant/tables/time/capacity
// against spec §8/§9, stage 2's combined-table rules, and stage 3's policy-selected terms, in
// this precedence: shape (duplicate table ids), resource existence, combination declared,
// local-time existence, opening hours & slot grid (per the resulting date's policy), party size
// vs that policy's summed capacity, then table occupancy for every selected table.
// ignoreReservationID excludes that reservation from the overlap check (used by amendments
// re-checking their own slot).
func (a *API) validateBooking(restaurantID string, tableIDs []string, startsAtLocal string, partySize int, ignoreReservationID string) (rest *store.Restaurant, tables []*store.Table, startsAt, endsAt time.Time, terms store.AcceptedTerms, apiErr *apiError) {
	rest, tables, startsAt, endsAt, terms, apiErr = a.resolveBookingFields(restaurantID, tableIDs, startsAtLocal, partySize)
	if apiErr != nil {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, apiErr
	}
	for _, t := range tables {
		if !a.Store.TableAvailable(rest.ID, t.ID, startsAt, endsAt, ignoreReservationID) {
			return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errTableUnavailable
		}
	}
	return rest, tables, startsAt, endsAt, terms, nil
}

// resolveBookingFields validates everything about a prospective booking except table
// occupancy (overlap), so batch callers (reservation-moves) can resolve every item first and
// run a combined overlap check across the whole batch afterward. It also selects and returns
// the policy terms applicable to the booking's local start date -- opening hours, slot grid,
// duration and capacities all come from that policy (or policy 0), never the restaurant's raw
// fixture fields directly.
func (a *API) resolveBookingFields(restaurantID string, tableIDs []string, startsAtLocal string, partySize int) (rest *store.Restaurant, tables []*store.Table, startsAt, endsAt time.Time, terms store.AcceptedTerms, apiErr *apiError) {
	if hasDuplicate(tableIDs) {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errValidationFailed("duplicate table id in the set")
	}

	rest, ok := a.Store.Restaurants[restaurantID]
	if !ok {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errNotFound
	}

	for _, id := range tableIDs {
		if _, found := rest.FindTable(id); !found {
			return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errNotFound
		}
	}

	// Canonicalize a pair's order to the declared combinable order, regardless of input order
	// ("a reversed-but-same-set input pair... names the same set" -- stage 3 history rules
	// depend on this canonical order being used consistently everywhere, not just in
	// availability's available_options).
	switch len(tableIDs) {
	case 1:
		// A single table is always a valid "combination" of one.
	case 2:
		pair, ok := rest.CombinablePair(tableIDs[0], tableIDs[1])
		if !ok {
			return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errCombinationNotAllowed
		}
		tableIDs = pair
	default:
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errCombinationNotAllowed
	}

	tables = make([]*store.Table, 0, len(tableIDs))
	for _, id := range tableIDs {
		t, _ := rest.FindTable(id)
		tables = append(tables, t)
	}

	loc, lerr := time.LoadLocation(rest.Timezone)
	if lerr != nil {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errValidationFailed("restaurant has an invalid timezone")
	}

	y, m, d, hh, mi, ok := splitLocal(startsAtLocal)
	if !ok {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errValidationFailed("starts_at_local must be a bare local YYYY-MM-DDTHH:MM")
	}

	start, rerr := timeutil.ResolveLocal(loc, y, time.Month(m), d, hh, mi)
	if rerr != nil {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errInvalidLocalTime
	}

	terms = rest.SelectPolicy(fmt.Sprintf("%04d-%02d-%02d", y, m, d))
	end := start.Add(time.Duration(terms.ReservationDurationMinutes) * time.Minute)

	noon := time.Date(y, time.Month(m), d, 12, 0, 0, 0, loc)
	wd := weekdayCode(noon.Weekday())
	oh, hasHours := openingHourFor(terms.OpeningHours, wd)
	localMinutes := hh*60 + mi
	if !hasHours {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errOutsideOpeningHours
	}
	openH, openM, _ := parseHHMM(oh.Opens)
	closeH, closeM, _ := parseHHMM(oh.Closes)
	openMin := openH*60 + openM
	closeMin := closeH*60 + closeM
	if localMinutes < openMin || localMinutes+terms.ReservationDurationMinutes > closeMin {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errOutsideOpeningHours
	}
	if (localMinutes-openMin)%terms.SlotMinutes != 0 {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errNotOnSlotGrid
	}

	totalCapacity := 0
	for _, t := range tables {
		totalCapacity += terms.Capacities[t.ID]
	}
	if partySize > totalCapacity {
		return nil, nil, time.Time{}, time.Time{}, store.AcceptedTerms{}, errPartyExceedsCapacity
	}

	return rest, tables, start, end, terms, nil
}

func tableIDsOf(tables []*store.Table) []string {
	ids := make([]string, len(tables))
	for i, t := range tables {
		ids[i] = t.ID
	}
	return ids
}
