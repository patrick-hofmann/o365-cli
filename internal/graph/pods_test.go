package graph

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRestrictedReadsDenyMutationsForeignOriginsAndRedirects(t *testing.T) {
	c := NewReadOnlyClient(context.Background(), "synthetic-secret")
	calls := 0
	c.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Prefer") != `IdType="ImmutableId"` {
			t.Fatal("immutable ids missing")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://unassigned.invalid/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		if _, err := c.DoRequest(method, GraphAPIBaseURL+"/me/messages", nil); err == nil {
			t.Fatal("mutation accepted")
		}
	}
	for _, endpoint := range []string{"http://graph.microsoft.com/v1.0/me", "https://graph.microsoft.com:443/v1.0/me", "https://graph.microsoft.com@unassigned.invalid/v1.0/me", "https://graph.microsoft.com/v1.0/../me"} {
		if _, err := c.DoRequest("GET", endpoint, nil); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if calls != 0 {
		t.Fatal("denied request reached transport")
	}
	if _, err := c.DoRequest("GET", GraphAPIBaseURL+"/me/messages", nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls != 1 {
		t.Fatal("redirect followed")
	}
}
func TestThrottledReadsCancelPromptlyWithoutLeakingBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	c := NewReadOnlyClient(ctx, "synthetic-secret")
	calls := 0
	c.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader("synthetic-secret"))}, nil
	})
	start := time.Now()
	_, err := c.DoRequest("GET", GraphAPIBaseURL+"/me/messages", nil)
	if err == nil || strings.Contains(err.Error(), "synthetic-secret") || time.Since(start) > time.Second || calls != 1 {
		t.Fatalf("cancellation: %v, %d calls", err, calls)
	}
}
