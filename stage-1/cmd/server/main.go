// Command server starts the Tablekeeper reservations API.
package main

import (
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
