package httpapi

import (
	"strconv"
	"strings"
	"time"

	"tablekeeper/internal/store"
	"tablekeeper/internal/timeutil"
)

var weekdayCodes = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func weekdayCode(w time.Weekday) string { return weekdayCodes[int(w)] }

// parseHHMM parses "HH:MM" into hour, minute. Returns ok=false if malformed.
func parseHHMM(s string) (hour, minute int, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// openingHourFor returns the opening hour entry for the given weekday code out of hours, if
// any (a day with no entry is closed). hours is the applicable policy's opening hours, not
// necessarily the restaurant's raw fixture fields.
func openingHourFor(hours []store.OpeningHour, weekday string) (store.OpeningHour, bool) {
	for _, oh := range hours {
		if oh.Weekday == weekday {
			return oh, true
		}
	}
	return store.OpeningHour{}, false
}

// parseDate parses a "YYYY-MM-DD" date string into y,m,d, rejecting calendar-impossible
// dates (e.g. 2026-02-30) in addition to malformed ones.
func parseDate(s string) (year, month, day int, ok bool) {
	if !dateRe.MatchString(s) {
		return 0, 0, 0, false
	}
	parts := strings.SplitN(s, "-", 3)
	y, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	d, e3 := strconv.Atoi(parts[2])
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, 0, 0, false
	}
	if !isValidCalendarDate(y, m, d) {
		return 0, 0, 0, false
	}
	return y, m, d, true
}

// isValidCalendarDate reports whether y-m-d is a real calendar date (rejects e.g. day 30 in
// February) by round-tripping through time.Date, which normalizes out-of-range components.
func isValidCalendarDate(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t.Year() == y && int(t.Month()) == m && t.Day() == d
}

// isValidCalendarDateString reports whether s (already matching the YYYY-MM-DD shape) names a
// real calendar date.
func isValidCalendarDateString(s string) bool {
	_, _, _, ok := parseDate(s)
	return ok
}

// splitLocal splits a "YYYY-MM-DDTHH:MM" string into its date and time components.
func splitLocal(s string) (year, month, day, hour, minute int, ok bool) {
	parts := strings.SplitN(s, "T", 2)
	if len(parts) != 2 {
		return 0, 0, 0, 0, 0, false
	}
	y, m, d, dok := parseDate(parts[0])
	if !dok {
		return 0, 0, 0, 0, 0, false
	}
	h, mi, tok := parseHHMM(parts[1])
	if !tok {
		return 0, 0, 0, 0, 0, false
	}
	return y, m, d, h, mi, true
}

// slotTimesForDay returns every local "HH:MM" grid start, from opens (inclusive) while
// start+durationMinutes <= closes, stepping by slotMinutes.
func slotTimesForDay(oh store.OpeningHour, slotMinutes, durationMinutes int) []string {
	openH, openM, _ := parseHHMM(oh.Opens)
	closeH, closeM, _ := parseHHMM(oh.Closes)
	openMin := openH*60 + openM
	closeMin := closeH*60 + closeM
	var out []string
	for t := openMin; t+durationMinutes <= closeMin; t += slotMinutes {
		out = append(out, formatHHMM(t/60, t%60))
	}
	return out
}

func formatHHMM(h, m int) string {
	return pad2(h) + ":" + pad2(m)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// resolveLocalString resolves a "YYYY-MM-DDTHH:MM" string against the restaurant's timezone.
func resolveLocalString(loc *time.Location, local string) (time.Time, error) {
	y, m, d, h, mi, ok := splitLocal(local)
	if !ok {
		return time.Time{}, timeutil.ErrSkipped
	}
	return timeutil.ResolveLocal(loc, y, time.Month(m), d, h, mi)
}
