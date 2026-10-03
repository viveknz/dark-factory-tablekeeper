package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestExportImportRoundTripPreservesEverything(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)
	token := loginToken(t, srv, "ada@example.com", "correct horse")

	_, out := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "ei1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	ref := out["reference"].(string)

	exportResp, err := http.Get(srv.URL + "/_test/export")
	if err != nil || exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export failed: %v status=%d", err, exportResp.StatusCode)
	}
	var exported map[string]interface{}
	json.NewDecoder(exportResp.Body).Decode(&exported)
	exportResp.Body.Close()
	if exported["track"] != "tablekeeper" {
		t.Fatalf("export track = %v", exported["track"])
	}

	// Mutate state after export: cancel the booking and reset entirely.
	doJSON(t, "POST", srv.URL+"/reservations/"+ref+"/cancel", map[string]string{"Authorization": "Bearer " + token}, nil)
	resetFixture(t, srv, `{"users":[],"restaurants":[],"reservations":[]}`)

	// Confirm reset really cleared it.
	respGone, _ := doJSON(t, "GET", srv.URL+"/restaurants/r_anker", nil, nil)
	if respGone.StatusCode != http.StatusNotFound {
		t.Fatalf("restaurant should be gone after reset, got %d", respGone.StatusCode)
	}

	// Import the original export back.
	exportedBytes, _ := json.Marshal(exported)
	importResp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader(exportedBytes))
	if err != nil {
		t.Fatalf("import error: %v", err)
	}
	importResp.Body.Close()
	if importResp.StatusCode != http.StatusNoContent {
		t.Fatalf("import status = %d, want 204", importResp.StatusCode)
	}

	// The original token must still work, the restaurant must be back, and the original
	// reservation must still be confirmed (import restores the pre-cancel, pre-reset snapshot).
	resp, outR := doJSON(t, "GET", srv.URL+"/reservations/"+ref, map[string]string{"Authorization": "Bearer " + token}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get reservation after import = %d %v", resp.StatusCode, outR)
	}
	if outR["status"] != "confirmed" {
		t.Errorf("status after import = %v, want confirmed (import restores snapshot, not current state)", outR["status"])
	}

	// The idempotency key used for the original booking must still replay correctly after import.
	respReplay, outReplay := doJSON(t, "POST", srv.URL+"/reservations", map[string]string{"Authorization": "Bearer " + token, "Idempotency-Key": "ei1"},
		map[string]interface{}{"restaurant_id": "r_anker", "table_id": "t_1", "starts_at_local": "2027-09-23T19:00", "party_size": 2})
	if respReplay.StatusCode != http.StatusOK || outReplay["reference"] != ref {
		t.Errorf("idempotency replay after import = %d %v", respReplay.StatusCode, outReplay)
	}
}

func TestImportRejectsInvalidPayloadWithoutChangingState(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, baseFixture)

	resp, err := http.Post(srv.URL+"/_test/import", "application/json", bytes.NewReader([]byte(`{"track":"wrong","format_version":1,"state":{}}`)))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad track import = %d, want 422", resp.StatusCode)
	}

	// Original restaurant must still be present.
	respCheck, _ := doJSON(t, "GET", srv.URL+"/restaurants/r_anker", nil, nil)
	if respCheck.StatusCode != http.StatusOK {
		t.Fatalf("state changed after rejected import: %d", respCheck.StatusCode)
	}
}
