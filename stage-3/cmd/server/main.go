// Command server starts the Tablekeeper reservations API.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"tablekeeper/internal/httpapi"
	"tablekeeper/internal/store"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	s := store.New()
	staticDir := "static"
	if _, err := os.Stat(staticDir); err != nil {
		staticDir = ""
	}
	handler := httpapi.NewRouter(s, staticDir)

	if os.Getenv("DEMO_SEED") == "1" {
		seedDemoOnStartup(s)
	}

	srv := &http.Server{
		Addr:         "0.0.0.0:" + port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("tablekeeper listening on 0.0.0.0:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// seedDemoOnStartup loads demo/fixture.json (copied into the image next to the binary, see
// Dockerfile) plus a handful of confirmed reservations on dates computed relative to today, the
// same demo data demo/load_demo.py posts to /_test/reset from outside the container. It never
// changes what /_test/reset itself does: a later reset still fully replaces this seeded state.
func seedDemoOnStartup(s *store.Store) {
	raw, err := os.ReadFile("demo/fixture.json")
	if err != nil {
		log.Printf("DEMO_SEED=1 but demo/fixture.json could not be read: %v", err)
		return
	}
	var fixture store.Fixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		log.Printf("DEMO_SEED=1 but demo/fixture.json is not valid JSON: %v", err)
		return
	}
	fixture.Reservations = demoReservations()
	if err := httpapi.SeedDemoFixture(s, fixture); err != nil {
		log.Printf("DEMO_SEED=1 seeding failed: %v", err)
		return
	}
	log.Printf("DEMO_SEED=1: loaded demo/fixture.json plus %d demo reservations", len(fixture.Reservations))
}

// demoReservations returns a handful of confirmed future reservations across both demo
// restaurants, computed relative to today so the demo never goes stale. Both demo restaurants
// open every day of the week, so any future date is open; only the table/time/party-size
// combinations need to respect each restaurant's capacity and hours. Mirrors the reservations
// demo/load_demo.py posts from outside the container.
func demoReservations() []store.FixtureReservation {
	today := time.Now().UTC()
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }
	return []store.FixtureReservation{
		{
			ID: "res_demo_1", UserID: "u_demo_diner", RestaurantID: "r_harbour_table",
			TableID: "t_window_1", PartySize: 2, Status: "confirmed",
			StartsAtLocal: day(1) + "T18:00",
		},
		{
			ID: "res_demo_2", UserID: "u_demo_manager", RestaurantID: "r_harbour_table",
			TableIDs: []string{"t_window_1", "t_window_2"}, PartySize: 4, Status: "confirmed",
			StartsAtLocal: day(3) + "T19:00",
		},
		{
			ID: "res_demo_3", UserID: "u_demo_diner", RestaurantID: "r_harbour_table",
			TableID: "t_long_table", PartySize: 5, Status: "confirmed",
			StartsAtLocal: day(7) + "T18:30",
		},
		{
			ID: "res_demo_4", UserID: "u_demo_manager", RestaurantID: "r_lantern_noodle_bar",
			TableID: "t_counter_1", PartySize: 2, Status: "confirmed",
			StartsAtLocal: day(1) + "T18:00",
		},
		{
			ID: "res_demo_5", UserID: "u_demo_diner", RestaurantID: "r_lantern_noodle_bar",
			TableID: "t_family", PartySize: 6, Status: "confirmed",
			StartsAtLocal: day(5) + "T18:30",
		},
	}
}
