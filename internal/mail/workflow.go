package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

const workflowFields = "id,changeKey,internetMessageId,conversationId,parentFolderId,subject,body,receivedDateTime,from,sender,replyTo,toRecipients,ccRecipients,hasAttachments,flag,importance,internetMessageHeaders"

type WorkflowRequest struct {
	Operation   string `json:"operation"`
	Account     string `json:"account"`
	Message     string `json:"message,omitempty"`
	Cursor      string `json:"cursor,omitempty"`
	Version     string `json:"version,omitempty"`
	Folder      string `json:"folder,omitempty"`
	Destination string `json:"destination,omitempty"`
}

type WorkflowReply struct {
	Protocol                string            `json:"protocol"`
	Account                 string            `json:"account"`
	Operation               string            `json:"operation"`
	Outcome                 string            `json:"outcome"`
	Items                   []json.RawMessage `json:"items"`
	Next                    string            `json:"next,omitempty"`
	Delta                   string            `json:"delta,omitempty"`
	BeforeID                string            `json:"beforeId,omitempty"`
	AfterID                 string            `json:"afterId,omitempty"`
	Receipt                 json.RawMessage   `json:"receipt,omitempty"`
	RequestID               string            `json:"requestId,omitempty"`
	Reason                  string            `json:"reason,omitempty"`
	ConditionalMoveVerified bool              `json:"conditionalMoveVerified"`
}

func (r WorkflowRequest) Validate() error { _, err := r.endpoint(); return err }

// A changeKey is standard base64 and may contain '/' and '+'; it only travels in the If-Match header.
func validVersion(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e || char == '"' {
			return false
		}
	}
	return true
}

// NotApplied is the receipt for a request refused before anything was sent to Microsoft Graph.
func NotApplied(r WorkflowRequest, err error) *WorkflowReply {
	return &WorkflowReply{Protocol: "pods-mail/v1", Account: r.Account, Operation: r.Operation, Outcome: "notApplied", Reason: err.Error()}
}

func (r WorkflowRequest) endpoint() (string, error) {
	address, err := mail.ParseAddress(r.Account)
	if err != nil || address.Address != r.Account || len(r.Account) > 320 {
		return "", errors.New("explicit account email required")
	}
	base := graph.GraphAPIBaseURL + "/users/" + url.PathEscape(r.Account)
	switch r.Operation {
	case "delta":
		if r.Message != "" || r.Version != "" || r.Folder != "" || r.Destination != "" {
			return "", errors.New("unexpected delta arguments")
		}
		path := base + "/mailFolders/inbox/messages/delta"
		if r.Cursor == "" {
			return path + "?" + url.Values{"$select": {workflowFields}, "$top": {"20"}}.Encode(), nil
		}
		if len(r.Cursor) > 16384 || graph.ValidateEndpoint(r.Cursor) != nil {
			return "", errors.New("invalid delta cursor")
		}
		parsed, err := url.Parse(r.Cursor)
		expected, _ := url.Parse(path)
		if err != nil || parsed.EscapedPath() != expected.EscapedPath() {
			return "", errors.New("delta cursor changed account or folder")
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return "", errors.New("invalid delta query")
		}
		for key, values := range query {
			if len(values) != 1 || !strings.Contains("|$select|$top|$skiptoken|$deltatoken|", "|"+key+"|") {
				return "", errors.New("unsupported delta query")
			}
			if key == "$select" && values[0] != workflowFields || key == "$top" && values[0] != "20" {
				return "", errors.New("delta cursor changed fields or page size")
			}
		}
		if query.Get("$skiptoken") == "" && query.Get("$deltatoken") == "" {
			return "", errors.New("missing delta continuation token")
		}
		return r.Cursor, nil
	case "read", "move":
		if !validID(r.Message) || r.Cursor != "" {
			return "", errors.New("explicit immutable message id required")
		}
		if r.Operation == "read" {
			if r.Version != "" || r.Folder != "" || r.Destination != "" {
				return "", errors.New("unexpected read arguments")
			}
			return base + "/messages/" + url.PathEscape(r.Message) + "?" + url.Values{"$select": {workflowFields}}.Encode(), nil
		}
		if !validVersion(r.Version) || !validID(r.Folder) || r.Destination != "archive" {
			return "", errors.New("move requires expected version, exact source folder and archive destination")
		}
		return base + "/mailFolders/" + url.PathEscape(r.Folder) + "/messages/" + url.PathEscape(r.Message) + "/move", nil
	default:
		return "", errors.New("unsupported workflow operation")
	}
}

// Graph documents folder-scoped move but does not promise an If-Match precondition.
// Consumers must not advertise verified conditional writes based on this header alone.
func Workflow(ctx context.Context, client *graph.Client, r WorkflowRequest) (*WorkflowReply, error) {
	bounded := *client
	bounded.Context = ctx
	client = &bounded
	endpoint, err := r.endpoint()
	if err != nil {
		return nil, err
	}
	result := &WorkflowReply{Protocol: "pods-mail/v1", Account: r.Account, Operation: r.Operation, Outcome: "notApplied", ConditionalMoveVerified: false}
	if err := ctx.Err(); err != nil {
		result.Reason = "cancelled before dispatch"
		return result, nil
	}
	destinationID := ""
	method := http.MethodGet
	var body []byte
	if r.Operation == "move" {
		if client.ReadOnly {
			return nil, errors.New("read-only connection cannot move messages")
		}
		before, err := Workflow(ctx, client, WorkflowRequest{Operation: "read", Account: r.Account, Message: r.Message})
		if err != nil || before.Outcome != "confirmed" || len(before.Items) != 1 {
			result.Reason = "preflight read failed"
			return result, nil
		}
		var current struct {
			ID      string `json:"id"`
			Version string `json:"changeKey"`
			Folder  string `json:"parentFolderId"`
		}
		if json.Unmarshal(before.Items[0], &current) != nil || current.ID != r.Message || current.Version != r.Version || current.Folder != r.Folder {
			result.Reason = "message changed or owner moved it"
			return result, nil
		}
		inbox, err := client.DoRequest(http.MethodGet, graph.GraphAPIBaseURL+"/users/"+url.PathEscape(r.Account)+"/mailFolders/inbox?$select=id", nil)
		var source struct {
			ID string `json:"id"`
		}
		if err != nil || json.Unmarshal(inbox, &source) != nil || source.ID != r.Folder {
			result.Reason = "source folder is not the account Inbox"
			return result, nil
		}
		archive, err := client.DoRequest(http.MethodGet, graph.GraphAPIBaseURL+"/users/"+url.PathEscape(r.Account)+"/mailFolders/archive?$select=id", nil)
		var destination struct {
			ID string `json:"id"`
		}
		if err != nil || json.Unmarshal(archive, &destination) != nil || !validID(destination.ID) {
			result.Reason = "archive folder could not be verified"
			return result, nil
		}
		destinationID = destination.ID
		method = http.MethodPost
		body = []byte(`{"destinationId":"archive"}`)
		result.BeforeID = r.Message
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.AccessToken)
	request.Header.Set("Prefer", `IdType="ImmutableId"`)
	request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost {
		request.Header.Set("If-Match", r.Version)
	}
	transport := *client.HttpClient
	transport.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return errors.New("workflow redirects are forbidden") }
	if err := ctx.Err(); err != nil {
		result.Reason = "cancelled before dispatch"
		return result, nil
	}
	response, err := transport.Do(request)
	result.Outcome = "unknown"
	if err != nil {
		result.Reason = "response unavailable after dispatch"
		return result, nil
	}
	defer response.Body.Close()
	result.RequestID = response.Header.Get("request-id")
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		result.Reason = "response truncated or oversized"
		return result, nil
	}
	if response.StatusCode != http.StatusOK && !(method == http.MethodPost && response.StatusCode == http.StatusCreated) {
		switch response.StatusCode {
		case 400, 401, 403, 404, 409, 412, 422:
			result.Outcome = "notApplied"
		}
		result.Reason = http.StatusText(response.StatusCode)
		return result, nil
	}
	if r.Operation == "delta" {
		var page struct {
			Items []json.RawMessage `json:"value"`
			Next  string            `json:"@odata.nextLink"`
			Delta string            `json:"@odata.deltaLink"`
		}
		if json.Unmarshal(data, &page) != nil || page.Items == nil || len(page.Items) > 100 || (page.Next == "") == (page.Delta == "") {
			result.Reason = "invalid or incomplete delta response"
			return result, nil
		}
		cursor := page.Next
		if cursor == "" {
			cursor = page.Delta
		}
		check := r
		check.Cursor = cursor
		if _, err := check.endpoint(); err != nil {
			result.Reason = "untrusted delta continuation"
			return result, nil
		}
		result.Items = page.Items
		result.Next = page.Next
		result.Delta = page.Delta
	} else {
		var message struct {
			ID      string `json:"id"`
			Version string `json:"changeKey"`
			Folder  string `json:"parentFolderId"`
		}
		if json.Unmarshal(data, &message) != nil || message.ID == "" || message.Version == "" || message.Folder == "" {
			result.Reason = "invalid message receipt"
			return result, nil
		}
		if r.Operation == "read" && message.ID != r.Message {
			result.Reason = "read returned a different identity"
			return result, nil
		}
		result.Items = []json.RawMessage{data}
		if r.Operation == "move" {
			if message.Folder != destinationID || result.RequestID == "" {
				result.Reason = "move receipt has an unexpected destination"
				return result, nil
			}
			result.AfterID = message.ID
			result.Receipt = data
		}
	}
	result.Outcome = "confirmed"
	return result, nil
}
