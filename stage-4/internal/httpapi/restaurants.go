package httpapi

import (
	"net/http"
	"time"

	"tablekeeper/internal/store"
)

type restaurantSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
}

func (a *API) handleListRestaurants(w http.ResponseWriter, r *http.Request) {
	a.Store.Lock()
	defer a.Store.Unlock()
	out := []restaurantSummary{}
	for _, rest := range a.Store.RestaurantsInOrder() {
		out = append(out, restaurantSummary{ID: rest.ID, Name: rest.Name, Timezone: rest.Timezone})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"restaurants": out})
}

type restaurantDetail struct {
	ID                         string              `json:"id"`
	Name                       string              `json:"name"`
	Timezone                   string              `json:"timezone"`
	SlotMinutes                int                 `json:"slot_minutes"`
	ReservationDurationMinutes int                 `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int                 `json:"cancellation_cutoff_minutes"`
	OpeningHours               []store.OpeningHour `json:"opening_hours"`
	Tables                     []store.Table       `json:"tables"`
	Combinable                 [][]string          `json:"combinable"`
}

func (a *API) handleGetRestaurant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.Store.Lock()
	defer a.Store.Unlock()
	rest, ok := a.Store.Restaurants[id]
	if !ok {
		writeError(w, errNotFound)
		return
	}
	writeJSON(w, http.StatusOK, restaurantDetail{
		ID:                         rest.ID,
		Name:                       rest.Name,
		Timezone:                   rest.Timezone,
		SlotMinutes:                rest.SlotMinutes,
		ReservationDurationMinutes: rest.ReservationDurationMinutes,
		CancellationCutoffMinutes:  rest.CancellationCutoffMinutes,
		OpeningHours:               rest.OpeningHours,
		Tables:                     rest.Tables,
		Combinable:                 rest.Combinable,
	})
}

type availabilityOption struct {
	TableIDs []string `json:"table_ids"`
	Capacity int      `json:"capacity"`
}

type explainRule struct {
	Rule  string `json:"rule"`
	Holds bool   `json:"holds"`
}

type explainTable struct {
	TableID       string        `json:"table_id"`
	PolicyVersion int           `json:"policy_version"`
	Available     bool          `json:"available"`
	Rules         []explainRule `json:"rules"`
}

type availabilitySlot struct {
	StartsAtLocal     string                `json:"starts_at_local"`
	StartsAt          string                `json:"starts_at"`
	AvailableTableIDs []string              `json:"available_table_ids"`
	AvailableOptions  []availabilityOption  `json:"available_options"`
	Explain           []explainTable        `json:"explain,omitempty"`
}

type availabilityResponse struct {
	RestaurantID string             `json:"restaurant_id"`
	Date         string             `json:"date"`
	Timezone     string             `json:"timezone"`
	Slots        []availabilitySlot `json:"slots"`
}

func (a *API) handleAvailability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	restaurantID := q.Get("restaurant_id")
	date := q.Get("date")
	partySizeStr := q.Get("party_size")

	if restaurantID == "" || date == "" || partySizeStr == "" {
		writeError(w, errValidationFailed("restaurant_id, date and party_size are all required"))
		return
	}
	if !isValidCalendarDateString(date) {
		writeError(w, errValidationFailed("date must be a valid YYYY-MM-DD calendar date"))
		return
	}
	partySize, ok := queryInt(partySizeStr)
	if !ok || partySize < 1 {
		writeError(w, errValidationFailed("party_size must be a positive integer"))
		return
	}
	explain := false
	if explainStr, present := q["explain"]; present {
		if len(explainStr) != 1 || explainStr[0] != "true" {
			writeError(w, errValidationFailed("explain, if given, must be exactly \"true\""))
			return
		}
		explain = true
	}

	a.Store.Lock()
	defer a.Store.Unlock()

	rest, ok := a.Store.Restaurants[restaurantID]
	if !ok {
		writeError(w, errNotFound)
		return
	}
	loc, err := time.LoadLocation(rest.Timezone)
	if err != nil {
		writeError(w, errValidationFailed("restaurant has an invalid timezone"))
		return
	}

	y, m, d, dok := parseDate(date)
	if !dok {
		writeError(w, errValidationFailed("date must be YYYY-MM-DD"))
		return
	}
	// Determine the weekday of this local calendar date in the restaurant's timezone, and the
	// policy applicable to this date -- opening hours, slot grid, duration and capacities all
	// come from it (policy 0 unless a published policy applies).
	noon := time.Date(y, time.Month(m), d, 12, 0, 0, 0, loc)
	wd := weekdayCode(noon.Weekday())
	terms := rest.SelectPolicy(date)

	slots := []availabilitySlot{}
	oh, hasHours := openingHourFor(terms.OpeningHours, wd)
	if hasHours {
		for _, localHHMM := range slotTimesForDay(oh, terms.SlotMinutes, terms.ReservationDurationMinutes) {
			localStr := date + "T" + localHHMM
			startsAt, rerr := resolveLocalString(loc, localStr)
			if rerr != nil {
				continue // skipped local time (spring-forward gap): never appears
			}
			endsAt := startsAt.Add(time.Duration(terms.ReservationDurationMinutes) * time.Minute)
			available := []string{}
			options := []availabilityOption{}
			var explainTables []explainTable
			for _, t := range rest.Tables {
				capacityHolds := terms.Capacities[t.ID] >= partySize
				overlapHolds := a.Store.TableAvailable(rest.ID, t.ID, startsAt, endsAt, "")
				isAvailable := capacityHolds && overlapHolds
				if isAvailable {
					available = append(available, t.ID)
					options = append(options, availabilityOption{TableIDs: []string{t.ID}, Capacity: terms.Capacities[t.ID]})
				}
				if explain {
					explainTables = append(explainTables, explainTable{
						TableID:       t.ID,
						PolicyVersion: terms.PolicyVersion,
						Available:     isAvailable,
						Rules: []explainRule{
							{Rule: "capacity", Holds: capacityHolds},
							{Rule: "no_overlap", Holds: overlapHolds},
						},
					})
				}
			}
			for _, pair := range rest.Combinable {
				if len(pair) != 2 {
					continue
				}
				t1, ok1 := rest.FindTable(pair[0])
				t2, ok2 := rest.FindTable(pair[1])
				if !ok1 || !ok2 {
					continue
				}
				capacity := terms.Capacities[t1.ID] + terms.Capacities[t2.ID]
				if capacity < partySize {
					continue
				}
				if a.Store.TableAvailable(rest.ID, t1.ID, startsAt, endsAt, "") && a.Store.TableAvailable(rest.ID, t2.ID, startsAt, endsAt, "") {
					options = append(options, availabilityOption{TableIDs: []string{pair[0], pair[1]}, Capacity: capacity})
				}
			}
			slots = append(slots, availabilitySlot{
				StartsAtLocal:     localStr,
				StartsAt:          formatRFC3339(startsAt),
				AvailableTableIDs: available,
				AvailableOptions:  options,
				Explain:           explainTables,
			})
		}
	}

	writeJSON(w, http.StatusOK, availabilityResponse{
		RestaurantID: rest.ID,
		Date:         date,
		Timezone:     rest.Timezone,
		Slots:        slots,
	})
}

func formatRFC3339(t time.Time) string {
	return t.Format("2006-01-02T15:04:05Z07:00")
}
