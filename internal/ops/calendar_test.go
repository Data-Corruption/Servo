package ops

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestCalendarDST(t *testing.T) {
	zone, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, wall, now, want string }{
		{"spring missing", "02:30", "2026-03-08T00:00:00-08:00", "2026-03-09T02:30:00-07:00"},
		{"fall first", "01:30", "2026-11-01T00:00:00-07:00", "2026-11-01T01:30:00-07:00"},
		{"fall no second", "01:30", "2026-11-01T01:40:00-07:00", "2026-11-02T01:30:00-08:00"},
		{"spring next day", "04:00", "2026-03-07T12:00:00-08:00", "2026-03-08T04:00:00-07:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tc.now)
			next, err := nextOccurrence(tc.wall, now.In(zone))
			if err != nil || next.Format(time.RFC3339) != tc.want {
				t.Fatalf("got %v (%v), want %s", next, err, tc.want)
			}
		})
	}
}
