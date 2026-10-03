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
	ID                         string               `json:"id"`
	Name                       string               `json:"name"`
	Timezone                   string               `json:"timezone"`
	SlotMinutes                int                  `json:"slot_minutes"`
	ReservationDurationMinutes int                  `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int                  `json:"cancellation_cutoff_minutes"`
	OpeningHours               []store.OpeningHour  `json:"opening_hours"`
	Tables                     []store.Table        `json:"tables"`
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
	})
}

type availabilitySlot struct {
	StartsAtLocal      string   `json:"starts_at_local"`
	StartsAt           string   `json:"starts_at"`
	AvailableTableIDs  []string `json:"available_table_ids"`
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
	// Determine the weekday of this local calendar date in the restaurant's timezone.
	noon := time.Date(y, time.Month(m), d, 12, 0, 0, 0, loc)
	wd := weekdayCode(noon.Weekday())

	slots := []availabilitySlot{}
	oh, hasHours := openingHourFor(rest, wd)
	if hasHours {
		for _, localHHMM := range slotTimesForDay(oh, rest.SlotMinutes, rest.ReservationDurationMinutes) {
			localStr := date + "T" + localHHMM
			startsAt, rerr := resolveLocalString(loc, localStr)
			if rerr != nil {
				continue // skipped local time (spring-forward gap): never appears
			}
			endsAt := startsAt.Add(time.Duration(rest.ReservationDurationMinutes) * time.Minute)
			available := []string{}
			for _, t := range rest.Tables {
				if t.Capacity < partySize {
					continue
				}
				if a.Store.TableAvailable(rest.ID, t.ID, startsAt, endsAt, "") {
					available = append(available, t.ID)
				}
			}
			slots = append(slots, availabilitySlot{
				StartsAtLocal:     localStr,
				StartsAt:          formatRFC3339(startsAt),
				AvailableTableIDs: available,
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
