package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestImportAcceptsRealStage1Export runs the ACTUAL accepted stage-1 service as a subprocess
// (not a hand-built "stage-1-shaped" fixture, since the export state is spec'd as opaque),
// creates a real booking with an Idempotency-Key on it, exports its real state, imports that
// real export into this package's stage-2 test server, and replays the original key+body as a
// client that never saw the first response. It then checks the replay's shape against a live
// GET on the same reservation, closing an independent-review finding: a migrated booking's
// frozen idempotency response must carry table_ids just like a live read does.
//
// Skipped (not failed) if a sibling stage-1/ source tree isn't available, e.g. a checkout of
// only stage-2/, or if the real stage-1 subprocess can't start in the time budget.
func TestImportAcceptsRealStage1Export(t *testing.T) {
	stage1Dir := findStage1Dir()
	if stage1Dir == "" {
		t.Skip("no sibling stage-1/ source tree found next to stage-2/; skipping real cross-version migration test")
	}

	port := freePort(t)
	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)

	// Build the real stage-1 binary first and run THAT directly, rather than "go run": "go
	// run" spawns the compiled binary as a child of its own wrapper process, and killing the
	// wrapper (what context cancellation does) does not kill that child, which then keeps the
	// wrapper's stdout/stderr pipes open forever and hangs Cmd.Wait() in cleanup. Running the
	// built binary directly means cancellation kills the actual server process.
	binPath := filepath.Join(t.TempDir(), "stage1-server")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/server")
	buildCmd.Dir = stage1Dir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Skipf("could not build the real stage-1 service (%v): %s", err, out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(port))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Skipf("could not start the real stage-1 service (%v); skipping", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	if !waitForHealth(baseURL, 30*time.Second) {
		t.Skipf("the real stage-1 service never became healthy within 30s (stderr: %s); skipping", stderr.String())
	}

	// Seed the REAL stage-1 service with a user/restaurant/table.
	fixtureBody := `{
		"users": [{"id":"u_real1","email":"real1@example.com","password":"correct horse","display_name":"Real One"}],
		"restaurants": [{
			"id":"r_real1","name":"Real One","timezone":"Europe/Berlin","slot_minutes":30,
			"reservation_duration_minutes":90,"cancellation_cutoff_minutes":120,
			"opening_hours":[{"weekday":"mon","opens":"18:00","closes":"23:00"}],
			"tables":[{"id":"t_real1","label":"1","capacity":2}]
		}],
		"reservations": []
	}`
	resetResp, err := http.Post(baseURL+"/_test/reset", "application/json", bytes.NewReader([]byte(fixtureBody)))
	if err != nil || statusOf(resetResp) != http.StatusNoContent {
		t.Fatalf("real stage-1 reset failed: err=%v status=%v", err, statusOf(resetResp))
	}

	token := realStage1Login(t, baseURL, "real1@example.com", "correct horse")

	// A real booking with an Idempotency-Key -- this is the request whose response we then
	// pretend the client never saw.
	bookingBody := []byte(`{"restaurant_id":"r_real1","table_id":"t_real1","starts_at_local":"2027-01-04T19:00","party_size":2}`)
	bookReq, _ := http.NewRequest("POST", baseURL+"/reservations", bytes.NewReader(bookingBody))
	bookReq.Header.Set("Content-Type", "application/json")
	bookReq.Header.Set("Authorization", "Bearer "+token)
	bookReq.Header.Set("Idempotency-Key", "real-lost-key")
	bookResp, err := http.DefaultClient.Do(bookReq)
	if err != nil || statusOf(bookResp) != http.StatusCreated {
		t.Fatalf("real stage-1 booking failed: err=%v status=%v", err, statusOf(bookResp))
	}
	bookResp.Body.Close() // the client "never saw" this response; not inspected further.

	// Export the REAL stage-1 state: an actual opaque envelope, not a guess at its shape.
	exportResp, err := http.Get(baseURL + "/_test/export")
	if err != nil || statusOf(exportResp) != http.StatusOK {
		t.Fatalf("real stage-1 export failed: err=%v status=%v", err, statusOf(exportResp))
	}
	exportBytes, _ := io.ReadAll(exportResp.Body)
	exportResp.Body.Close()

	// Import that real stage-1 export into THIS package's stage-2 test server.
	srv, _ := newTestServer(t)
	importResp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader(exportBytes))
	if err != nil {
		t.Fatalf("stage-2 import failed: %v", err)
	}
	importResp.Body.Close()
	if importResp.StatusCode != http.StatusNoContent {
		t.Fatalf("stage-2 import status = %d, want 204 (export body was: %s)", importResp.StatusCode, exportBytes)
	}

	// The lost-response retry on stage-2: identical key+body, as a client that never saw the
	// real stage-1 answer.
	respRetry, outRetry := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "real-lost-key"},
		map[string]interface{}{"restaurant_id": "r_real1", "table_id": "t_real1", "starts_at_local": "2027-01-04T19:00", "party_size": 2})
	if respRetry.StatusCode != http.StatusOK {
		t.Fatalf("lost-response retry after a REAL stage-1 import = %d %v, want 200", respRetry.StatusCode, outRetry)
	}
	ids, ok := outRetry["table_ids"].([]interface{})
	if !ok || len(ids) != 1 || ids[0] != "t_real1" {
		t.Errorf("replay of a REAL stage-1 idempotency record: table_ids = %v, want [t_real1]", outRetry["table_ids"])
	}
	if outRetry["table_id"] != "t_real1" {
		t.Errorf("replay table_id = %v, want t_real1", outRetry["table_id"])
	}

	// A live GET on the same reservation must show the identical, current-shape response.
	ref, _ := outRetry["reference"].(string)
	if ref == "" {
		t.Fatalf("replay response had no reference: %v", outRetry)
	}
	_, outGet := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + token}, nil)
	if idsGet, ok := outGet["table_ids"].([]interface{}); !ok || len(idsGet) != 1 || idsGet[0] != "t_real1" {
		t.Errorf("live GET table_ids = %v, want [t_real1]", outGet["table_ids"])
	}
}

// findStage1Dir looks for a sibling stage-1/ source tree next to this stage-2/ checkout
// (this package lives at <repo>/stage-2/internal/httpapi). Returns "" if not found.
func findStage1Dir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(wd, "..", "..", "..", "stage-1")
	if info, err := os.Stat(filepath.Join(candidate, "cmd", "server", "main.go")); err == nil && !info.IsDir() {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return ""
		}
		return abs
	}
	return ""
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not reserve a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func waitForHealth(baseURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func realStage1Login(t *testing.T, baseURL, email, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(baseURL+"/auth/login", "application/json", bytes.NewReader(body))
	if err != nil || statusOf(resp) != http.StatusOK {
		t.Fatalf("real stage-1 login failed: err=%v status=%v", err, statusOf(resp))
	}
	defer resp.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode real stage-1 login response: %v", err)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatalf("real stage-1 login response had no token: %v", out)
	}
	return token
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return -1
	}
	return resp.StatusCode
}
