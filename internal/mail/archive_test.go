package mail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

const archiveFixture = `{"id":"immutable-before","changeKey":"v1","parentFolderId":"inbox-id","from":{"emailAddress":{"address":"Sender@Example.invalid"}},"subject":"Reviewed message","body":{"content":"complete body"},"receivedDateTime":"2026-10-15T12:00:00Z","hasAttachments":false}`

func archiveFixtureClient(t *testing.T, raw string, outcome string) (*graph.Client, *int) {
	t.Helper()
	count := 0
	client := graph.NewClient("synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.Path, "/v1.0/users/pod@example.invalid/") {
			t.Fatal("wrong account")
		}
		if r.Method == "GET" {
			if strings.Contains(r.URL.Path, "/mailFolders/archive") {
				return fixtureResponse(`{"id":"archive-id"}`), nil
			}
			if strings.Contains(r.URL.Path, "/mailFolders/inbox") {
				return fixtureResponse(`{"id":"inbox-id"}`), nil
			}
			return fixtureResponse(raw), nil
		}
		count++
		if r.Header.Get("If-Match") != "v1" || !strings.HasSuffix(r.URL.Path, "/mailFolders/inbox-id/messages/immutable-before/move") {
			t.Fatal("missing exact provider preflight binding")
		}
		if outcome == "timeout" {
			return nil, errors.New("synthetic lost reply")
		}
		folder := "archive-id"
		if outcome == "wrong-folder" {
			folder = "other-folder"
		}
		response := fixtureResponse(`{"id":"immutable-after","changeKey":"v2","parentFolderId":"` + folder + `"}`)
		response.StatusCode = 201
		response.Header = http.Header{"Request-Id": {"synthetic-receipt"}}
		return response, nil
	})
	return client, &count
}
func archiveReview(t *testing.T) ArchiveRequest {
	t.Helper()
	request, err := ReviewArchive(json.RawMessage(archiveFixture), "pod@example.invalid", "operation-1")
	if err != nil {
		t.Fatal(err)
	}
	return *request
}
func archiveDirectory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return real
}
func TestProtectedArchiveKeepsProviderReceiptAndNeverReplays(t *testing.T) {
	request := archiveReview(t)
	root := archiveDirectory(t)
	client, count := archiveFixtureClient(t, archiveFixture, "confirmed")
	for i := 0; i < 2; i++ {
		reply, err := ProtectedArchive(context.Background(), client, root, request)
		if err != nil || reply.Outcome != "archived" || reply.Provider.RequestID != "synthetic-receipt" || reply.Provider.BeforeID != request.Message || reply.Provider.AfterID != "immutable-after" {
			t.Fatalf("bad result %+v %v", reply, err)
		}
	}
	if *count != 1 {
		t.Fatalf("replayed %d writes", *count)
	}
	request.OperationID = "other-pod-operation"
	reply, err := ProtectedArchive(context.Background(), client, root, request)
	if err != nil || reply.Outcome != "archived" || *count != 1 {
		t.Fatalf("another Pod replayed %+v %v %d", reply, err, *count)
	}
}
func TestProtectedArchiveRefusesChangedReviewedObject(t *testing.T) {
	for _, field := range []string{"id", "folder", "content", "sender", "subject", "destination", "account", "version"} {
		t.Run(field, func(t *testing.T) {
			request := archiveReview(t)
			raw := archiveFixture
			switch field {
			case "id":
				raw = strings.Replace(raw, "immutable-before", "another-message", 1)
			case "folder":
				raw = strings.Replace(raw, "inbox-id", "another-folder", 1)
			case "content":
				raw = strings.Replace(raw, "complete body", "changed body", 1)
			case "sender":
				request.Sender = "different@example.invalid"
			case "subject":
				request.Subject = "Different subject"
			case "destination":
				request.Destination = "deleteditems"
			case "account":
				request.Account = "not an email"
			case "version":
				request.ContentVersion = "content:legacy"
			}
			client, count := archiveFixtureClient(t, raw, "confirmed")
			reply, err := ProtectedArchive(context.Background(), client, archiveDirectory(t), request)
			if *count != 0 || err == nil && reply.Outcome != "notApplied" {
				t.Fatalf("changed review executed %+v %v %d", reply, err, *count)
			}
		})
	}
}
func TestProtectedArchiveHoldsUnknownAcrossPodsAndVersions(t *testing.T) {
	for _, scenario := range []string{"timeout", "wrong-folder"} {
		t.Run(scenario, func(t *testing.T) {
			request := archiveReview(t)
			root := archiveDirectory(t)
			client, count := archiveFixtureClient(t, archiveFixture, scenario)
			reply, err := ProtectedArchive(context.Background(), client, root, request)
			if err != nil || reply.Outcome != "unknown" || *count != 1 {
				t.Fatalf("lost ambiguous result %+v %v", reply, err)
			}
			request.OperationID = "other-pod-operation"
			request.ContentVersion = "content-v2:" + strings.Repeat("a", 64)
			if _, err := ProtectedArchive(context.Background(), client, root, request); err == nil || !strings.Contains(err.Error(), "reconciliation") || *count != 1 {
				t.Fatalf("replayed unknown result %v %d", err, *count)
			}
		})
	}
}
func TestProtectedArchiveRecoversIntentAndSerializesConcurrentCalls(t *testing.T) {
	request := archiveReview(t)
	root := archiveDirectory(t)
	journal, err := openArchiveJournal(root, request.Account)
	if err != nil {
		t.Fatal(err)
	}
	client, count := archiveFixtureClient(t, archiveFixture, "confirmed")
	if _, err := ProtectedArchive(context.Background(), client, root, request); err == nil || *count != 0 {
		t.Fatal("concurrent call passed the mailbox lock")
	}
	if err := journal.save(&ArchiveReceipt{Protocol: "o365-archive/v1", Request: request, Outcome: "unknown"}); err != nil {
		t.Fatal(err)
	}
	journal.close()
	reply, err := ProtectedArchive(context.Background(), client, root, request)
	if err != nil || reply.Outcome != "unknown" || *count != 0 {
		t.Fatal("reopened intent executed")
	}
}
func TestArchiveContentVersionIgnoresReadAndCategoryChanges(t *testing.T) {
	original, err := ArchiveContentVersion(json.RawMessage(archiveFixture))
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.TrimSuffix(archiveFixture, "}") + `,"isRead":true,"categories":["Reviewed"],"flag":{"flagStatus":"flagged"}}`
	changed = strings.Replace(changed, `"changeKey":"v1"`, `"changeKey":"v2"`, 1)
	version, err := ArchiveContentVersion(json.RawMessage(changed))
	if err != nil || version != original {
		t.Fatalf("non-content change altered version %s %v", version, err)
	}
}
