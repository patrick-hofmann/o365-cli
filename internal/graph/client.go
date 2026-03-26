package graph

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	GraphAPIBaseURL = "https://graph.microsoft.com/v1.0"
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

// DoRequest performs an HTTP request to Graph API.
func (c *Client) DoRequest(method, endpoint string, body []byte) ([]byte, error) {
	var req *http.Request
	var err error

	if body != nil {
		req, err = http.NewRequest(method, endpoint, bytes.NewBuffer(body))
	} else {
		req, err = http.NewRequest(method, endpoint, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Graph API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
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
