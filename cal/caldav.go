package cal

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type CaldavURL struct {
	BaseURL  *url.URL // URL without credentials
	AppleID  string
	Password string // empty if not provided
	HasPass  bool
}

// ParseCaldavURL parses URLs of the form:
//
//	http[s]://[apple-id][:password]@host[:port]/path?query#frag
//
// It is tolerant of Apple IDs containing "@" without percent-encoding by
// splitting the authority at the *last* '@'.
//
// Recommended input (standards-compliant):
//
//	https://matthias.hochgatterer%40gmail.com:pass@caldav.icloud.com/
func ParseCaldavURL(raw string) (*CaldavURL, error) {
	if raw == "" {
		return nil, errors.New("empty url")
	}

	// Split scheme://rest
	i := strings.Index(raw, "://")
	if i <= 0 {
		return nil, fmt.Errorf("missing scheme in %q", raw)
	}
	scheme := strings.ToLower(raw[:i])
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q (want http/https)", scheme)
	}
	rest := raw[i+3:]

	// Split rest into authority and the remainder (/path?query#frag or empty)
	authority := rest
	remainder := ""
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		authority = rest[:j]
		remainder = rest[j:]
	}
	if authority == "" {
		return nil, fmt.Errorf("missing authority in %q", raw)
	}

	// authority is: [userinfo@]host[:port]
	// We REQUIRE userinfo here because that's your desired format.
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return nil, fmt.Errorf("missing credentials (no @) in %q", raw)
	}
	userinfoRaw := authority[:at]
	hostport := authority[at+1:]
	if hostport == "" {
		return nil, fmt.Errorf("missing host after @ in %q", raw)
	}
	if userinfoRaw == "" {
		return nil, fmt.Errorf("missing userinfo before @ in %q", raw)
	}

	// userinfo is: user[:password]
	// Use first ":" as separator; everything after it is password (can include ':')
	userRaw := userinfoRaw
	passRaw := ""
	hasPass := false
	if c := strings.Index(userinfoRaw, ":"); c >= 0 {
		userRaw = userinfoRaw[:c]
		passRaw = userinfoRaw[c+1:]
		hasPass = true
	}
	if userRaw == "" {
		return nil, fmt.Errorf("missing apple-id in %q", raw)
	}

	// Percent-decode user/pass (so %40 works for '@', etc.)
	user, err := url.QueryUnescape(userRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid percent-encoding in apple-id: %w", err)
	}
	pass := ""
	if hasPass {
		pass, err = url.QueryUnescape(passRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid percent-encoding in password: %w", err)
		}
	}

	// Build sanitized URL without credentials
	sanitized := scheme + "://" + hostport + remainder
	u, err := url.Parse(sanitized)
	if err != nil {
		return nil, fmt.Errorf("invalid url after sanitizing creds: %w", err)
	}
	// Ensure credentials are not present
	u.User = nil

	return &CaldavURL{
		BaseURL:  u,
		AppleID:  user,
		Password: pass,
		HasPass:  hasPass,
	}, nil
}
