package timeutil

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func TestSkippedSpringForward(t *testing.T) {
	cases := []struct {
		zone          string
		year          int
		month         time.Month
		day, hour, mn int
	}{
		{"Europe/Berlin", 2026, 3, 29, 2, 30},
		{"Europe/Berlin", 2027, 3, 28, 2, 30},
		{"America/New_York", 2026, 3, 8, 2, 30},
		{"America/New_York", 2027, 3, 14, 2, 30},
		{"Australia/Melbourne", 2026, 10, 4, 2, 30},
	}
	for _, c := range cases {
		loc := mustLoc(t, c.zone)
		_, err := ResolveLocal(loc, c.year, c.month, c.day, c.hour, c.mn)
		if err != ErrSkipped {
			t.Errorf("%s %04d-%02d-%02d %02d:%02d: expected ErrSkipped, got %v", c.zone, c.year, c.month, c.day, c.hour, c.mn, err)
		}
	}
}

func TestAmbiguousFallBackResolvesToFirstOccurrence(t *testing.T) {
	cases := []struct {
		zone           string
		year           int
		month          time.Month
		day, hour, mn  int
		wantOffsetSecs int // offset of the FIRST (pre-transition) occurrence
	}{
		{"Europe/Berlin", 2026, 10, 25, 2, 30, 2 * 3600},
		{"Europe/Berlin", 2027, 10, 31, 2, 30, 2 * 3600},
		{"America/New_York", 2026, 11, 1, 1, 30, -4 * 3600},
		{"America/New_York", 2027, 11, 7, 1, 30, -4 * 3600},
		{"Australia/Melbourne", 2027, 4, 4, 2, 30, 11 * 3600},
	}
	for _, c := range cases {
		loc := mustLoc(t, c.zone)
		got, err := ResolveLocal(loc, c.year, c.month, c.day, c.hour, c.mn)
		if err != nil {
			t.Fatalf("%s %04d-%02d-%02d %02d:%02d: unexpected error %v", c.zone, c.year, c.month, c.day, c.hour, c.mn, err)
		}
		_, off := got.In(loc).Zone()
		if off != c.wantOffsetSecs {
			t.Errorf("%s %04d-%02d-%02d %02d:%02d: offset = %d, want %d (first/pre-transition occurrence)", c.zone, c.year, c.month, c.day, c.hour, c.mn, off, c.wantOffsetSecs)
		}
	}
}

// Sanity: the day before/of/after each transition, and ordinary times, resolve normally
// (not skipped, not shifted) across 2026 and 2027 for the three named zones.
func TestOrdinaryDaysAroundTransitionsUnaffected(t *testing.T) {
	zones := []string{"Europe/Berlin", "America/New_York", "Australia/Melbourne"}
	days := []struct{ y int; m time.Month; d int }{
		{2026, 3, 7}, {2026, 3, 8}, {2026, 3, 9},
		{2026, 3, 28}, {2026, 3, 29}, {2026, 3, 30},
		{2026, 10, 3}, {2026, 10, 4}, {2026, 10, 5},
		{2026, 10, 24}, {2026, 10, 25}, {2026, 10, 26},
		{2026, 10, 31}, {2026, 11, 1}, {2026, 11, 2},
		{2027, 3, 13}, {2027, 3, 14}, {2027, 3, 15},
		{2027, 3, 27}, {2027, 3, 28}, {2027, 3, 29},
		{2027, 4, 3}, {2027, 4, 4}, {2027, 4, 5},
		{2027, 10, 30}, {2027, 10, 31}, {2027, 11, 1},
		{2027, 11, 6}, {2027, 11, 7}, {2027, 11, 8},
	}
	for _, zone := range zones {
		loc := mustLoc(t, zone)
		for _, day := range days {
			// Noon local time never lands in a gap or repeat.
			got, err := ResolveLocal(loc, day.y, day.m, day.d, 12, 0)
			if err != nil {
				t.Errorf("%s %04d-%02d-%02d 12:00: unexpected error %v", zone, day.y, day.m, day.d, err)
				continue
			}
			if got.In(loc).Format("2006-01-02T15:04") != got.In(loc).Format("2006-01-02T15:04") {
				t.Fatal("unreachable")
			}
			wantStr := got.In(loc).Format("2006-01-02T15:04")
			expect := timeStr(day.y, day.m, day.d, 12, 0)
			if wantStr != expect {
				t.Errorf("%s %v: roundtrip mismatch got %s want %s", zone, day, wantStr, expect)
			}
		}
	}
}

func timeStr(y int, m time.Month, d, hh, mm int) string {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC).Format("2006-01-02T15:04")
}
