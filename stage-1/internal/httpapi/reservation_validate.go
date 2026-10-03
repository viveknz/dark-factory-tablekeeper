package httpapi

import (
	"time"

	"tablekeeper/internal/store"
	"tablekeeper/internal/timeutil"
)

// validateBooking checks a prospective (or amended) booking's restaurant/table/time/capacity
// against spec §8/§9, in this precedence: resource existence, local-time existence, opening
// hours & slot grid, party size vs capacity, then table occupancy. ignoreReservationID excludes
// that reservation from the overlap check (used by amendments re-checking their own slot).
func (a *API) validateBooking(restaurantID, tableID, startsAtLocal string, partySize int, ignoreReservationID string) (rest *store.Restaurant, tbl *store.Table, startsAt, endsAt time.Time, apiErr *apiError) {
	rest, tbl, startsAt, endsAt, apiErr = a.resolveBookingFields(restaurantID, tableID, startsAtLocal, partySize)
	if apiErr != nil {
		return nil, nil, time.Time{}, time.Time{}, apiErr
	}
	if !a.Store.TableAvailable(rest.ID, tbl.ID, startsAt, endsAt, ignoreReservationID) {
		return nil, nil, time.Time{}, time.Time{}, errTableUnavailable
	}
	return rest, tbl, startsAt, endsAt, nil
}

// resolveBookingFields validates everything about a prospective booking except table
// occupancy (overlap), so batch callers (reservation-moves) can resolve every item first and
// run a combined overlap check across the whole batch afterward.
func (a *API) resolveBookingFields(restaurantID, tableID, startsAtLocal string, partySize int) (rest *store.Restaurant, tbl *store.Table, startsAt, endsAt time.Time, apiErr *apiError) {
	rest, ok := a.Store.Restaurants[restaurantID]
	if !ok {
		return nil, nil, time.Time{}, time.Time{}, errNotFound
	}
	var table *store.Table
	for i := range rest.Tables {
		if rest.Tables[i].ID == tableID {
			table = &rest.Tables[i]
			break
		}
	}
	if table == nil {
		return nil, nil, time.Time{}, time.Time{}, errNotFound
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

	if partySize > table.Capacity {
		return nil, nil, time.Time{}, time.Time{}, errPartyExceedsCapacity
	}

	return rest, table, start, end, nil
}
