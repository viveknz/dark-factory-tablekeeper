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
	ID            string    `json:"id"`
	Reference     string    `json:"reference"`
	RestaurantID  string    `json:"restaurant_id"`
	TableIDs      []string  `json:"table_ids"`
	UserID        string    `json:"user_id"`
	PartySize     int       `json:"party_size"`
	Status        string    `json:"status"` // "confirmed" | "cancelled"
	StartsAtLocal string    `json:"starts_at_local"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// Overlaps reports whether this reservation's occupancy interval overlaps [start,end).
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
		ID            string    `json:"id"`
		Reference     string    `json:"reference"`
		RestaurantID  string    `json:"restaurant_id"`
		TableIDs      []string  `json:"table_ids"`
		TableID       *string   `json:"table_id"`
		UserID        string    `json:"user_id"`
		PartySize     int       `json:"party_size"`
		Status        string    `json:"status"`
		StartsAtLocal string    `json:"starts_at_local"`
		StartsAt      time.Time `json:"starts_at"`
		EndsAt        time.Time `json:"ends_at"`
		CreatedAt     time.Time `json:"created_at"`
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
