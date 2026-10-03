package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"tablekeeper/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	s := store.New()
	srv := httptest.NewServer(NewRouter(s, ""))
	t.Cleanup(srv.Close)
	return srv, s
}

func doJSON(t *testing.T, method, url string, headers map[string]string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()
	// Fatalf/FailNow is only safe from the test's own goroutine; some callers run this from
	// worker goroutines (concurrency tests), so failures here use Errorf and a zero-value
	// response instead of aborting the whole test.
	var buf bytes.Buffer
	if body != nil {
		if raw, ok := body.(json.RawMessage); ok {
			buf.Write(raw)
		} else if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Errorf("encode body: %v", err)
			return &http.Response{StatusCode: -1}, nil
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Errorf("new request: %v", err)
		return &http.Response{StatusCode: -1}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("do request: %v", err)
		return &http.Response{StatusCode: -1}, nil
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	dec := json.NewDecoder(resp.Body)
	_ = dec.Decode(&out) // best-effort; some responses (204) have no body
	return resp, out
}

const baseFixture = `{
  "users": [
    {"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"},
    {"id": "u_bob", "email": "bob@example.com", "password": "correct horse2", "display_name": "Bob"}
  ],
  "restaurants": [
    {
      "id": "r_anker",
      "name": "Zum Anker",
      "timezone": "Europe/Berlin",
      "slot_minutes": 30,
      "reservation_duration_minutes": 90,
      "cancellation_cutoff_minutes": 120,
      "opening_hours": [
        {"weekday": "thu", "opens": "18:00", "closes": "23:00"},
        {"weekday": "fri", "opens": "18:00", "closes": "23:30"}
      ],
      "tables": [
        {"id": "t_1", "label": "1", "capacity": 2},
        {"id": "t_2", "label": "2", "capacity": 4}
      ]
    }
  ],
  "reservations": []
}`

func resetFixture(t *testing.T, srv *httptest.Server, fixture string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/_test/reset", "application/json", bytes.NewReader([]byte(fixture)))
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset status = %d, want 204", resp.StatusCode)
	}
}

func loginToken(t *testing.T, srv *httptest.Server, email, password string) string {
	t.Helper()
	resp, out := doJSON(t, "POST", srv.URL+"/auth/login", nil, map[string]string{"email": email, "password": password})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d body=%v", resp.StatusCode, out)
	}
	return out["token"].(string)
}
