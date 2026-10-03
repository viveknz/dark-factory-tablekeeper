package httpapi

import (
	"net/http"
	"testing"
)

func TestHealthAlwaysPublic(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, out := doJSON(t, "GET", srv.URL+"/health", nil, nil)
	if resp.StatusCode != http.StatusOK || out["status"] != "ok" {
		t.Fatalf("health = %d %v", resp.StatusCode, out)
	}
}

func TestSignupAndLogin(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, out := doJSON(t, "POST", srv.URL+"/auth/signup", nil, map[string]string{
		"email": "new@example.com", "password": "correct horse", "display_name": "New",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup = %d %v", resp.StatusCode, out)
	}
	if out["token"] == "" || out["user_id"] == "" {
		t.Fatalf("signup response missing fields: %v", out)
	}

	resp2, _ := doJSON(t, "POST", srv.URL+"/auth/signup", nil, map[string]string{
		"email": "new@example.com", "password": "another password",
	})
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate signup = %d, want 409", resp2.StatusCode)
	}

	resp3, out3 := doJSON(t, "POST", srv.URL+"/auth/login", nil, map[string]string{
		"email": "new@example.com", "password": "correct horse",
	})
	if resp3.StatusCode != http.StatusOK || out3["token"] == "" {
		t.Fatalf("login = %d %v", resp3.StatusCode, out3)
	}

	resp4, _ := doJSON(t, "POST", srv.URL+"/auth/login", nil, map[string]string{
		"email": "new@example.com", "password": "wrong password",
	})
	if resp4.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", resp4.StatusCode)
	}

	resp5, _ := doJSON(t, "POST", srv.URL+"/auth/signup", nil, map[string]string{
		"email": "bademail", "password": "correct horse",
	})
	if resp5.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad email = %d, want 422", resp5.StatusCode)
	}

	resp6, _ := doJSON(t, "POST", srv.URL+"/auth/signup", nil, map[string]string{
		"email": "short@example.com", "password": "short",
	})
	if resp6.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("short password = %d, want 422", resp6.StatusCode)
	}
}

func TestPublicEndpointsNoAuthRequired(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)

	resp, _ := doJSON(t, "GET", srv.URL+"/restaurants", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /restaurants = %d", resp.StatusCode)
	}
	resp2, _ := doJSON(t, "GET", srv.URL+"/restaurants/r_anker", nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /restaurants/{id} = %d", resp2.StatusCode)
	}
	resp3, _ := doJSON(t, "GET", srv.URL+"/restaurants/unknown", nil, nil)
	if resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /restaurants/unknown = %d, want 404", resp3.StatusCode)
	}
	resp4, _ := doJSON(t, "GET", srv.URL+"/availability?restaurant_id=r_anker&date=2027-09-23&party_size=2", nil, nil)
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("GET /availability = %d", resp4.StatusCode)
	}
}

func TestProtectedEndpointRequiresBearer(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	resp, _ := doJSON(t, "GET", srv.URL+"/reservations", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
	resp2, _ := doJSON(t, "GET", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer garbage"}, nil)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token = %d, want 401", resp2.StatusCode)
	}
}
