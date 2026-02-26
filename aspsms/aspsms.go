package aspsms

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	userKey    string
	password   string
	originator string
	client     *http.Client
	endpoint   string
}

func NewClient(userKey, password, originator string, timeout time.Duration) *Client {
	return NewClientWithHTTP(userKey, password, originator, &http.Client{Timeout: timeout}, "https://webapi.aspsms.com/SendSimpleSMS")
}

func NewClientWithHTTP(userKey, password, originator string, httpClient *http.Client, endpoint string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "https://webapi.aspsms.com/SendSimpleSMS"
	}

	return &Client{
		userKey:    userKey,
		password:   password,
		originator: originator,
		client:     httpClient,
		endpoint:   endpoint,
	}
}

// SendSimpleSMS uses ASPSMS WebAPI endpoint GET /SendSimpleSMS.
// Parameters (per ASPSMS connector docs): MSISDN, MessageData, Originator, optional LifeTime, DeferredDeliveryTime, TransactionReferenceNumber. :contentReference[oaicite:1]{index=1}
//
// We keep it minimal: MSISDN + MessageData + Originator.
func (c *Client) SendSimpleTextSMS(recipientE164 string, text string) error {
	if c.userKey == "" {
		return fmt.Errorf("missing ASPSMS userkey")
	}
	if c.password == "" {
		return fmt.Errorf("missing ASPSMS password")
	}

	q := url.Values{}
	q.Set("UserKey", c.userKey)
	q.Set("Password", c.password)
	q.Set("MSISDN", recipientE164)
	q.Set("MessageData", text)

	orig := strings.TrimSpace(c.originator)
	if orig != "" {
		q.Set("Originator", orig)
	}

	reqURL := c.endpoint + "?" + q.Encode()
	resp, err := c.client.Get(reqURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// The WebAPI commonly returns an ErrorCode integer (1 == OK).
	if code, descr, ok := parseError(body); ok {
		if code == 0 || code == 1 {
			return nil
		}
		// ASPSMS documents error codes like "Invalid UserKey", "Invalid Password", etc. :contentReference[oaicite:2]{index=2}
		return fmt.Errorf("aspsms error: %s (code: %d)", descr, code)
	}

	return fmt.Errorf("unexpected ASPSMS response: %s", strings.TrimSpace(string(body)))
}

func parseError(body []byte) (int, string, bool) {
	var obj struct {
		ErrorCode        int    `json:"ErrorCode"`
		ErrorDescription string `json:"ErrorDescription"`
	}
	if err := json.Unmarshal(body, &obj); err == nil {
		return obj.ErrorCode, obj.ErrorDescription, true
	}

	return 0, "", false
}
