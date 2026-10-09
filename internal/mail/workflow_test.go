package mail

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

func workflowClient(t *testing.T, post func(*http.Request) (*http.Response, error)) (*graph.Client, *int) {
	t.Helper()
	count := 0
	client := graph.NewClient("synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1.0/users/pod@example.invalid/mailFolders/inbox" {
			return fixtureResponse(`{"id":"inbox-id"}`), nil
		}
		if strings.Contains(r.URL.Path, "/mailFolders/archive") {
			return fixtureResponse(`{"id":"archive-id"}`), nil
		}
		if r.Header.Get("Prefer") != `IdType="ImmutableId"` {
			t.Fatal("missing immutable identity header")
		}
		if !strings.Contains(r.URL.Path, "/users/pod@example.invalid/") {
			t.Fatal("wrong account path")
		}
		if r.Method == "GET" {
			return fixtureResponse(`{"id":"immutable-before","changeKey":"v1","parentFolderId":"inbox-id"}`), nil
		}
		count++
		if r.Header.Get("If-Match") != "v1" || !strings.HasSuffix(r.URL.Path, "/mailFolders/inbox-id/messages/immutable-before/move") {
			t.Fatal("missing expected version or exact source folder")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"destinationId":"archive"}` {
			t.Fatal("wrong move body")
		}
		return post(r)
	})
	return client, &count
}
func moveRequest() WorkflowRequest {
	return WorkflowRequest{Operation: "move", Account: "pod@example.invalid", Message: "immutable-before", Version: "v1", Folder: "inbox-id", Destination: "archive"}
}
func TestWorkflowMoveRetainsChangedIDAndOneAttemptReceipt(t *testing.T) {
	client, count := workflowClient(t, func(_ *http.Request) (*http.Response, error) {
		r := fixtureResponse(`{"id":"immutable-after","changeKey":"v2","parentFolderId":"archive-id"}`)
		r.StatusCode = 201
		r.Header = http.Header{"Request-Id": {"receipt-1"}}
		return r, nil
	})
	result, err := Workflow(context.Background(), client, moveRequest())
	if err != nil || result.Outcome != "confirmed" || result.BeforeID != "immutable-before" || result.AfterID != "immutable-after" || *count != 1 || result.ConditionalMoveVerified {
		t.Fatalf("invalid result: %+v %v attempts=%d", result, err, *count)
	}
}

type brokenBody struct{}

func (brokenBody) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenBody) Close() error               { return nil }
func TestWorkflowMoveNeverRetriesUnknownResponses(t *testing.T) {
	for _, scenario := range []string{"timeout", "truncated", "server-error", "invalid-json"} {
		t.Run(scenario, func(t *testing.T) {
			client, count := workflowClient(t, func(_ *http.Request) (*http.Response, error) {
				switch scenario {
				case "timeout":
					return nil, errors.New("synthetic timeout")
				case "truncated":
					return &http.Response{StatusCode: 201, Body: brokenBody{}}, nil
				case "server-error":
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}, nil
				default:
					return fixtureResponse("{"), nil
				}
			})
			result, err := Workflow(context.Background(), client, moveRequest())
			if err != nil || result.Outcome != "unknown" || *count != 1 {
				t.Fatalf("must remain unknown after one dispatch: %+v %v %d", result, err, *count)
			}
		})
	}
}
func TestWorkflowRejectsOwnerChangesBeforeDispatch(t *testing.T) {
	client, count := workflowClient(t, func(_ *http.Request) (*http.Response, error) { t.Fatal("must not dispatch move"); return nil, nil })
	for _, field := range []string{"version", "folder"} {
		r := moveRequest()
		if field == "version" {
			r.Version = "new-version"
		} else {
			r.Folder = "owner-moved-folder"
		}
		result, err := Workflow(context.Background(), client, r)
		if err != nil || result.Outcome != "notApplied" || *count != 0 {
			t.Fatalf("changed message moved: %+v %v", result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, _ := Workflow(ctx, client, moveRequest())
	if result.Outcome != "notApplied" {
		t.Fatal(result)
	}
}
func TestWorkflowBindsDeltaPagesToTheExactAccountAndInbox(t *testing.T) {
	cursor := "https://graph.microsoft.com/v1.0/users/pod@example.invalid/mailFolders/inbox/messages/delta?$deltatoken=sealed"
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Prefer") != `IdType="ImmutableId"` || r.URL.Query().Get("$top") != "20" {
			t.Fatal("unexpected read contract")
		}
		return fixtureResponse(`{"value":[],"@odata.deltaLink":"` + cursor + `"}`), nil
	})
	result, err := Workflow(context.Background(), client, WorkflowRequest{Operation: "delta", Account: "pod@example.invalid"})
	if err != nil || result.Delta != cursor || result.Outcome != "confirmed" {
		t.Fatalf("delta result: %+v %v", result, err)
	}
	for _, invalid := range []string{strings.Replace(cursor, "pod@example.invalid", "other@example.invalid", 1), strings.Replace(cursor, "inbox", "sentitems", 1), cursor + "&$expand=attachments", cursor + "&$top=1000"} {
		if _, err := (WorkflowRequest{Operation: "delta", Account: "pod@example.invalid", Cursor: invalid}).endpoint(); err == nil {
			t.Fatal("foreign cursor accepted")
		}
	}
	if _, err := Workflow(context.Background(), client, moveRequest()); err == nil {
		t.Fatal("read-only move accepted")
	}
}

func TestWorkflowMoveAcceptsBase64ChangeKeys(t *testing.T) {
	request := moveRequest()
	request.Version = "CQAAABYAAACKHHSq3O/eT5cOYQR9J1iW+AkscXMX"
	if err := request.Validate(); err != nil {
		t.Fatalf("base64 changeKey refused: %v", err)
	}
	for _, version := range []string{"", "with space", "quote\"", "line\nbreak"} {
		request.Version = version
		if request.Validate() == nil {
			t.Fatalf("unsafe changeKey %q accepted", version)
		}
	}
}

func TestNotAppliedReceiptNamesTheRefusal(t *testing.T) {
	reply := NotApplied(moveRequest(), errors.New("refused before dispatch"))
	if reply.Outcome != "notApplied" || reply.Protocol != "pods-mail/v1" || reply.Operation != "move" || reply.Reason != "refused before dispatch" {
		t.Fatalf("unexpected receipt: %+v", reply)
	}
}

func TestWorkflowMoveReceiptLeavesTheMessageBodyOut(t *testing.T) {
	client, _ := workflowClient(t, func(_ *http.Request) (*http.Response, error) {
		r := fixtureResponse(`{"id":"immutable-after","changeKey":"v2","parentFolderId":"archive-id","body":{"content":"` + strings.Repeat("x", 300000) + `"}}`)
		r.StatusCode = 201
		r.Header = http.Header{"Request-Id": {"receipt-1"}}
		return r, nil
	})
	result, err := Workflow(context.Background(), client, moveRequest())
	if err != nil || result.Outcome != "confirmed" || len(result.Receipt) > 200 || len(result.Items) != 1 || len(result.Items[0]) > 200 || !strings.Contains(string(result.Receipt), "archive-id") {
		t.Fatalf("receipt not compact: outcome=%s receipt=%d bytes err=%v", result.Outcome, len(result.Receipt), err)
	}
}
