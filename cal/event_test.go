package cal

import (
	"strings"
	"testing"
	"time"
)

func TestEventFormatters(t *testing.T) {
	e := Event{
		Start:       time.Date(2026, 2, 27, 12, 30, 0, 0, time.UTC),
		End:         time.Date(2026, 2, 27, 13, 45, 0, 0, time.UTC),
		Summary:     "Checkup",
		Description: "Bring docs",
	}

	if e.StartDate() != "2026-02-27" {
		t.Fatalf("StartDate = %q", e.StartDate())
	}
	if e.StartTime() != "12:30" {
		t.Fatalf("StartTime = %q", e.StartTime())
	}
	if e.EndTime() != "13:45" {
		t.Fatalf("EndTime = %q", e.EndTime())
	}
	if s := e.String(); !strings.Contains(s, "Checkup") {
		t.Fatalf("String() missing summary: %s", s)
	}
}
