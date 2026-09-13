package mail

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestPodsFixtureReadPaginationAndAttachments(t *testing.T) {
	c := NewClient("synthetic-token")
	calls := 0
	c.HttpClient = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Host != "graph.microsoft.com" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/attachments"):
			return fixtureResponse(`{"value":[{"name":"a.txt","contentBytes":"b2s="}]}`), nil
		case strings.HasSuffix(r.URL.Path, "/messages/m1"):
			return fixtureResponse(`{"id":"m1","isRead":false,"body":{"content":"fixture"}}`), nil
		case r.URL.Query().Get("page") == "2":
			return fixtureResponse(`{"value":[{"id":"m2","isRead":false}]}`), nil
		default:
			return fixtureResponse(`{"value":[{"id":"m1","isRead":false}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?page=2"}`), nil
		}
	})}
	rows, err := c.ListEmails("inbox", 10, false, true, false)
	if err != nil || len(rows) != 2 {
		t.Fatalf("pagination: %v %v", rows, err)
	}
	row, err := c.GetEmail("inbox", "m1")
	if err != nil || !row.Unread {
		t.Fatalf("read state: %v %v", row, err)
	}
	dir := t.TempDir()
	att, err := c.GetAttachments("inbox", "m1", dir)
	if err != nil || len(att) != 1 {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(b) != "ok" || calls != 4 {
		t.Fatalf("attachment/calls: %q %v %d", b, err, calls)
	}
}
func TestPodsSecurityAttachmentMustStayWithinDownloadDirectory(t *testing.T) {
	c := NewClient("synthetic-token")
	c.HttpClient = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(`{"value":[{"name":"../escaped.txt","contentBytes":"b2s="}]}`), nil
	})}
	root := t.TempDir()
	_, err := c.GetAttachments("inbox", "m1", filepath.Join(root, "download"))
	if _, statErr := os.Stat(filepath.Join(root, "escaped.txt")); statErr == nil {
		t.Fatal("SECURITY FINDING: synthetic attachment escaped assigned download directory")
	}
	if err == nil {
		t.Fatal("unsafe filename was not rejected")
	}
}
func TestPodsSecurityNextLinkMustNotDeliverBearerToForeignOrigin(t *testing.T) {
	c := NewClient("synthetic-token")
	c.HttpClient = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "unassigned.invalid" {
			if r.Header.Get("Authorization") == "Bearer synthetic-token" {
				t.Error("SECURITY FINDING: fake bearer attached to unassigned pagination origin; intercepted without network")
			}
			return fixtureResponse(`{"value":[]}`), nil
		}
		return fixtureResponse(`{"value":[],"@odata.nextLink":"https://unassigned.invalid/page"}`), nil
	})}
	_, _ = c.ListEmails("inbox", 10, false, false, false)
}

func TestPodsAttachmentDoesNotOverwriteSymlinkOrExistingFile(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "download")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "a.txt")); err != nil {
		t.Fatal(err)
	}
	c := NewClient("synthetic-token")
	c.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(`{"value":[{"name":"a.txt","contentBytes":"b2s="}]}`), nil
	})
	if _, err := c.GetAttachments("inbox", "m1", directory); err == nil {
		t.Fatal("symlink overwrite accepted")
	}
	value, err := os.ReadFile(outside)
	if err != nil || string(value) != "keep" {
		t.Fatal("outside file changed")
	}
}
func TestPodsFolderInventoryIncludesAllChildPagesAndRejectsPartialReads(t *testing.T) {
	c := NewClient("synthetic-token")
	fail := false
	c.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "childFolders") {
			if fail {
				return nil, fmt.Errorf("synthetic child page failure")
			}
			if r.URL.Query().Get("page") == "2" {
				return fixtureResponse(`{"value":[{"id":"child2","displayName":"Second"}]}`), nil
			}
			return fixtureResponse(`{"value":[{"id":"child1","displayName":"First"}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/mailFolders/parent/childFolders?page=2"}`), nil
		}
		return fixtureResponse(`{"value":[{"id":"parent","displayName":"Rules","childFolderCount":2}]}`), nil
	})
	folders, err := c.ListFolders()
	if err != nil || len(folders) != 3 || folders[2].Name != "Rules/Second" {
		t.Fatalf("inventory: %v %v", folders, err)
	}
	fail = true
	if _, err := c.ListFolders(); err == nil {
		t.Fatal("partial inventory returned success")
	}
}
