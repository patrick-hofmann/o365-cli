package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

func TestArchiveLabelledMovesOnlyLabelledUnchangedInboxMail(t *testing.T) {
	var message map[string]any
	moves := 0
	client := graph.NewClient("synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/mailFolders/inbox"):
			return fixtureResponse(`{"id":"inbox-id"}`), nil
		case strings.HasSuffix(r.URL.Path, "/mailFolders/archive"):
			return fixtureResponse(`{"id":"archive-id"}`), nil
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/move"):
			moves++
			message["parentFolderId"] = "archive-id"
			response := fixtureResponse(`{"id":"immutable-before","changeKey":"v2","parentFolderId":"archive-id"}`)
			response.Header.Set("request-id", "req-1")
			return response, nil
		}
		raw, _ := json.Marshal(message)
		return fixtureResponse(string(raw)), nil
	})
	reset := func(categories ...string) {
		json.Unmarshal([]byte(labelFixture), &message)
		message["categories"] = categories
	}
	reset("Unrelated")
	snapshot, err := ReadLabelSnapshot(context.Background(), client, "pod@example.invalid", "immutable-before")
	if err != nil {
		t.Fatal(err)
	}
	version, root := snapshot.AssessmentVersion, archiveDirectory(t)
	archive := func() *WorkflowReply {
		reply, err := ArchiveLabelled(context.Background(), client, root, "pod@example.invalid", "immutable-before", "Rechnung: abgelegt", version)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	if reply := archive(); reply.Outcome != "notApplied" || moves != 0 {
		t.Fatalf("archived without the required label: %+v", reply)
	}
	reset("Unrelated", "Rechnung: abgelegt")
	message["body"] = map[string]any{"content": "Changed after the decision"}
	if reply := archive(); reply.Outcome != "deferred" || moves != 0 {
		t.Fatalf("archived changed content: %+v", reply)
	}
	reset("Unrelated", "Rechnung: abgelegt")
	if reply := archive(); reply.Outcome != "confirmed" || reply.Operation != "archive-labelled" || moves != 1 {
		t.Fatalf("labelled mail not archived: %+v", reply)
	}
	if reply := archive(); reply.Outcome != "skipped" || moves != 1 {
		t.Fatalf("repeat moved again: %+v", reply)
	}
}
