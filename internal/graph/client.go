package graph

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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
)

// Client is a generic Microsoft Graph API HTTP client.
type Client struct {
	HttpClient  *http.Client
	AccessToken string
}

// NewClient creates a new Graph API client.
func NewClient(accessToken string) *Client {
	return &Client{
		HttpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		AccessToken: accessToken,
	}
}

// DoRequest performs an HTTP request to Graph API, retrying while Graph asks
// us to slow down.
func (c *Client) DoRequest(method, endpoint string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		respBody, retryAfter, err := c.doOnce(method, endpoint, body)
		if retryAfter == 0 {
			return respBody, err
		}
		lastErr = err
		time.Sleep(retryAfter)
	}
	return nil, fmt.Errorf("giving up after %d throttled attempts: %w", maxRetries, lastErr)
}

// doOnce returns a non-zero retryAfter when the request should be repeated.
func (c *Client) doOnce(method, endpoint string, body []byte) ([]byte, time.Duration, error) {
	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(method, endpoint, bytes.NewBuffer(body))
	} else {
		req, err = http.NewRequest(method, endpoint, nil)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		apiErr := fmt.Errorf("Graph API error (status %d): %s", resp.StatusCode, string(respBody))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, retryAfter(resp), apiErr
		}
		return nil, 0, apiErr
	}

	return respBody, 0, nil
}

// retryAfter reads the Retry-After header Graph sends with a 429.
func retryAfter(resp *http.Response) time.Duration {
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
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
