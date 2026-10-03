package store

import "time"

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
	ID                          string        `json:"id"`
	Name                        string        `json:"name"`
	Timezone                    string        `json:"timezone"`
	SlotMinutes                 int           `json:"slot_minutes"`
	ReservationDurationMinutes  int           `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int           `json:"cancellation_cutoff_minutes"`
	OpeningHours                []OpeningHour `json:"opening_hours"`
	Tables                      []Table       `json:"tables"`
}

// User is a registered diner.
type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash []byte `json:"password_hash"`
	DisplayName  string `json:"display_name"`
}

// Reservation is a booking, confirmed or cancelled.
type Reservation struct {
	ID            string    `json:"id"`
	Reference     string    `json:"reference"`
	RestaurantID  string    `json:"restaurant_id"`
	TableID       string    `json:"table_id"`
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

// IdempotencyRecord remembers the first outcome of a key use, scoped to one user/method/path.
type IdempotencyRecord struct {
	BodyHash     string `json:"body_hash"`
	Status       int    `json:"status"`
	ResponseBody string `json:"response_body"` // raw JSON text of the stored response
}
