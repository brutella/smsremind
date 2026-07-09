package caldav

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brutella/smsremind/cal"
	ical "github.com/emersion/go-ical"
)

func TestEventsFromCalendarDecodesEscapedDescription(t *testing.T) {
	ics := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:video-call\nDTSTART:20260710T090000\nDTEND:20260710T100000\nSUMMARY:Matthias Hochgatterer\nDESCRIPTION:----( Video Call )----\\ntel:06604670967\\n---===---\nEND:VEVENT\nEND:VCALENDAR\n"
	decoded, err := ical.NewDecoder(strings.NewReader(ics)).Decode()
	if err != nil {
		t.Fatalf("Decode error = %v", err)
	}

	events, err := eventsFromCalendar(decoded, time.UTC)
	if err != nil {
		t.Fatalf("eventsFromCalendar error = %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	wantDescription := "----( Video Call )----\ntel:06604670967\n---===---"
	if events[0].Description != wantDescription {
		t.Fatalf("Description = %q, want %q", events[0].Description, wantDescription)
	}
	if got, want := cal.EventPhoneNumber(events[0]), "+436604670967"; got != want {
		t.Fatalf("EventPhoneNumber = %q, want %q", got, want)
	}
}

func TestClientEventsSuccessAndFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			_, _ = io.WriteString(w, principalResponse("/principal/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/principal/":
			_, _ = io.WriteString(w, homeSetResponse("/home/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/home/":
			_, _ = io.WriteString(w, calendarsResponse())
		case r.Method == "REPORT" && r.URL.Path == "/home/work/":
			_, _ = io.WriteString(w, reportResponse(validICS("uid-work", "Work event")))
		case r.Method == "REPORT" && r.URL.Path == "/home/private/":
			_, _ = io.WriteString(w, reportResponse(validICS("uid-private", "Private event")))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "u", "p", server.Client())
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}

	events, err := client.Events(context.Background(), time.Now(), time.Now().Add(24*time.Hour), []string{"Work"}, time.UTC)
	if err != nil {
		t.Fatalf("Events error = %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].UID != "uid-work" {
		t.Fatalf("unexpected uid: %s", events[0].UID)
	}
}

func TestClientEventsHandlesGzip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			_, _ = io.WriteString(w, principalResponse("/principal/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/principal/":
			_, _ = io.WriteString(w, homeSetResponse("/home/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/home/":
			_, _ = io.WriteString(w, calendarsSingleResponse())
		case r.Method == "REPORT" && r.URL.Path == "/home/work/":
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = io.WriteString(gz, reportResponse(validICS("uid-gzip", "Gzip event")))
			_ = gz.Close()
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "u", "p", server.Client())
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}

	events, err := client.Events(context.Background(), time.Now(), time.Now().Add(24*time.Hour), nil, time.UTC)
	if err != nil {
		t.Fatalf("Events error = %v", err)
	}
	if len(events) != 1 || events[0].UID != "uid-gzip" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestClientEventsMalformedICS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			_, _ = io.WriteString(w, principalResponse("/principal/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/principal/":
			_, _ = io.WriteString(w, homeSetResponse("/home/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/home/":
			_, _ = io.WriteString(w, calendarsSingleResponse())
		case r.Method == "REPORT" && r.URL.Path == "/home/work/":
			bad := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:1\nDTSTART:bad\nEND:VEVENT\nEND:VCALENDAR\n"
			_, _ = io.WriteString(w, reportResponse(bad))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "u", "p", server.Client())
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}

	_, err = client.Events(context.Background(), time.Now(), time.Now().Add(24*time.Hour), nil, time.UTC)
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestClientEventsFailsWhenRequestedCalendarIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			_, _ = io.WriteString(w, principalResponse("/principal/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/principal/":
			_, _ = io.WriteString(w, homeSetResponse("/home/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/home/":
			_, _ = io.WriteString(w, calendarsResponse())
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "u", "p", server.Client())
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}

	_, err = client.Events(context.Background(), time.Now(), time.Now().Add(24*time.Hour), []string{"Missing"}, time.UTC)
	if err == nil || !strings.Contains(err.Error(), "calendars not found") {
		t.Fatalf("expected missing calendar error, got %v", err)
	}
}

func TestClientEventsFailsWhenAnyRequestedCalendarIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/":
			_, _ = io.WriteString(w, principalResponse("/principal/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/principal/":
			_, _ = io.WriteString(w, homeSetResponse("/home/"))
		case r.Method == "PROPFIND" && r.URL.Path == "/home/":
			_, _ = io.WriteString(w, calendarsResponse())
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "u", "p", server.Client())
	if err != nil {
		t.Fatalf("NewClient error = %v", err)
	}

	_, err = client.Events(context.Background(), time.Now(), time.Now().Add(24*time.Hour), []string{"Work", "Missing"}, time.UTC)
	if err == nil || !strings.Contains(err.Error(), "calendars not found") {
		t.Fatalf("expected missing calendar error, got %v", err)
	}
}

func principalResponse(href string) string {
	return fmt.Sprintf(`<multistatus><response><propstat><prop><current-user-principal><href>%s</href></current-user-principal></prop></propstat></response></multistatus>`, href)
}

func homeSetResponse(href string) string {
	return fmt.Sprintf(`<multistatus><response><propstat><prop><calendar-home-set><href>%s</href></calendar-home-set></prop></propstat></response></multistatus>`, href)
}

func calendarsResponse() string {
	return `<multistatus>
<response><href>/home/work/</href><propstat><prop><displayname>Work</displayname><resourcetype><collection/><calendar/></resourcetype></prop></propstat></response>
<response><href>/home/private/</href><propstat><prop><displayname>Private</displayname><resourcetype><collection/><calendar/></resourcetype></prop></propstat></response>
</multistatus>`
}

func calendarsSingleResponse() string {
	return `<multistatus>
<response><href>/home/work/</href><propstat><prop><displayname>Work</displayname><resourcetype><collection/><calendar/></resourcetype></prop></propstat></response>
</multistatus>`
}

func reportResponse(ics string) string {
	return fmt.Sprintf(`<multistatus><response><propstat><prop><calendar-data><![CDATA[%s]]></calendar-data></prop></propstat></response></multistatus>`, ics)
}

func validICS(uid, summary string) string {
	return fmt.Sprintf("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:%s\nDTSTART:20260227T123000Z\nDTEND:20260227T130000Z\nSUMMARY:%s\nEND:VEVENT\nEND:VCALENDAR\n", uid, summary)
}
