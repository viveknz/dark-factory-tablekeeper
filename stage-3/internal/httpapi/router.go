// Package httpapi wires the Tablekeeper HTTP routes to the in-memory store.
package httpapi

import (
	"net/http"
	"os"
	"path/filepath"

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
	mux.HandleFunc("POST /restaurants/{id}/policies", a.handlePublishPolicy)
	mux.HandleFunc("GET /restaurants/{id}/policies", a.handleListPolicies)

	mux.HandleFunc("POST /reservations", a.handleCreateReservation)
	mux.HandleFunc("GET /reservations", a.handleListReservations)
	mux.HandleFunc("GET /reservations/{reference}", a.handleGetReservation)
	mux.HandleFunc("POST /reservations/{reference}/cancel", a.handleCancelReservation)
	mux.HandleFunc("PATCH /reservations/{reference}", a.handleAmendReservation)
	mux.HandleFunc("GET /reservations/{reference}/history", a.handleReservationHistory)
	mux.HandleFunc("GET /reservations/{reference}/decision", a.handleReservationDecision)

	mux.HandleFunc("POST /reservation-moves", a.handleReservationMoves)

	mux.HandleFunc("POST /series", a.handleCreateSeries)
	mux.HandleFunc("GET /series/{series_id}", a.handleGetSeries)

	if staticDir != "" {
		fs := http.FileServer(http.Dir(staticDir))
		mux.Handle("GET /static/", http.StripPrefix("/static/", fs))
	}

	// The four screen routes required by spec §"Routes" are exact-match patterns (the root
	// uses "{$}" so it does not become Go 1.22+ ServeMux's subtree catch-all for "/", which
	// would otherwise swallow every unmatched GET as an HTML 200). Anything else unmatched
	// falls through to the catch-all below, which keeps returning the JSON error envelope
	// §3.4 requires for the API, not an HTML or plain-text 404.
	screen := handleScreen(staticDir)
	mux.HandleFunc("GET /{$}", screen)
	mux.HandleFunc("GET /signup", screen)
	mux.HandleFunc("GET /login", screen)
	mux.HandleFunc("GET /lookup", screen)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, errNotFound)
	})

	return withRecover(mux)
}

// placeholderScreenHTML is served when the frontend has not yet committed static/index.html
// (or no staticDir is configured, as in this package's own tests), so the four screen routes
// are reachable from the start rather than only once the frontend's build lands.
const placeholderScreenHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Tablekeeper</title></head>
<body><p>Tablekeeper is starting up.</p></body>
</html>
`

// handleScreen serves the single shared document for every screen route ("/", "/signup",
// "/login", "/lookup"); the document reads window.location.pathname client-side to render the
// right screen. Falls back to a minimal placeholder if the frontend's static/index.html isn't
// present yet.
func handleScreen(staticDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if staticDir != "" {
			indexPath := filepath.Join(staticDir, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(placeholderScreenHTML))
	}
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
