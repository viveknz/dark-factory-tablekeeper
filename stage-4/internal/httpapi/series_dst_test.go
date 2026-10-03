package httpapi

import (
	"net/http"
	"testing"
)

const dstSeriesFixture = `{
	"users": [{"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}],
	"restaurants": [
		{
			"id": "r_dst", "name": "DST Place", "timezone": "Europe/Berlin",
			"slot_minutes": 30, "reservation_duration_minutes": 30, "cancellation_cutoff_minutes": 0,
			"opening_hours": [{"weekday": "sun", "opens": "00:00", "closes": "06:00"}],
			"tables": [{"id": "t_1", "label": "1", "capacity": 2}]
		}
	],
	"reservations": []
}`

// TestSeriesSpringForwardRejectsWholeAdoption: 2030-03-31 is Berlin's spring-forward Sunday
// (02:00 -> 03:00; 02:30 does not exist). An anchor one week earlier at the same clock time,
// adopted weekly, must have its occurrence landing on the skipped time reject the ENTIRE
// adoption as invalid_local_time, leaving nothing created.
func TestSeriesSpringForwardRejectsWholeAdoption(t *testing.T) {
	srv, _ := newTestServer(t)
	resetFixture(t, srv, dstSeriesFixture)
	ada := loginToken(t, srv, "ada@example.com", "correct horse")

	_, anchorOut := doJSON(t, "POST", srv.URL+"/reservations",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "anchor"},
		map[string]interface{}{"restaurant_id": "r_dst", "table_id": "t_1", "starts_at_local": "2030-03-24T02:30", "party_size": 2})
	if _, ok := anchorOut["reference"]; !ok {
		t.Fatalf("anchor creation failed: %v", anchorOut)
	}
	anchorRef := anchorOut["reference"].(string)

	resp, out := doJSON(t, "POST", srv.URL+"/series",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-dst"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 1})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("spring-forward occurrence status = %d %v, want 422", resp.StatusCode, out)
	}
	if out["error"].(map[string]interface{})["code"] != "invalid_local_time" {
		t.Errorf("error code = %v, want invalid_local_time", out["error"])
	}

	// Nothing was created: the anchor is still not part of any series.
	resp2, out2 := doJSON(t, "POST", srv.URL+"/series",
		map[string]string{"Authorization": "Bearer " + ada, "Idempotency-Key": "s-retry-with-just-anchor"},
		map[string]interface{}{"anchor_reference": anchorRef, "count": 2, "interval_weeks": 4})
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("anchor should still be adoptable after the failed attempt: %d %v", resp2.StatusCode, out2)
	}
}
