package cal

import "testing"

func TestParseCaldavURL(t *testing.T) {
	u, err := ParseCaldavURL("https://alice%40example.com:secret@caldav.icloud.com/")
	if err != nil {
		t.Fatalf("ParseCaldavURL error = %v", err)
	}
	if u.AppleID != "alice@example.com" {
		t.Fatalf("AppleID = %q", u.AppleID)
	}
	if u.Password != "secret" {
		t.Fatalf("Password = %q", u.Password)
	}
	if u.BaseURL.User != nil {
		t.Fatalf("expected sanitized url user=nil")
	}
}

func TestParseCaldavURLMissingCreds(t *testing.T) {
	if _, err := ParseCaldavURL("https://caldav.icloud.com/"); err == nil {
		t.Fatalf("expected error")
	}
}
