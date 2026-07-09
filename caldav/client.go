package caldav

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brutella/smsremind/cal"
	ical "github.com/emersion/go-ical"
)

type Client struct {
	baseURL *url.URL
	user    string
	pass    string
	http    *http.Client
}

func NewClient(endpoint, user, pass string, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		httpClient = NewHTTPClient(30 * time.Second)
	}
	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("invalid endpoint: empty")
	}

	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid endpoint: missing scheme or host")
	}

	return &Client{baseURL: baseURL, user: user, pass: pass, http: httpClient}, nil
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				if auth := via[0].Header.Get("Authorization"); auth != "" {
					req.Header.Set("Authorization", auth)
				}
			}
			return nil
		},
	}
}

type CalendarInfo struct {
	DisplayName string
	URL         *url.URL
}

func (c *Client) Events(ctx context.Context, start, end time.Time, calendars []string, defaultTZ *time.Location) ([]cal.Event, error) {
	if defaultTZ == nil {
		defaultTZ = time.Local
	}

	principalHref, err := c.propfindCurrentUserPrincipal(ctx, c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("current-user-principal: %w", err)
	}
	principalURL := resolveHref(c.baseURL, principalHref)

	homeSetHref, err := c.propfindCalendarHomeSet(ctx, principalURL)
	if err != nil {
		return nil, fmt.Errorf("calendar-home-set: %w", err)
	}
	homeSetURL := resolveHref(principalURL, homeSetHref)

	calInfos, err := c.propfindCalendars(ctx, homeSetURL)
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	filtered, err := filterCalendars(calInfos, calendars)
	if err != nil {
		return nil, err
	}

	var events []cal.Event
	for _, calInfo := range filtered {
		icsBlobs, err := c.reportCalendarQuery(ctx, calInfo.URL, start, end)
		if err != nil {
			return nil, fmt.Errorf("report calendar %s: %w", calInfo.DisplayName, err)
		}

		for _, icsText := range icsBlobs {
			dec := ical.NewDecoder(strings.NewReader(icsText))
			for {
				calObj, derr := dec.Decode()
				if derr == io.EOF {
					break
				}
				if derr != nil {
					return nil, fmt.Errorf("decode ics for calendar %s: %w", calInfo.DisplayName, derr)
				}

				evs, perr := eventsFromCalendar(calObj, defaultTZ)
				if perr != nil {
					return nil, fmt.Errorf("parse events for calendar %s: %w", calInfo.DisplayName, perr)
				}
				events = append(events, evs...)
			}
		}
	}

	return events, nil
}

func filterCalendars(calInfos []CalendarInfo, calendars []string) ([]CalendarInfo, error) {
	if len(calendars) == 0 {
		return calInfos, nil
	}

	available := make(map[string]CalendarInfo, len(calInfos))
	for _, calInfo := range calInfos {
		available[strings.ToLower(calInfo.DisplayName)] = calInfo
	}

	result := make([]CalendarInfo, 0, len(calendars))
	missing := make([]string, 0)
	for _, name := range calendars {
		calInfo, ok := available[strings.ToLower(name)]
		if !ok {
			missing = append(missing, name)
			continue
		}
		result = append(result, calInfo)
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("calendars not found: %v", missing)
	}

	return result, nil
}

func (c *Client) doDAV(ctx context.Context, method string, u *url.URL, depth string, body []byte) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, 0, err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/xml, text/xml, */*")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Accept-Encoding", "gzip")
	if depth != "" {
		req.Header.Set("Depth", depth)
	}

	resp, err := c.http.Do(req)
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b, resp.Header, resp.StatusCode, fmt.Errorf("%s %s -> %s", method, u.String(), resp.Status)
	}

	return b, resp.Header, resp.StatusCode, nil
}

func resolveHref(base *url.URL, href string) *url.URL {
	href = strings.TrimSpace(href)
	u, err := url.Parse(href)
	if err != nil {
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

func (c *Client) propfindCurrentUserPrincipal(ctx context.Context, endpoint *url.URL) (string, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:current-user-principal/></d:prop>
</d:propfind>`)

	b, _, _, err := c.doDAV(ctx, "PROPFIND", endpoint, "0", body)
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

func (c *Client) propfindCalendarHomeSet(ctx context.Context, principal *url.URL) (string, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav">
  <d:prop><cal:calendar-home-set/></d:prop>
</d:propfind>`)

	b, _, _, err := c.doDAV(ctx, "PROPFIND", principal, "0", body)
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

func (c *Client) propfindCalendars(ctx context.Context, home *url.URL) ([]CalendarInfo, error) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cal="urn:ietf:params:xml:ns:caldav">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
  </d:prop>
</d:propfind>`)

	b, _, _, err := c.doDAV(ctx, "PROPFIND", home, "1", body)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, string(b))
	}

	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, err
	}

	var out []CalendarInfo
	for _, r := range ms.Responses {
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

func (c *Client) reportCalendarQuery(ctx context.Context, calURL *url.URL, start, end time.Time) ([]string, error) {
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

	b, _, _, err := c.doDAV(ctx, "REPORT", calURL, "1", body)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, string(b))
	}

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

		summary, err := firstTextPropValue(c.Props, "SUMMARY")
		if err != nil {
			return nil, fmt.Errorf("parse SUMMARY for %s: %w", uid, err)
		}
		description, err := firstTextPropValue(c.Props, "DESCRIPTION")
		if err != nil {
			return nil, fmt.Errorf("parse DESCRIPTION for %s: %w", uid, err)
		}
		comment, err := firstTextPropValue(c.Props, "COMMENT")
		if err != nil {
			return nil, fmt.Errorf("parse COMMENT for %s: %w", uid, err)
		}

		out = append(out, cal.Event{
			UID:         uid,
			Start:       start,
			End:         end,
			Summary:     summary,
			Description: description,
			Comment:     comment,
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

// firstPropValue returns the raw unfolded iCalendar value without TEXT
// unescaping. Use this for opaque identifiers and structured values.
func firstPropValue(props ical.Props, name string) string {
	p := firstProp(props, name)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Value)
}

// firstTextPropValue decodes RFC 5545 TEXT escaping, e.g. "\n" into a
// newline. Use firstPropValue instead for opaque/raw fields such as UID and
// date/time properties, where changing the raw value can affect identity.
func firstTextPropValue(props ical.Props, name string) (string, error) {
	p := firstProp(props, name)
	if p == nil {
		return "", nil
	}

	text, err := p.Text()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
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

	if valueType == "DATE" || (len(v) == 8 && !strings.Contains(v, "T")) {
		t, err := time.ParseInLocation("20060102", v, defaultTZ)
		return t, true, err
	}

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
