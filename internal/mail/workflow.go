package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

const workflowFields = "id,changeKey,internetMessageId,conversationId,parentFolderId,subject,body,receivedDateTime,sentDateTime,from,sender,replyTo,toRecipients,ccRecipients,hasAttachments,flag,importance,internetMessageHeaders,categories,isRead"

// Delta pages keep bounded previews. A single-message read preserves the complete body;
// consumers must refetch truncated previews before deriving content versions or taking action.
// The page size is a Prefer header: Graph treats $top on a delta query as a limit on all results, not per page.
const workflowPageSize = "5"
const workflowBodyLimit = 24000

// Delta pages are limited to the folders a Pods mail synchronization observes.
var deltaFolders = map[string]bool{"inbox": true, "sentitems": true}

type WorkflowRequest struct {
	Operation   string   `json:"operation"`
	Account     string   `json:"account"`
	Message     string   `json:"message,omitempty"`
	Cursor      string   `json:"cursor,omitempty"`
	Version     string   `json:"version,omitempty"`
	Folder      string   `json:"folder,omitempty"`
	Destination string   `json:"destination,omitempty"`
	Categories  []string `json:"categories,omitempty"`
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

// A complete category list: Outlook stores categories as names, so the list replaces the message's set exactly.
func validCategories(categories []string) error {
	if categories == nil || len(categories) > 25 {
		return errors.New("a category list has at most 25 names")
	}
	seen := map[string]bool{}
	for _, name := range categories {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 255 || strings.ContainsAny(name, "\r\n\x00,") || seen[name] {
			return errors.New("category names are unique, trimmed and contain no line breaks or commas")
		}
		seen[name] = true
	}
	return nil
}

func sameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := map[string]bool{}
	for _, name := range left {
		seen[name] = true
	}
	for _, name := range right {
		if !seen[name] {
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
		folder := r.Folder
		if folder == "" {
			folder = "inbox"
		}
		if r.Message != "" || r.Version != "" || r.Destination != "" || r.Categories != nil || !deltaFolders[folder] {
			return "", errors.New("unexpected delta arguments")
		}
		path := base + "/mailFolders/" + folder + "/messages/delta"
		if r.Cursor == "" {
			return path + "?" + url.Values{"$select": {workflowFields}}.Encode(), nil
		}
		if len(r.Cursor) > 16384 || graph.ValidateEndpoint(r.Cursor) != nil {
			return "", errors.New("invalid delta cursor")
		}
		parsed, err := url.Parse(r.Cursor)
		expected, _ := url.Parse(path)
		// Graph writes the folder of its continuation links in OData key syntax: mailFolders('inbox').
		keyed, _ := url.Parse(base + "/mailFolders('" + folder + "')/messages/delta")
		if err != nil || (parsed.EscapedPath() != expected.EscapedPath() && parsed.EscapedPath() != keyed.EscapedPath()) {
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
			if key == "$select" && values[0] != workflowFields || key == "$top" && values[0] != workflowPageSize {
				return "", errors.New("delta cursor changed fields or page size")
			}
		}
		if query.Get("$skiptoken") == "" && query.Get("$deltatoken") == "" {
			return "", errors.New("missing delta continuation token")
		}
		return r.Cursor, nil
	case "categorize":
		if !validID(r.Message) || r.Cursor != "" || r.Folder != "" || r.Destination != "" || !validVersion(r.Version) || validCategories(r.Categories) != nil {
			return "", errors.New("categorize requires the message, its expected version and the complete category list")
		}
		return base + "/messages/" + url.PathEscape(r.Message), nil
	case "ensure-categories":
		if r.Message != "" || r.Cursor != "" || r.Version != "" || r.Folder != "" || r.Destination != "" || validCategories(r.Categories) != nil || len(r.Categories) == 0 {
			return "", errors.New("ensure-categories requires only the category names")
		}
		return base + "/outlook/masterCategories", nil
	case "read", "move":
		if !validID(r.Message) || r.Cursor != "" || r.Categories != nil {
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
	if r.Operation == "categorize" {
		if client.ReadOnly {
			return nil, errors.New("read-only connection cannot change categories")
		}
		method = http.MethodPatch
		body, _ = json.Marshal(map[string][]string{"categories": r.Categories})
	}
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
	if method == http.MethodGet {
		request.Header.Set("Prefer", `IdType="ImmutableId", outlook.body-content-type="text", odata.maxpagesize=`+workflowPageSize)
	} else {
		request.Header.Set("Prefer", `IdType="ImmutableId"`)
	}
	request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost || method == http.MethodPatch {
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
		for index, item := range page.Items {
			page.Items[index] = compactMessage(item, true)
		}
		result.Items = page.Items
		result.Next = page.Next
		result.Delta = page.Delta
	} else if r.Operation == "categorize" {
		var message struct {
			ID         string    `json:"id"`
			Version    string    `json:"changeKey"`
			Categories *[]string `json:"categories"`
		}
		// Outlook does not document If-Match as a guarantee; the result itself must show exactly the requested set.
		if json.Unmarshal(data, &message) != nil || message.ID != r.Message || message.Version == "" || message.Categories == nil || !sameSet(*message.Categories, r.Categories) {
			result.Reason = "category receipt does not show the requested categories"
			return result, nil
		}
		receipt, _ := json.Marshal(message)
		result.Items = []json.RawMessage{receipt}
		result.Receipt = receipt
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
		result.Items = []json.RawMessage{compactMessage(data, false)}
		if r.Operation == "move" {
			if message.Folder != destinationID || result.RequestID == "" {
				result.Reason = "move receipt has an unexpected destination"
				return result, nil
			}
			// The moved message carries its whole body; the receipt only needs the identity and folder.
			receipt, _ := json.Marshal(message)
			result.Items = []json.RawMessage{receipt}
			result.AfterID = message.ID
			result.Receipt = receipt
		}
	}
	result.Outcome = "confirmed"
	return result, nil
}

// compactMessage optionally caps previews and keeps only the List-Unsubscribe header.
func compactMessage(raw json.RawMessage, preview bool) json.RawMessage {
	var message map[string]any
	if json.Unmarshal(raw, &message) != nil {
		return raw
	}
	if body, ok := message["body"].(map[string]any); ok {
		if content, ok := body["content"].(string); ok && preview && len([]rune(content)) > workflowBodyLimit {
			body["content"] = string([]rune(content)[:workflowBodyLimit])
			message["bodyTruncated"] = true
		}
	}
	if headers, ok := message["internetMessageHeaders"].([]any); ok {
		kept := []any{}
		for _, header := range headers {
			if entry, ok := header.(map[string]any); ok && strings.EqualFold(stringValue(entry["name"]), "list-unsubscribe") {
				kept = append(kept, entry)
			}
		}
		message["internetMessageHeaders"] = kept
	}
	compact, err := json.Marshal(message)
	if err != nil {
		return raw
	}
	return compact
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

// EnsureCategories adds the names missing from the account's Outlook category list. Outlook shows a message
// category only when its name is in that list; existing entries and their colors stay untouched.
func EnsureCategories(ctx context.Context, client *graph.Client, r WorkflowRequest) (*WorkflowReply, error) {
	bounded := *client
	bounded.Context = ctx
	client = &bounded
	if r.Operation != "ensure-categories" {
		return nil, errors.New("unsupported workflow operation")
	}
	endpoint, err := r.endpoint()
	if err != nil {
		return nil, err
	}
	if client.ReadOnly {
		return nil, errors.New("read-only connection cannot change the category list")
	}
	result := &WorkflowReply{Protocol: "pods-mail/v1", Account: r.Account, Operation: r.Operation, Outcome: "notApplied"}
	listed := func() (map[string]bool, error) {
		data, err := client.DoRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		var list struct {
			Value []struct {
				Name string `json:"displayName"`
			} `json:"value"`
		}
		if json.Unmarshal(data, &list) != nil || list.Value == nil {
			return nil, errors.New("invalid category list")
		}
		names := map[string]bool{}
		for _, entry := range list.Value {
			names[strings.ToLower(entry.Name)] = true
		}
		return names, nil
	}
	present, err := listed()
	if err != nil {
		result.Reason = "category list unavailable: " + err.Error()
		return result, nil
	}
	created := []string{}
	for _, name := range r.Categories {
		if present[strings.ToLower(name)] {
			continue
		}
		body, _ := json.Marshal(map[string]string{"displayName": name, "color": presetColor(name)})
		if _, err := client.DoRequest(http.MethodPost, endpoint, body); err != nil {
			result.Outcome = "unknown"
			result.Reason = "adding " + name + " has no confirmed result"
			break
		}
		created = append(created, name)
	}
	present, err = listed()
	if err != nil {
		result.Outcome = "unknown"
		result.Reason = "category list could not be read back"
		return result, nil
	}
	for _, name := range r.Categories {
		if !present[strings.ToLower(name)] {
			result.Outcome = "unknown"
			if result.Reason == "" {
				result.Reason = "category list does not show " + name
			}
			return result, nil
		}
	}
	receipt, _ := json.Marshal(map[string][]string{"created": created, "categories": r.Categories})
	result.Items = []json.RawMessage{receipt}
	result.Receipt = receipt
	result.Outcome = "confirmed"
	result.Reason = ""
	return result, nil
}

// presetColor gives every name the same one of Outlook's 25 preset colors on every mailbox.
func presetColor(name string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	return fmt.Sprintf("preset%d", hash.Sum32()%25)
}
