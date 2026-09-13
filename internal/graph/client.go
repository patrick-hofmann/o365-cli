package graph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	GraphAPIBaseURL = "https://graph.microsoft.com/v1.0"

	// Graph throttles bulk reads. Exporting a large mailbox means hundreds of
	// paginated requests, so a 429 is expected traffic, not an error.
	maxRetries        = 5
	fallbackRetryWait = 10 * time.Second

	// A single page of 100 messages with bodies is a few megabytes; 30s was
	// enough for metadata but cuts bulk reads off mid-body.
	requestTimeout = 3 * time.Minute
)

// Client is a generic Microsoft Graph API HTTP client.
type Client struct {
	HttpClient  *http.Client
	AccessToken string
	ReadOnly    bool
	Context     context.Context
}

// NewClient creates a new Graph API client.
func NewClient(accessToken string) *Client {
	return &Client{
		HttpClient: &http.Client{
			Timeout: requestTimeout,
		},
		AccessToken: accessToken,
	}
}

func NewReadOnlyClient(ctx context.Context, accessToken string) *Client {
	c := NewClient(accessToken)
	c.ReadOnly, c.Context = true, ctx
	return c
}

func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host != "graph.microsoft.com" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.Path, "/v1.0/") {
		return errors.New("Graph endpoint is outside the assigned origin")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." || strings.ContainsAny(part, "\\\x00") {
			return errors.New("Graph endpoint has an invalid path")
		}
	}
	return nil
}

func (c *Client) DoRequest(method, endpoint string, body []byte) ([]byte, error) {
	if err := ValidateEndpoint(endpoint); err != nil {
		return nil, err
	}
	if c.ReadOnly && (method != http.MethodGet || body != nil) {
		return nil, errors.New("this connection permits only non-mutating reads")
	}
	ctx := c.Context
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		respBody, wait, err := c.doOnce(ctx, method, endpoint, body)
		if wait == 0 {
			return respBody, err
		}
		lastErr = err
		if attempt == maxRetries-1 {
			break
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxRetries, lastErr)
}

func (c *Client) doOnce(ctx context.Context, method, endpoint string, body []byte) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, errors.New("cannot create Graph request")
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	if c.ReadOnly {
		req.Header.Set("Prefer", `IdType="ImmutableId"`)
	}
	client := *c.HttpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return errors.New("Graph redirects are not permitted") }
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, errors.New("Graph request failed or was cancelled")
	}
	defer resp.Body.Close()
	const maxBody = 32 << 20
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fallbackRetryWait, errors.New("Graph response body was truncated")
	}
	if len(respBody) > maxBody {
		return nil, 0, errors.New("Graph response exceeds 32 MiB; narrow the request")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := fmt.Errorf("Graph API returned status %d", resp.StatusCode)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, retryAfter(resp), apiErr
		}
		return nil, 0, apiErr
	}
	return respBody, 0, nil
}

func retryAfter(resp *http.Response) time.Duration {
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 && secs <= 60 {
		return time.Duration(secs) * time.Second
	}
	return fallbackRetryWait
}

// GraphEmailAddress represents an email address.
type GraphEmailAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

// GraphEmailAddressWrapper wraps an email address for Graph API.
type GraphEmailAddressWrapper struct {
	EmailAddress GraphEmailAddress `json:"emailAddress"`
}

// GraphBodyResponse represents a body from Graph API.
type GraphBodyResponse struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

// ParseEmail extracts an email address from a string like "Name <email@example.com>".
func ParseEmail(addr string) string {
	addr = strings.TrimSpace(addr)
	if idx := strings.Index(addr, "<"); idx != -1 {
		if end := strings.Index(addr, ">"); end != -1 {
			return addr[idx+1 : end]
		}
	}
	return addr
}

// FormatGraphAddress formats a Graph API email address.
func FormatGraphAddress(addr GraphEmailAddress) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Address)
	}
	return addr.Address
}
