package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type offlineOAuthTransport struct {
	mode      string
	refreshes int
	requests  []string
	scopes    []string
}

func (f *offlineOAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "login.microsoftonline.com" {
		return nil, fmt.Errorf("unexpected fixture host: %s", r.URL.Host)
	}
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	body := ""
	status := 200
	authority := "https://login.microsoftonline.com/common"
	switch {
	case strings.Contains(r.URL.Path, ".well-known"):
		body = fmt.Sprintf(`{"token_endpoint":%q,"authorization_endpoint":%q,"issuer":%q}`, authority+"/oauth2/v2.0/token", authority+"/oauth2/v2.0/authorize", authority+"/v2.0")
	case strings.Contains(r.URL.Path, "discovery/instance"):
		body = fmt.Sprintf(`{"tenant_discovery_endpoint":%q,"metadata":[{"preferred_network":"login.microsoftonline.com","preferred_cache":"login.microsoftonline.com","aliases":["login.microsoftonline.com"]}]}`, authority+"/v2.0/.well-known/openid-configuration")
	case strings.HasSuffix(r.URL.Path, "/devicecode"):
		body = `{"device_code":"synthetic-device","user_code":"SYNTHETIC","verification_uri":"https://fixture.invalid/verify","expires_in":600,"interval":1,"message":"Synthetic fixture"}`
	case strings.HasSuffix(r.URL.Path, "/token"):
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		if f.mode == "cancel" {
			return nil, context.Canceled
		}
		if r.Form.Get("grant_type") == "refresh_token" {
			f.refreshes++
			if f.mode == "invalid" {
				status = 400
				body = `{"error":"invalid_grant","error_description":"Synthetic expired refresh"}`
				break
			}
		}
		expiry := 3600
		token := "synthetic-access-refreshed"
		if r.Form.Get("grant_type") != "refresh_token" {
			expiry = 1
			token = "synthetic-access-initial"
		}
		claims, _ := json.Marshal(map[string]any{"aud": DefaultClientID, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "iss": authority + "/v2.0", "tid": "fixture-tenant", "oid": "fixture-user", "sub": "fixture-user", "preferred_username": "pod@example.invalid"})
		id := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
		info := base64.RawURLEncoding.EncodeToString([]byte(`{"uid":"fixture-user","utid":"fixture-tenant"}`))
		scope := r.Form.Get("scope")
		if scope == "" {
			scope = strings.Join(Scopes, " ")
		}
		f.scopes = append(f.scopes, scope)
		b, _ := json.Marshal(map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": expiry, "refresh_token": "synthetic-refresh", "id_token": id, "client_info": info, "scope": scope})
		body = string(b)
	default:
		return nil, fmt.Errorf("unexpected fixture endpoint: %s", r.URL)
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
func TestPodsOAuthDeviceRefreshRestartAccountAndInvalidRefresh(t *testing.T) {
	f := &offlineOAuthTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
	dir := t.TempDir()
	c, err := NewOAuthClient("", dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	code, ch, err := c.StartDeviceCodeFlow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if code.UserCode != "SYNTHETIC" {
		t.Fatal("wrong fixture code")
	}
	result := <-ch
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	if result.Email != "pod@example.invalid" {
		t.Fatalf("account mismatch: %s", result.Email)
	}
	restarted, err := NewOAuthClient("", dir)
	if err != nil {
		t.Fatal(err)
	}
	token, err := restarted.GetAccessToken(ctx, "pod@example.invalid")
	if err != nil || token != "synthetic-access-refreshed" || f.refreshes != 1 {
		t.Fatalf("refresh: %s %v count=%d", token, err, f.refreshes)
	}
	if _, err := restarted.GetAccessToken(ctx, "other@example.invalid"); err == nil {
		t.Fatal("cross-account fallback")
	}
	d, err := NewOAuthClient("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, ch, err = d.StartDeviceCodeFlow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if x := <-ch; x.Error != nil {
		t.Fatal(x.Error)
	}
	f.mode = "invalid"
	if _, err := d.GetAccessToken(ctx, "pod@example.invalid"); err == nil || !strings.Contains(err.Error(), "token refresh failed") {
		t.Fatalf("invalid refresh not surfaced: %v", err)
	}
	t.Logf("offline OAuth request methods/paths: %v", f.requests)
}
func TestPodsOAuthDeviceCancellation(t *testing.T) {
	f := &offlineOAuthTransport{mode: "cancel"}
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
	c, err := NewOAuthClient("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, ch, err := c.StartDeviceCodeFlow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case result := <-ch:
		if result.Error == nil {
			t.Fatal("cancel reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not complete")
	}
}

func TestPodsOAuthRequestsOnlyReadScope(t *testing.T) {
	f := &offlineOAuthTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = previous })
	c, err := NewReadOnlyOAuthClient("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, ch, err := c.StartDeviceCodeFlow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result := <-ch; result.Error != nil {
		t.Fatal(result.Error)
	}
	if _, err := c.GetAccessToken(ctx, "pod@example.invalid"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range f.scopes {
		if !strings.Contains(scope, "Mail.Read") || strings.Contains(scope, "Write") || strings.Contains(scope, "Send") || strings.Contains(scope, "Calendars") {
			t.Fatalf("excessive scopes: %s", scope)
		}
	}
}
