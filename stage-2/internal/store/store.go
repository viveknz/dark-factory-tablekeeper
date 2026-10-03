package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Store holds all service state behind a single mutex. Every state-changing handler, and
// every handler whose correctness depends on a consistent snapshot, takes the lock for its
// whole critical section. This also gives idempotency and booking-overlap checks atomicity
// under concurrent requests for free.
type Store struct {
	mu sync.Mutex

	Users        map[string]*User // by id
	usersByEmail map[string]*User
	Tokens       map[string]string // token -> user id

	Restaurants     map[string]*Restaurant
	restaurantOrder []string

	Reservations           map[string]*Reservation // by id
	reservationByReference map[string]*Reservation

	Idempotency map[string]*IdempotencyRecord // see idempotencyKey()
}

// HashPassword hashes a password with bcrypt. Passwords are truncated to bcrypt's 72-byte
// limit before hashing so no valid request can ever hit bcrypt's ErrPasswordTooLong (the
// service must never return 5xx).
func HashPassword(password string) ([]byte, error) {
	b := []byte(password)
	if len(b) > 72 {
		b = b[:72]
	}
	return bcrypt.GenerateFromPassword(b, bcrypt.DefaultCost)
}

// CheckPassword reports whether password matches hash, applying the same 72-byte truncation
// used by HashPassword.
func CheckPassword(hash []byte, password string) bool {
	b := []byte(password)
	if len(b) > 72 {
		b = b[:72]
	}
	return bcrypt.CompareHashAndPassword(hash, b) == nil
}

func New() *Store {
	s := &Store{}
	s.resetLocked(Fixture{})
	return s
}

// Lock/Unlock expose the single store mutex to handlers that need an atomic critical section
// spanning multiple store operations (e.g. check-then-write).
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

// Fixture is the shape accepted by POST /_test/reset, matching spec §4.
type Fixture struct {
	Users       []FixtureUser       `json:"users"`
	Restaurants []FixtureRestaurant `json:"restaurants"`
	Reservations []FixtureReservation `json:"reservations"`
}

type FixtureUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type FixtureRestaurant struct {
	ID                         string        `json:"id"`
	Name                       string        `json:"name"`
	Timezone                   string        `json:"timezone"`
	SlotMinutes                int           `json:"slot_minutes"`
	ReservationDurationMinutes int           `json:"reservation_duration_minutes"`
	CancellationCutoffMinutes  int           `json:"cancellation_cutoff_minutes"`
	OpeningHours               []OpeningHour `json:"opening_hours"`
	Tables                     []Table       `json:"tables"`
	Combinable                 [][]string    `json:"combinable"`
}

// FixtureReservation may specify either a single table_id or a set of table_ids, and an
// optional status ("confirmed" is the default; "cancelled" is accepted).
type FixtureReservation struct {
	ID            string   `json:"id"`
	Reference     string   `json:"reference"`
	UserID        string   `json:"user_id"`
	RestaurantID  string   `json:"restaurant_id"`
	TableID       string   `json:"table_id"`
	TableIDs      []string `json:"table_ids"`
	PartySize     int      `json:"party_size"`
	StartsAtLocal string   `json:"starts_at_local"`
	Status        string   `json:"status"`
}

// ResolvedTableIDs returns table_ids if given, else a single-element set from table_id.
func (fr FixtureReservation) ResolvedTableIDs() []string {
	if len(fr.TableIDs) > 0 {
		return fr.TableIDs
	}
	if fr.TableID != "" {
		return []string{fr.TableID}
	}
	return nil
}

// ResetAndSeed replaces all state with the given fixture, then uses buildReservation to turn
// each seeded reservation entry into a store Reservation (resolving its local time against
// the restaurant's timezone is the caller's job, since that logic lives in timeutil). The
// whole operation is atomic under the store lock.
func (s *Store) ResetAndSeed(f Fixture, buildReservation func(FixtureReservation) (*Reservation, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.resetLocked(f); err != nil {
		return err
	}
	for _, fr := range f.Reservations {
		r, err := buildReservation(fr)
		if err != nil {
			return err
		}
		s.Reservations[r.ID] = r
		s.reservationByReference[r.Reference] = r
	}
	return nil
}

func (s *Store) resetLocked(f Fixture) error {
	s.Users = map[string]*User{}
	s.usersByEmail = map[string]*User{}
	s.Tokens = map[string]string{}
	s.Restaurants = map[string]*Restaurant{}
	s.restaurantOrder = nil
	s.Reservations = map[string]*Reservation{}
	s.reservationByReference = map[string]*Reservation{}
	s.Idempotency = map[string]*IdempotencyRecord{}

	for _, fu := range f.Users {
		hash, err := HashPassword(fu.Password)
		if err != nil {
			return fmt.Errorf("hash password for %s: %w", fu.ID, err)
		}
		u := &User{ID: fu.ID, Email: fu.Email, PasswordHash: hash, DisplayName: fu.DisplayName}
		s.Users[u.ID] = u
		s.usersByEmail[u.Email] = u
	}

	for _, fr := range f.Restaurants {
		r := &Restaurant{
			ID:                         fr.ID,
			Name:                       fr.Name,
			Timezone:                   fr.Timezone,
			SlotMinutes:                fr.SlotMinutes,
			ReservationDurationMinutes: fr.ReservationDurationMinutes,
			CancellationCutoffMinutes:  fr.CancellationCutoffMinutes,
			OpeningHours:               fr.OpeningHours,
			Tables:                     fr.Tables,
			Combinable:                 fr.Combinable,
		}
		s.Restaurants[r.ID] = r
		s.restaurantOrder = append(s.restaurantOrder, r.ID)
	}

	return nil
}

func (s *Store) UserByEmail(email string) (*User, bool) {
	u, ok := s.usersByEmail[email]
	return u, ok
}

func (s *Store) UserByToken(token string) (*User, bool) {
	uid, ok := s.Tokens[token]
	if !ok {
		return nil, false
	}
	u, ok := s.Users[uid]
	return u, ok
}

func (s *Store) AddUser(u *User) {
	s.Users[u.ID] = u
	s.usersByEmail[u.Email] = u
}

func (s *Store) AddToken(token, userID string) {
	s.Tokens[token] = userID
}

func (s *Store) RestaurantsInOrder() []*Restaurant {
	out := make([]*Restaurant, 0, len(s.restaurantOrder))
	for _, id := range s.restaurantOrder {
		out = append(out, s.Restaurants[id])
	}
	return out
}

func (s *Store) AddRestaurant(r *Restaurant) {
	if _, exists := s.Restaurants[r.ID]; !exists {
		s.restaurantOrder = append(s.restaurantOrder, r.ID)
	}
	s.Restaurants[r.ID] = r
}

func (s *Store) AddReservation(r *Reservation) {
	s.Reservations[r.ID] = r
	s.reservationByReference[r.Reference] = r
}

func (s *Store) ReservationByReference(ref string) (*Reservation, bool) {
	r, ok := s.reservationByReference[ref]
	return r, ok
}

func (s *Store) ReferenceTaken(ref string) bool {
	_, ok := s.reservationByReference[ref]
	return ok
}

func (s *Store) ReservationsByUser(userID string) []*Reservation {
	out := []*Reservation{}
	for _, r := range s.Reservations {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out
}

// TableAvailable reports whether tableID has no overlapping confirmed reservation in
// [start,end) (checking every reservation that occupies tableID among its one or two
// tables), ignoring the reservation given in ignoreID if any (used by amendments, which
// recheck availability for their own booking).
func (s *Store) TableAvailable(restaurantID, tableID string, start, end time.Time, ignoreID string) bool {
	for _, r := range s.Reservations {
		if r.ID == ignoreID {
			continue
		}
		if r.Status != "confirmed" {
			continue
		}
		if r.RestaurantID != restaurantID || !r.HasTable(tableID) {
			continue
		}
		if r.Overlaps(start, end) {
			return false
		}
	}
	return true
}

func idempotencyKey(userID, method, path, key string) string {
	return userID + "\x00" + method + "\x00" + path + "\x00" + key
}

func (s *Store) IdempotencyGet(userID, method, path, key string) (*IdempotencyRecord, bool) {
	rec, ok := s.Idempotency[idempotencyKey(userID, method, path, key)]
	return rec, ok
}

func (s *Store) IdempotencyPut(userID, method, path, key string, rec *IdempotencyRecord) {
	s.Idempotency[idempotencyKey(userID, method, path, key)] = rec
}

// ExportState is the opaque snapshot shape returned by export and accepted by import.
type ExportState struct {
	Users        map[string]*User              `json:"users"`
	Tokens       map[string]string              `json:"tokens"`
	Restaurants  map[string]*Restaurant         `json:"restaurants"`
	RestOrder    []string                       `json:"restaurant_order"`
	Reservations map[string]*Reservation        `json:"reservations"`
	Idempotency  map[string]*IdempotencyRecord `json:"idempotency"`
}

func (s *Store) ExportLocked() ExportState {
	return ExportState{
		Users:        s.Users,
		Tokens:       s.Tokens,
		Restaurants:  s.Restaurants,
		RestOrder:    s.restaurantOrder,
		Reservations: s.Reservations,
		Idempotency:  s.Idempotency,
	}
}

func (s *Store) ImportLocked(st ExportState) {
	s.Users = st.Users
	if s.Users == nil {
		s.Users = map[string]*User{}
	}
	s.usersByEmail = map[string]*User{}
	for _, u := range s.Users {
		s.usersByEmail[u.Email] = u
	}
	s.Tokens = st.Tokens
	if s.Tokens == nil {
		s.Tokens = map[string]string{}
	}
	s.Restaurants = st.Restaurants
	if s.Restaurants == nil {
		s.Restaurants = map[string]*Restaurant{}
	}
	s.restaurantOrder = st.RestOrder
	s.Reservations = st.Reservations
	if s.Reservations == nil {
		s.Reservations = map[string]*Reservation{}
	}
	s.reservationByReference = map[string]*Reservation{}
	for _, r := range s.Reservations {
		s.reservationByReference[r.Reference] = r
	}
	s.Idempotency = st.Idempotency
	if s.Idempotency == nil {
		s.Idempotency = map[string]*IdempotencyRecord{}
	}
}

// MarshalExport/UnmarshalExport let httpapi treat the state as an opaque JSON value.
func MarshalExport(st ExportState) (json.RawMessage, error) {
	return json.Marshal(st)
}

func UnmarshalExport(raw json.RawMessage) (ExportState, error) {
	var st ExportState
	err := json.Unmarshal(raw, &st)
	return st, err
}
