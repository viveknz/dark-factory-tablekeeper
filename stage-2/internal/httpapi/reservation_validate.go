package httpapi

import (
	"time"

	"tablekeeper/internal/store"
	"tablekeeper/internal/timeutil"
)

// validateBooking checks a prospective (or amended) booking's restaurant/tables/time/capacity
// against spec §8/§9 and stage 2's combined-table rules, in this precedence: shape (duplicate
// table ids), resource existence, combination declared, local-time existence, opening hours &
// slot grid, party size vs summed capacity, then table occupancy for every selected table.
// ignoreReservationID excludes that reservation from the overlap check (used by amendments
// re-checking their own slot).
func (a *API) validateBooking(restaurantID string, tableIDs []string, startsAtLocal string, partySize int, ignoreReservationID string) (rest *store.Restaurant, tables []*store.Table, startsAt, endsAt time.Time, apiErr *apiError) {
	rest, tables, startsAt, endsAt, apiErr = a.resolveBookingFields(restaurantID, tableIDs, startsAtLocal, partySize)
	if apiErr != nil {
		return nil, nil, time.Time{}, time.Time{}, apiErr
	}
	for _, t := range tables {
		if !a.Store.TableAvailable(rest.ID, t.ID, startsAt, endsAt, ignoreReservationID) {
			return nil, nil, time.Time{}, time.Time{}, errTableUnavailable
		}
	}
	return rest, tables, startsAt, endsAt, nil
}

// resolveBookingFields validates everything about a prospective booking except table
// occupancy (overlap), so batch callers (reservation-moves) can resolve every item first and
// run a combined overlap check across the whole batch afterward.
func (a *API) resolveBookingFields(restaurantID string, tableIDs []string, startsAtLocal string, partySize int) (rest *store.Restaurant, tables []*store.Table, startsAt, endsAt time.Time, apiErr *apiError) {
	if hasDuplicate(tableIDs) {
		return nil, nil, time.Time{}, time.Time{}, errValidationFailed("duplicate table id in the set")
	}

	rest, ok := a.Store.Restaurants[restaurantID]
	if !ok {
		return nil, nil, time.Time{}, time.Time{}, errNotFound
	}

	tables = make([]*store.Table, 0, len(tableIDs))
	for _, id := range tableIDs {
		t, found := rest.FindTable(id)
		if !found {
			return nil, nil, time.Time{}, time.Time{}, errNotFound
		}
		tables = append(tables, t)
	}

	switch len(tableIDs) {
	case 1:
		// A single table is always a valid "combination" of one.
	case 2:
		if _, ok := rest.CombinablePair(tableIDs[0], tableIDs[1]); !ok {
			return nil, nil, time.Time{}, time.Time{}, errCombinationNotAllowed
		}
	default:
		return nil, nil, time.Time{}, time.Time{}, errCombinationNotAllowed
	}

	loc, lerr := time.LoadLocation(rest.Timezone)
	if lerr != nil {
		return nil, nil, time.Time{}, time.Time{}, errValidationFailed("restaurant has an invalid timezone")
	}

	y, m, d, hh, mi, ok := splitLocal(startsAtLocal)
	if !ok {
		return nil, nil, time.Time{}, time.Time{}, errValidationFailed("starts_at_local must be a bare local YYYY-MM-DDTHH:MM")
	}

	start, rerr := timeutil.ResolveLocal(loc, y, time.Month(m), d, hh, mi)
	if rerr != nil {
		return nil, nil, time.Time{}, time.Time{}, errInvalidLocalTime
	}
	end := start.Add(time.Duration(rest.ReservationDurationMinutes) * time.Minute)

	noon := time.Date(y, time.Month(m), d, 12, 0, 0, 0, loc)
	wd := weekdayCode(noon.Weekday())
	oh, hasHours := openingHourFor(rest, wd)
	localMinutes := hh*60 + mi
	if !hasHours {
		return nil, nil, time.Time{}, time.Time{}, errOutsideOpeningHours
	}
	openH, openM, _ := parseHHMM(oh.Opens)
	closeH, closeM, _ := parseHHMM(oh.Closes)
	openMin := openH*60 + openM
	closeMin := closeH*60 + closeM
	if localMinutes < openMin || localMinutes+rest.ReservationDurationMinutes > closeMin {
		return nil, nil, time.Time{}, time.Time{}, errOutsideOpeningHours
	}
	if (localMinutes-openMin)%rest.SlotMinutes != 0 {
		return nil, nil, time.Time{}, time.Time{}, errNotOnSlotGrid
	}

	totalCapacity := 0
	for _, t := range tables {
		totalCapacity += t.Capacity
	}
	if partySize > totalCapacity {
		return nil, nil, time.Time{}, time.Time{}, errPartyExceedsCapacity
	}

	return rest, tables, start, end, nil
}

func tableIDsOf(tables []*store.Table) []string {
	ids := make([]string, len(tables))
	for i, t := range tables {
		ids[i] = t.ID
	}
	return ids
}
