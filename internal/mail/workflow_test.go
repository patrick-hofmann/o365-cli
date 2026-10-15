package mail

import (
	"context"
	"encoding/json"
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
		if !strings.Contains(r.Header.Get("Prefer"), `IdType="ImmutableId"`) {
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
		if r.Header.Get("Prefer") != `IdType="ImmutableId", outlook.body-content-type="text", odata.maxpagesize=5` || r.URL.Query().Has("$top") {
			t.Fatal("unexpected read contract")
		}
		return fixtureResponse(`{"value":[],"@odata.deltaLink":"` + cursor + `"}`), nil
	})
	result, err := Workflow(context.Background(), client, WorkflowRequest{Operation: "delta", Account: "pod@example.invalid"})
	if err != nil || result.Delta != cursor || result.Outcome != "confirmed" {
		t.Fatalf("delta result: %+v %v", result, err)
	}
	for _, invalid := range []string{strings.Replace(cursor, "pod@example.invalid", "other@example.invalid", 1), strings.Replace(cursor, "inbox", "sentitems", 1), cursor + "&$expand=attachments", cursor + "&$top=1000", cursor + "&$top=20"} {
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

func TestWorkflowDeltaReadsTheSentFolderOnlyWithItsOwnCursor(t *testing.T) {
	sent := "https://graph.microsoft.com/v1.0/users/pod@example.invalid/mailFolders/sentitems/messages/delta?$deltatoken=sealed"
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/users/pod@example.invalid/mailFolders/sentitems/messages/delta") || !strings.Contains(r.URL.Query().Get("$select"), "categories") {
			t.Fatalf("unexpected sent delta request %s", r.URL)
		}
		return fixtureResponse(`{"value":[{"id":"m1","@removed":{"reason":"deleted"}}],"@odata.deltaLink":"` + sent + `"}`), nil
	})
	result, err := Workflow(context.Background(), client, WorkflowRequest{Operation: "delta", Account: "pod@example.invalid", Folder: "sentitems"})
	if err != nil || result.Outcome != "confirmed" || result.Delta != sent || len(result.Items) != 1 {
		t.Fatalf("sent delta: %+v %v", result, err)
	}
	// Graph names the folder of its continuation links in OData key syntax; only the same folder may continue.
	graphStyle := "https://graph.microsoft.com/v1.0/users/pod@example.invalid/mailFolders('sentitems')/messages/delta?$skiptoken=next"
	if _, err := (WorkflowRequest{Operation: "delta", Account: "pod@example.invalid", Folder: "sentitems", Cursor: graphStyle}).endpoint(); err != nil {
		t.Fatalf("Graph continuation refused: %v", err)
	}
	if _, err := (WorkflowRequest{Operation: "delta", Account: "pod@example.invalid", Cursor: graphStyle}).endpoint(); err == nil {
		t.Fatal("a sent continuation continued the inbox")
	}
	inbox := strings.Replace(sent, "sentitems", "inbox", 1)
	if _, err := (WorkflowRequest{Operation: "delta", Account: "pod@example.invalid", Folder: "sentitems", Cursor: inbox}).endpoint(); err == nil {
		t.Fatal("an inbox cursor continued the sent folder")
	}
	if _, err := (WorkflowRequest{Operation: "delta", Account: "pod@example.invalid", Folder: "archive"}).endpoint(); err == nil {
		t.Fatal("an unlisted folder was accepted")
	}
}

func categorizeRequest(categories ...string) WorkflowRequest {
	return WorkflowRequest{Operation: "categorize", Account: "pod@example.invalid", Message: "immutable-1", Version: "CQAAABYAAAA/v1+", Categories: categories}
}

func TestWorkflowCategorizeWritesTheCompleteListOnceAndVerifiesIt(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
		outcome        string
	}{
		{"applied", `{"id":"immutable-1","changeKey":"v2","categories":["Behalten","Newsletter"]}`, 200, "confirmed"},
		{"changed meanwhile", ``, 412, "notApplied"},
		{"different result", `{"id":"immutable-1","changeKey":"v2","categories":["Newsletter"]}`, 200, "unknown"},
	} {
		calls := 0
		client := graph.NewClient("synthetic-secret")
		client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			body, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPatch || r.URL.Path != "/v1.0/users/pod@example.invalid/messages/immutable-1" || r.Header.Get("If-Match") != "CQAAABYAAAA/v1+" || string(body) != `{"categories":["Newsletter","Behalten"]}` {
				t.Fatalf("%s: unexpected request %s %s %s", tc.name, r.Method, r.URL.Path, body)
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.response)), Header: http.Header{}}, nil
		})
		result, err := Workflow(context.Background(), client, categorizeRequest("Newsletter", "Behalten"))
		if err != nil || result.Outcome != tc.outcome || calls != 1 {
			t.Fatalf("%s: %+v %v calls=%d", tc.name, result, err, calls)
		}
	}
}

func TestWorkflowCategorizeRefusesIncompleteRequestsBeforeDispatch(t *testing.T) {
	for _, request := range []WorkflowRequest{categorizeRequest("a", "a"), categorizeRequest("a,b"), categorizeRequest(" padded"), {Operation: "categorize", Account: "pod@example.invalid", Message: "immutable-1", Version: "v1"}, func() WorkflowRequest { r := categorizeRequest("a"); r.Version = ""; return r }()} {
		if err := request.Validate(); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	if _, err := Workflow(context.Background(), graph.NewReadOnlyClient(context.Background(), "synthetic-secret"), categorizeRequest("a")); err == nil {
		t.Fatal("read-only connection changed categories")
	}
}

func TestWorkflowPagesStayBelowThePodsToolOutputLimit(t *testing.T) {
	long := strings.Repeat("x", 30000)
	cursor := "https://graph.microsoft.com/v1.0/users/pod@example.invalid/mailFolders/inbox/messages/delta?$deltatoken=sealed"
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(`{"value":[{"id":"m1","changeKey":"v1","body":{"contentType":"text","content":"` + long + `"},"internetMessageHeaders":[{"name":"Received","value":"hop"},{"name":"List-Unsubscribe","value":"<mailto:u@example.invalid>"}]},{"id":"m2","@removed":{"reason":"deleted"}}],"@odata.deltaLink":"` + cursor + `"}`), nil
	})
	result, err := Workflow(context.Background(), client, WorkflowRequest{Operation: "delta", Account: "pod@example.invalid"})
	if err != nil || result.Outcome != "confirmed" || len(result.Items) != 2 {
		t.Fatalf("delta result: %+v %v", result, err)
	}
	var message struct {
		Body      struct{ Content string } `json:"body"`
		Truncated bool                     `json:"bodyTruncated"`
		Headers   []struct{ Name string }  `json:"internetMessageHeaders"`
	}
	if json.Unmarshal(result.Items[0], &message) != nil || len(message.Body.Content) != workflowBodyLimit || !message.Truncated || len(message.Headers) != 1 || message.Headers[0].Name != "List-Unsubscribe" {
		t.Fatalf("message was not reduced: %s", string(result.Items[0])[:200])
	}
	if !strings.Contains(string(result.Items[1]), "@removed") {
		t.Fatal("removed entry changed")
	}
}

func TestEnsureCategoriesAddsOnlyMissingNamesAndVerifiesTheList(t *testing.T) {
	listed := []string{`{"displayName":"behalten","color":"preset3"}`}
	posts := []string{}
	client := graph.NewClient("synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1.0/users/pod@example.invalid/outlook/masterCategories" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			posts = append(posts, string(body))
			listed = append(listed, string(body))
			return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(string(body))), Header: http.Header{}}, nil
		}
		return fixtureResponse(`{"value":[` + strings.Join(listed, ",") + `]}`), nil
	})
	request := WorkflowRequest{Operation: "ensure-categories", Account: "pod@example.invalid", Categories: []string{"Behalten", "LiCare"}}
	result, err := EnsureCategories(context.Background(), client, request)
	if err != nil || result.Outcome != "confirmed" || len(posts) != 1 || !strings.Contains(posts[0], `"displayName":"LiCare"`) || !strings.Contains(posts[0], `"color":"preset`) {
		t.Fatalf("%+v %v %v", result, err, posts)
	}
	// A second run finds every name and changes nothing.
	if result, err = EnsureCategories(context.Background(), client, request); err != nil || result.Outcome != "confirmed" || len(posts) != 1 {
		t.Fatalf("second run %+v %v %v", result, err, posts)
	}
	if _, err := EnsureCategories(context.Background(), graph.NewReadOnlyClient(context.Background(), "synthetic-secret"), request); err == nil {
		t.Fatal("read-only connection changed the category list")
	}
	for _, bad := range []WorkflowRequest{{Operation: "ensure-categories", Account: "pod@example.invalid"}, {Operation: "ensure-categories", Account: "pod@example.invalid", Message: "x", Categories: []string{"a"}}} {
		if bad.Validate() == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestWorkflowReadPreservesCompleteLongBody(t *testing.T) {
	content := strings.Repeat("long text ", 6000) + "INVOICE AT THE END"
	raw, _ := json.Marshal(map[string]any{"id": "immutable-before", "changeKey": "v1", "parentFolderId": "inbox-id", "body": map[string]string{"contentType": "text", "content": content}})
	client := graph.NewClient("synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Prefer"), `outlook.body-content-type="text"`) {
			t.Fatal("unexpected request")
		}
		return fixtureResponse(string(raw)), nil
	})
	result, err := Workflow(context.Background(), client, WorkflowRequest{Operation: "read", Account: "pod@example.invalid", Message: "immutable-before"})
	if err != nil || result.Outcome != "confirmed" || len(result.Items) != 1 {
		t.Fatalf("read failed: %v %#v", err, result)
	}
	var message struct {
		Body      struct{ Content string } `json:"body"`
		Truncated bool                     `json:"bodyTruncated"`
	}
	if json.Unmarshal(result.Items[0], &message) != nil || message.Body.Content != content || message.Truncated {
		t.Fatal("single-message read lost content")
	}
}
