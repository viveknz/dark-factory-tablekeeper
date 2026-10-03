package httpapi

import (
	"io"
	"net/http"
	"testing"
)

func TestScreenRoutesReturnHTML(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/", "/signup", "/login", "/lookup"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200 (body=%q)", path, resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("GET %s Content-Type = %q, want text/html; charset=utf-8", path, ct)
		}
	}
}

func TestUnknownPathStillReturnsJSONNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, out := doJSON(t, "GET", srv.URL+"/reservationz", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /reservationz status = %d, want 404", resp.StatusCode)
	}
	errObj, ok := out["error"].(map[string]interface{})
	if !ok || errObj["code"] != "not_found" {
		t.Errorf("GET /reservationz body = %v, want JSON not_found envelope", out)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("GET /reservationz Content-Type = %q, want application/json; charset=utf-8", ct)
	}
}

func TestUnknownPathUnderReservationsStillJSON(t *testing.T) {
	srv, _ := newTestServer(t)
	// A deeper unmatched path under an otherwise-real API prefix must still 404 as JSON, not
	// fall through to the HTML screen handling.
	resp, out := doJSON(t, "GET", srv.URL+"/reservations/REF1/nope", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	errObj, ok := out["error"].(map[string]interface{})
	if !ok || errObj["code"] != "not_found" {
		t.Errorf("body = %v, want JSON not_found envelope", out)
	}
}

func TestApiEndpointsStillJSONAfterScreenRoutesAdded(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, out := doJSON(t, "GET", srv.URL+"/restaurants", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /restaurants status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("GET /restaurants Content-Type = %q, want application/json; charset=utf-8", ct)
	}
	if _, ok := out["restaurants"]; !ok {
		t.Errorf("GET /restaurants body = %v, want a restaurants key", out)
	}
}
