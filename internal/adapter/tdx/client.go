package tdx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the v3 rail API root.
const DefaultBaseURL = "https://tdx.transportdata.tw/api/basic/v3/Rail/TRA"

// DefaultAuthURL is the OAuth2 token endpoint.
const DefaultAuthURL = "https://tdx.transportdata.tw/auth/realms/TDXConnect/protocol/openid-connect/token"

// backoffSchedule is the wait after each 429. Probing showed the limit trips
// after roughly eight requests in quick succession and that these waits clear
// it. The real flow only makes three requests, so this should never be needed —
// it exists for the day TDX tightens the limit, not for normal operation.
var backoffSchedule = []time.Duration{5 * time.Second, 10 * time.Second, 15 * time.Second}

// Credentials are the TDX client credentials, supplied via the environment.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// Options configure the client.
type Options struct {
	BaseURL string
	AuthURL string
	// Interval is the minimum gap between requests. Requests are issued
	// strictly sequentially: this flow makes three calls a day and has a ten
	// second budget, so there is no reason to risk the rate limit for
	// concurrency that buys nothing.
	Interval time.Duration
	Timeout  time.Duration
	HTTP     *http.Client
	Log      *slog.Logger
	// Sleep is injected by tests so backoff does not really wait.
	Sleep func(time.Duration)
	// Archiver, when set, receives every raw response body.
	Archiver func(name string, payload []byte)
}

// Client is a throttled TDX API client. It is not safe for concurrent use,
// which is deliberate: the throttle only means anything if one goroutine owns
// the request sequence.
type Client struct {
	creds Credentials
	opts  Options
	loc   *time.Location

	token     string
	lastReqAt time.Time
}

// New builds a client. loc is the location every naive API clock is resolved
// against.
func New(creds Credentials, loc *time.Location, opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.AuthURL == "" {
		opts.AuthURL = DefaultAuthURL
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: opts.Timeout}
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Sleep == nil {
		opts.Sleep = time.Sleep
	}
	return &Client{creds: creds, opts: opts, loc: loc}
}

// Authenticate obtains an access token.
//
// The token is not cached to disk. It is valid for 24 hours, but the program
// runs once a day, so a cache would add a file, its permissions and its
// staleness handling in exchange for saving one request out of three.
func (c *Client) Authenticate(ctx context.Context) error {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.creds.ClientID},
		"client_secret": {c.creds.ClientSecret},
	}
	body, err := c.do(ctx, "auth", http.MethodPost, c.opts.AuthURL, form)
	if err != nil {
		return fmt.Errorf("tdx auth: %w", err)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("tdx auth: decode: %w", err)
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("tdx auth: response carried no access_token")
	}
	c.token = tr.AccessToken
	return nil
}

func (c *Client) get(ctx context.Context, name, path string) ([]byte, error) {
	if c.token == "" {
		return nil, fmt.Errorf("tdx: not authenticated")
	}
	endpoint := c.opts.BaseURL + path
	if strings.Contains(endpoint, "?") {
		endpoint += "&$format=JSON"
	} else {
		endpoint += "?$format=JSON"
	}
	body, err := c.do(ctx, name, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// Only data responses are archived. The token response is deliberately
	// excluded: it carries a live bearer credential, and the archive is a
	// long-lived file kept for diagnosing recommendations, where a JWT has no
	// diagnostic value whatsoever.
	if c.opts.Archiver != nil {
		c.opts.Archiver(name, body)
	}
	return body, nil
}

// do issues one request, honouring the inter-request interval and retrying on
// 429 with the fixed backoff schedule.
func (c *Client) do(ctx context.Context, name, method, endpoint string, form url.Values) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= len(backoffSchedule); attempt++ {
		if attempt > 0 {
			wait := backoffSchedule[attempt-1]
			c.opts.Log.Warn("tdx rate limited, backing off",
				"request", name, "attempt", attempt, "wait", wait)
			c.opts.Sleep(wait)
		}
		c.throttle()

		body, status, err := c.roundTrip(ctx, method, endpoint, form)
		if err != nil {
			return nil, err
		}
		switch {
		case status == http.StatusTooManyRequests:
			lastErr = fmt.Errorf("rate limited (HTTP 429) after %d attempts", attempt+1)
			continue
		case status < 200 || status >= 300:
			return nil, fmt.Errorf("HTTP %d: %s", status, truncate(string(body), 200))
		}
		return body, nil
	}
	return nil, lastErr
}

func (c *Client) roundTrip(ctx context.Context, method, endpoint string, form url.Values) ([]byte, int, error) {
	var reader io.Reader
	if form != nil {
		reader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// throttle enforces the minimum gap since the previous request.
func (c *Client) throttle() {
	defer func() { c.lastReqAt = time.Now() }()
	if c.lastReqAt.IsZero() || c.opts.Interval <= 0 {
		return
	}
	if wait := c.opts.Interval - time.Since(c.lastReqAt); wait > 0 {
		c.opts.Sleep(wait)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
