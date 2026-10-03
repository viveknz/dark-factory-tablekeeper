// Package timeutil resolves restaurant-local wall-clock strings ("YYYY-MM-DDTHH:MM") to
// absolute instants, handling daylight-saving transitions per spec §9:
//   - a local time in a spring-forward gap does not exist (ErrSkipped)
//   - a local time in a fall-back repeat exists twice; the first (pre-transition) occurrence
//     is always the one that applies.
package timeutil

import (
	"fmt"
	"sort"
	"time"

	_ "time/tzdata" // embed the IANA database so the image needs no system tzdata at runtime
)

// ErrSkipped indicates the requested local wall-clock time does not exist in the zone
// (it falls in a spring-forward gap).
var ErrSkipped = fmt.Errorf("local time does not exist")

// ResolveLocal resolves the local wall-clock time y-m-d hh:mi (seconds always 0) in loc to an
// absolute instant. If the time is ambiguous (a fall-back repeat), it returns the earlier
// (pre-transition) occurrence. If the time does not exist, it returns ErrSkipped.
func ResolveLocal(loc *time.Location, year int, month time.Month, day, hour, minute int) (time.Time, error) {
	wantStr := fmt.Sprintf("%04d-%02d-%02dT%02d:%02d", year, int(month), day, hour, minute)

	guess := time.Date(year, month, day, hour, minute, 0, 0, loc)
	if guess.Format("2006-01-02T15:04") != wantStr {
		return time.Time{}, ErrSkipped
	}

	_, offGuess := guess.Zone()
	candidates := map[int]bool{offGuess: true}
	for _, probe := range []time.Time{guess.Add(-3 * time.Hour), guess.Add(3 * time.Hour)} {
		_, off := probe.Zone()
		candidates[off] = true
	}

	naiveUTC := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	var valid []time.Time
	for off := range candidates {
		inst := naiveUTC.Add(-time.Duration(off) * time.Second)
		back := inst.In(loc)
		_, backOff := back.Zone()
		if backOff == off && back.Format("2006-01-02T15:04") == wantStr {
			valid = append(valid, inst)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Before(valid[j]) })
	if len(valid) == 0 {
		// Should not happen given the guess round-tripped, but fall back to the guess.
		return guess, nil
	}
	// Re-attach loc (candidates were built from time.UTC-based arithmetic) so later
	// formatting of this instant renders the zone's local offset, not "Z"/UTC.
	return valid[0].In(loc), nil
}
