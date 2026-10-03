// Package httpapi wires the Tablekeeper HTTP routes to the in-memory store.
package httpapi

import (
	"net/http"

	"tablekeeper/internal/store"
)

// API holds the shared store used by every handler.
type API struct {
	Store *store.Store
}

// NewRouter builds the complete HTTP mux for the service, including static file serving for
// a later stage's screens (kept in the "static" subdirectory next to the binary).
func NewRouter(s *store.Store, staticDir string) http.Handler {
	a := &API{Store: s}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("POST /_test/reset", a.handleReset)
	mux.HandleFunc("GET /_test/export", a.handleExport)
	mux.HandleFunc("POST /_test/import", a.handleImport)

	mux.HandleFunc("POST /auth/signup", a.handleSignup)
	mux.HandleFunc("POST /auth/login", a.handleLogin)

	mux.HandleFunc("GET /restaurants", a.handleListRestaurants)
	mux.HandleFunc("GET /restaurants/{id}", a.handleGetRestaurant)
	mux.HandleFunc("GET /availability", a.handleAvailability)

	mux.HandleFunc("POST /reservations", a.handleCreateReservation)
	mux.HandleFunc("GET /reservations", a.handleListReservations)
	mux.HandleFunc("GET /reservations/{reference}", a.handleGetReservation)
	mux.HandleFunc("POST /reservations/{reference}/cancel", a.handleCancelReservation)
	mux.HandleFunc("PATCH /reservations/{reference}", a.handleAmendReservation)

	mux.HandleFunc("POST /reservation-moves", a.handleReservationMoves)

	if staticDir != "" {
		fs := http.FileServer(http.Dir(staticDir))
		mux.Handle("GET /static/", http.StripPrefix("/static/", fs))
	}

	return withRecover(mux)
}

// withRecover guarantees the "never 5xx" requirement holds even if a handler panics on an
// unexpected input shape: it converts a panic into 422 validation_failed instead of a crash.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeError(w, errValidationFailed("request could not be processed"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
