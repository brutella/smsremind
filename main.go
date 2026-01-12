package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/brutella/smsremind/aspsms"
	"github.com/brutella/smsremind/cal"
	"github.com/brutella/smsremind/idempotency"
	ical "github.com/emersion/go-ical"
)

var stateDir = flag.String("state-dir", ".", "Directory used to store internal states.")
var offset = flag.Int("offset", 1, "Number of days in the future from now for which a reminder should be sent.")
var calendars = flag.String("calendars", "", "Command separates list of calendar names")
var caldav = flag.String("caldav", "", "The caldav URL include the Apple ID and app-specific password.")
var dryRun = flag.Bool("dry-run", true, "Do not send SMS – only print.")
var msg = flag.String("sms-template", "Your next appointment is on {{ .StartDate }} at {{ .StartTime }}", "The SMS template")
var sender = flag.String("sender", "Reminder", "The SMS originator name.")
var aspsmsUserkey = flag.String("aspsms-userkey", "", "The ASPSMS Userkey")
var aspsmsApiPwd = flag.String("aspsms-password", "", "The ASPSMS API password")
var timezone = flag.String("timezone", "Europe/Vienna", "Timezone location")

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	flag.Parse()
	msgTmpl, err := template.New("output").Parse(*msg)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(*stateDir, "simremind.lock")
	lock, err := idempotency.AcquireLock(lockPath, 1*time.Minute)
	if err != nil {
		// Another instance is running or lock is valid → exit quietly
		os.Exit(0)
	}
	defer lock.Release()

	statePath := filepath.Join(*stateDir, "sent.json")
	store, err := idempotency.Open(statePath)
	if err != nil {
		return err
	}
	defer store.Close()

	calURL, err := cal.ParseCaldavURL(*caldav)
	if err != nil {
		return err
	}

	client := aspsms.NewClient(*aspsmsUserkey, *aspsmsApiPwd, *sender, 5*time.Second)

	ctx := context.Background()
	loc, err := time.LoadLocation(*timezone)
	if err != nil {
		log.Fatal("timezone:", err)
	}

	day := time.Now().AddDate(0, 0, *offset)
	query := Query{
		Endpoint:  calURL.BaseURL.String(),
		AppleId:   calURL.AppleID,
		Password:  calURL.Password,
		Start:     startOfDay(day, loc),
		End:       endOfDay(day, loc),
		Calendars: parseCalendarNames(*calendars),
	}
	events, err := execute(ctx, query, loc)
	if err != nil {
		return err
	}

	for _, event := range events {
		num := cal.EventPhoneNumber(event)
		if num == "" {
			// Skip if no phone number was found.
			continue
		}

		key := eventMessageKey(event)
		if store.Exists(key) {
			// Skip messages which where already sent.
			continue
		}

		// Generate a new message
		var buf bytes.Buffer
		if err := msgTmpl.Execute(&buf, event); err != nil {
			return err
		}
		msg := buf.String()
		fmt.Fprintf(os.Stdout, "remind %s %s: %s\n", event.Summary, num, msg)
		if *dryRun {
			continue
		}

		if err := client.SendSimpleTextSMS(num, msg); err != nil {
			return err
		}

		err = store.Mark(key)
		if err != nil {
			return err
		}
	}

	return nil
}

type Query struct {
	Endpoint  string
	AppleId   string
	Password  string
	Start     time.Time
	End       time.Time
	Calendars []string
}

func execute(ctx context.Context, query Query, defaultTZ *time.Location) ([]cal.Event, error) {
	if defaultTZ == nil {
		defaultTZ = time.Local
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Preserve Authorization across redirects (iCloud often redirects to pXX host).
			if len(via) > 0 {
				if auth := via[0].Header.Get("Authorization"); auth != "" {
					req.Header.Set("Authorization", auth)
				}
			}
			return nil
		},
	}

	endpoint := query.Endpoint
	appleID := query.AppleId
	appPassword := query.Password

	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}

	// 1) Discover current-user-principal
	principalHref, err := propfindCurrentUserPrincipal(ctx, httpClient, baseURL, appleID, appPassword)
	if err != nil {
		return nil, fmt.Errorf("current-user-principal: %w", err)
	}
	principalURL := resolveHref(baseURL, principalHref)

	// 2) Discover calendar-home-set
	homeSetHref, err := propfindCalendarHomeSet(ctx, httpClient, principalURL, appleID, appPassword)
	if err != nil {
		return nil, fmt.Errorf("calendar-home-set: %w", err)
	}
	homeSetURL := resolveHref(principalURL, homeSetHref)

	// 3) List calendars (Depth:1) under home set
	calendars, err := propfindCalendars(ctx, httpClient, homeSetURL, appleID, appPassword)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	start := query.Start
	end := query.End

	events := []cal.Event{}
	for _, cal := range calendars {
		if len(query.Calendars) > 0 {
			// Filter by name
			var found = false
			for _, name := range query.Calendars {
				if strings.EqualFold(cal.DisplayName, name) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		icsBlobs, err := reportCalendarQuery(ctx, httpClient, cal.URL, appleID, appPassword, start, end)
		if err != nil {
			continue
		}
		if len(icsBlobs) == 0 {
			continue
		}

		for _, icsText := range icsBlobs {
			// Parse returned VCALENDAR text
			dec := ical.NewDecoder(strings.NewReader(icsText))
			for {
				calObj, derr := dec.Decode()
				if derr == io.EOF {
					break
				}
				if derr != nil {
					break
				}

				evs, perr := eventsFromCalendar(calObj, defaultTZ)
				if perr != nil {
					break
				}

				events = append(events, evs...)
			}
		}
	}

	return events, nil
}

func parseCalendarNames(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Returns the time marking the start of a day.
func startOfDay(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// Returns the time marking the end of a day.
func endOfDay(d time.Time, loc *time.Location) time.Time {
	start := startOfDay(d, loc)
	return start.AddDate(0, 0, 1)
}

// Returns the UUID of a message related to an event.
func eventMessageKey(event cal.Event) string {
	return event.UID + "|" + event.Start.Format(time.RFC3339) + fmt.Sprintf("|T-%dd", *offset)
}

func doDAV(ctx context.Context, c *http.Client, method string, u *url.URL, user, pass string, depth string, body []byte) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, 0, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Accept", "application/xml, text/xml, */*")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Accept-Encoding", "gzip")
	if depth != "" {
		req.Header.Set("Depth", depth)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	var r io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, resp.Header, resp.StatusCode, err
		}
		defer gr.Close()
		r = gr
	}

	b, err := io.ReadAll(r)
	if err != nil {
		return nil, resp.Header, resp.StatusCode, err
	}

	// WebDAV uses 207 Multi-Status for PROPFIND/REPORT (still success).
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b, resp.Header, resp.StatusCode, fmt.Errorf("%s %s -> %s", method, u.String(), resp.Status)
	}

	return b, resp.Header, resp.StatusCode, nil
}

func resolveHref(base *url.URL, href string) *url.URL {
	href = strings.TrimSpace(href)
	u, err := url.Parse(href)
	if err != nil {
		// fallback: treat as relative path
		return base.ResolveReference(&url.URL{Path: href})
	}
	return base.ResolveReference(u)
}

type multistatus struct {
	XMLName   xml.Name `xml:"multistatus"`
	Responses []msResp `xml:"response"`
}
type msResp struct {
	Href      string     `xml:"href"`
	Propstats []propstat `xml:"propstat"`
}
type propstat struct {
	Prop props `xml:"prop"`
}
type props struct {
	CurrentUserPrincipal hrefSet `xml:"current-user-principal"`
	CalendarHomeSet      hrefSet `xml:"calendar-home-set"`
	DisplayName          string  `xml:"displayname"`
	ResourceType         resType `xml:"resourcetype"`
}
type hrefSet struct {
	Href string `xml:"href"`
}
type resType struct {
	Collection *struct{} `xml:"collection"`
	Calendar   *struct{} `xml:"calendar"`
}

func propfindCurrentUserPrincipal(ctx context.Context, c *http.Client, endpoint *url.URL, user, pass string) (string, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:current-user-principal/></d:prop>
</d:propfind>`)
	b, _, _, err := doDAV(ctx, c, "PROPFIND", endpoint, user, pass, "0", body)
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, string(b))
	}

	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if ps.Prop.CurrentUserPrincipal.Href != "" {
				return ps.Prop.CurrentUserPrincipal.Href, nil
			}
		}
	}
	return "", fmt.Errorf("current-user-principal not found")
}

func propfindCalendarHomeSet(ctx context.Context, c *http.Client, principal *url.URL, user, pass string) (string, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav">
  <d:prop><cal:calendar-home-set/></d:prop>
</d:propfind>`)
	b, _, _, err := doDAV(ctx, c, "PROPFIND", principal, user, pass, "0", body)
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, string(b))
	}

	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			if ps.Prop.CalendarHomeSet.Href != "" {
				return ps.Prop.CalendarHomeSet.Href, nil
			}
		}
	}
	return "", fmt.Errorf("calendar-home-set not found")
}

type CalendarInfo struct {
	DisplayName string
	URL         *url.URL
}

// 3) list calendars under home set
func propfindCalendars(ctx context.Context, c *http.Client, home *url.URL, user, pass string) ([]CalendarInfo, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
  </d:prop>
</d:propfind>`)

	b, _, _, err := doDAV(ctx, c, "PROPFIND", home, user, pass, "1", body)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, string(b))
	}

	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, err
	}

	var out []CalendarInfo
	for _, r := range ms.Responses {
		// calendar collections have <cal:calendar/> in resourcetype
		for _, ps := range r.Propstats {
			if ps.Prop.ResourceType.Calendar != nil {
				out = append(out, CalendarInfo{
					DisplayName: strings.TrimSpace(ps.Prop.DisplayName),
					URL:         resolveHref(home, r.Href),
				})
				break
			}
		}
	}
	return out, nil
}

// 4) REPORT calendar-query: fetch calendar-data for VEVENTs in range
func reportCalendarQuery(ctx context.Context, c *http.Client, calURL *url.URL, user, pass string, start, end time.Time) ([]string, error) {
	startUTC := start.UTC().Format("20060102T150405Z")
	endUTC := end.UTC().Format("20060102T150405Z")

	body := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:getetag/>
    <c:calendar-data/>
  </d:prop>
  <c:filter>
    <c:comp-filter name="VCALENDAR">
      <c:comp-filter name="VEVENT">
        <c:time-range start="%s" end="%s"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>`, startUTC, endUTC))

	b, _, _, err := doDAV(ctx, c, "REPORT", calURL, user, pass, "1", body)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, string(b))
	}

	// Parse multistatus and extract <calendar-data>
	type reportMS struct {
		Responses []struct {
			Propstats []struct {
				Prop struct {
					CalendarData string `xml:"calendar-data"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	var ms reportMS
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, err
	}

	var out []string
	for _, r := range ms.Responses {
		for _, ps := range r.Propstats {
			cd := strings.TrimSpace(ps.Prop.CalendarData)
			if cd != "" {
				out = append(out, cd)
			}
		}
	}
	return out, nil
}

/* =========================
   iCalendar parsing helpers
   ========================= */

func eventsFromCalendar(c *ical.Calendar, defaultTZ *time.Location) ([]cal.Event, error) {
	if c == nil {
		return nil, fmt.Errorf("nil calendar")
	}
	if defaultTZ == nil {
		defaultTZ = time.Local
	}

	var out []cal.Event
	for _, c := range c.Children {
		if c == nil || c.Name != "VEVENT" {
			continue
		}

		uid := firstPropValue(c.Props, "UID")
		if uid == "" {
			uid = "(missing-uid)"
		}

		dtStart := firstProp(c.Props, "DTSTART")
		if dtStart == nil {
			continue
		}
		start, startIsDate, err := parseICalDateTime(dtStart, defaultTZ)
		if err != nil {
			return nil, fmt.Errorf("parse DTSTART for %s: %w", uid, err)
		}

		var end time.Time
		if dtEnd := firstProp(c.Props, "DTEND"); dtEnd != nil {
			end, _, err = parseICalDateTime(dtEnd, defaultTZ)
			if err != nil {
				return nil, fmt.Errorf("parse DTEND for %s: %w", uid, err)
			}
		} else if startIsDate {
			end = start.Add(24 * time.Hour)
		} else {
			end = start
		}

		out = append(out, cal.Event{
			UID:         uid,
			Start:       start,
			End:         end,
			Summary:     firstPropValue(c.Props, "SUMMARY"),
			Description: firstPropValue(c.Props, "DESCRIPTION"),
			Comment:     firstPropValue(c.Props, "COMMENT"),
		})
	}
	return out, nil
}

func firstProp(props ical.Props, name string) *ical.Prop {
	ps := props[name]
	if len(ps) == 0 {
		return nil
	}
	return &ps[0]
}

func firstPropValue(props ical.Props, name string) string {
	p := firstProp(props, name)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Value)
}

func parseICalDateTime(p *ical.Prop, defaultTZ *time.Location) (time.Time, bool, error) {
	if p == nil {
		return time.Time{}, false, fmt.Errorf("nil prop")
	}
	if defaultTZ == nil {
		defaultTZ = time.Local
	}

	v := strings.TrimSpace(p.Value)
	if v == "" {
		return time.Time{}, false, fmt.Errorf("empty datetime")
	}

	getParam := func(key string) string {
		if p.Params == nil {
			return ""
		}
		vals := p.Params[key]
		if len(vals) == 0 {
			return ""
		}
		return strings.TrimSpace(vals[0])
	}

	valueType := strings.ToUpper(getParam("VALUE"))
	tzid := getParam("TZID")

	// All-day date
	if valueType == "DATE" || (len(v) == 8 && !strings.Contains(v, "T")) {
		t, err := time.ParseInLocation("20060102", v, defaultTZ)
		return t, true, err
	}

	// UTC
	if strings.HasSuffix(v, "Z") {
		if t, err := time.Parse("20060102T150405Z", v); err == nil {
			return t, false, nil
		}
		if t, err := time.Parse("20060102T1504Z", v); err == nil {
			return t, false, nil
		}
		return time.Time{}, false, fmt.Errorf("unsupported UTC datetime: %q", v)
	}

	loc := defaultTZ
	if tzid != "" {
		if l, err := time.LoadLocation(tzid); err == nil {
			loc = l
		}
	}

	if t, err := time.ParseInLocation("20060102T150405", v, loc); err == nil {
		return t, false, nil
	}
	if t, err := time.ParseInLocation("20060102T1504", v, loc); err == nil {
		return t, false, nil
	}

	return time.Time{}, false, fmt.Errorf("unsupported datetime: %q", v)
}
