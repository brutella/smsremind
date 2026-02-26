package aspsms

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSendSimpleTextSMSSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("MSISDN"); got != "+436604670967" {
			t.Fatalf("unexpected MSISDN: %s", got)
		}
		if got := r.URL.Query().Get("Originator"); got != "Reminder" {
			t.Fatalf("unexpected originator: %s", got)
		}
		_, _ = io.WriteString(w, `{"ErrorCode":1,"ErrorDescription":"OK"}`)
	}))
	defer server.Close()

	client := NewClientWithHTTP("key", "pwd", "Reminder", server.Client(), server.URL)
	if err := client.SendSimpleTextSMS("+436604670967", "hello"); err != nil {
		t.Fatalf("SendSimpleTextSMS error = %v", err)
	}
}

func TestSendSimpleTextSMSApiError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ErrorCode":23,"ErrorDescription":"Invalid UserKey"}`)
	}))
	defer server.Close()

	client := NewClientWithHTTP("key", "pwd", "Reminder", server.Client(), server.URL)
	err := client.SendSimpleTextSMS("+436604670967", "hello")
	if err == nil || !strings.Contains(err.Error(), "aspsms error") {
		t.Fatalf("expected aspsms error, got %v", err)
	}
}

func TestSendSimpleTextSMSHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no")
	}))
	defer server.Close()

	client := NewClientWithHTTP("key", "pwd", "Reminder", server.Client(), server.URL)
	err := client.SendSimpleTextSMS("+436604670967", "hello")
	if err == nil || !strings.Contains(err.Error(), "http 401") {
		t.Fatalf("expected http status error, got %v", err)
	}
}

func TestSendSimpleTextSMSUnexpectedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not-json")
	}))
	defer server.Close()

	client := NewClientWithHTTP("key", "pwd", "Reminder", server.Client(), server.URL)
	err := client.SendSimpleTextSMS("+436604670967", "hello")
	if err == nil || !strings.Contains(err.Error(), "unexpected ASPSMS response") {
		t.Fatalf("expected unexpected response error, got %v", err)
	}
}

func TestNewClientWithHTTPDefaultEndpoint(t *testing.T) {
	c := NewClientWithHTTP("k", "p", "o", &http.Client{}, "")
	u, err := url.Parse(c.endpoint)
	if err != nil {
		t.Fatalf("endpoint parse error: %v", err)
	}
	if u.Host != "webapi.aspsms.com" {
		t.Fatalf("unexpected host: %s", u.Host)
	}
}
