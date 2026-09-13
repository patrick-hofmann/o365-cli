package mail

import (
	"net/url"
	"strings"
	"testing"
)

func TestPodsHistoryBoundIsPreservedAcrossPages(t *testing.T) {
	request := ReadRequest{Operation: "messages", Folder: "inbox", Since: "2026-06-01T00:00:00Z"}
	endpoint, err := request.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(endpoint)
	if parsed.Query().Get("$filter") != "receivedDateTime ge 2026-06-01T00:00:00Z" {
		t.Fatalf("missing server-side boundary: %s", endpoint)
	}
	query := parsed.Query()
	query.Set("$skiptoken", "synthetic-next")
	parsed.RawQuery = query.Encode()
	request.Cursor = parsed.String()
	if _, err := request.Endpoint(); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"", "receivedDateTime ge 2020-01-01T00:00:00Z"} {
		query.Set("$filter", replacement)
		parsed.RawQuery = query.Encode()
		request.Cursor = parsed.String()
		if _, err := request.Endpoint(); err == nil {
			t.Fatal("historical scope expansion accepted")
		}
	}
	request.Since = ""
	request.Cursor = ""
	endpoint, err = request.Endpoint()
	if err != nil || strings.Contains(endpoint, "%24filter") {
		t.Fatalf("full-history compatibility: %s %v", endpoint, err)
	}
}
func TestPodsHistoryRejectsInvalidDatesAndNonMessageOperations(t *testing.T) {
	for _, since := range []string{"yesterday", "2026-02-30T00:00:00Z", "2026-06-01", "2026-06-01T00:00:00+01:00", "2026-06-01T00:00:00Z or true"} {
		if _, err := (ReadRequest{Operation: "messages", Folder: "inbox", Since: since}).Endpoint(); err == nil {
			t.Fatalf("invalid date accepted: %s", since)
		}
	}
	for _, operation := range []string{"folders", "attachments", "attachment"} {
		if _, err := (ReadRequest{Operation: operation, Folder: "inbox", Message: "m", Attachment: "a", Since: "2026-06-01T00:00:00Z"}).Endpoint(); err == nil {
			t.Fatalf("invalid operation accepted: %s", operation)
		}
	}
}
