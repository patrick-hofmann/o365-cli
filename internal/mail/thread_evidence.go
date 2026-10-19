package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

const threadPageSize = "5"

type ThreadEvidence struct {
	Protocol       string            `json:"protocol"`
	Account        string            `json:"account"`
	Message        string            `json:"message"`
	ContentVersion string            `json:"contentVersion"`
	Conversation   string            `json:"conversationId"`
	SentFolder     string            `json:"sentFolder"`
	Items          []json.RawMessage `json:"items"`
	Next           string            `json:"next,omitempty"`
	Complete       bool              `json:"complete"`
}

type threadCursor struct {
	Message string `json:"message"`
	Version string `json:"version"`
	Next    string `json:"next"`
}

func threadEndpoint(account, conversation string) string {
	return graph.GraphAPIBaseURL + "/users/" + url.PathEscape(account) + "/messages?" + url.Values{"$filter": {"conversationId eq '" + strings.ReplaceAll(conversation, "'", "''") + "'"}, "$select": {workflowFields}, "$top": {threadPageSize}}.Encode()
}
func validateThreadNext(candidate, expected string) error {
	if len(candidate) > 16384 || graph.ValidateEndpoint(candidate) != nil {
		return errors.New("invalid thread continuation")
	}
	parsed, err := url.Parse(candidate)
	reference, _ := url.Parse(expected)
	if err != nil || parsed.EscapedPath() != reference.EscapedPath() {
		return errors.New("thread continuation changed account or collection")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return errors.New("invalid thread continuation query")
	}
	original := reference.Query()
	for key, values := range query {
		if len(values) != 1 {
			return errors.New("duplicate thread continuation option")
		}
		if key == "$skiptoken" || key == "$skip" {
			continue
		}
		if original.Get(key) == "" || values[0] != original.Get(key) {
			return errors.New("thread continuation changed assessment fields or conversation")
		}
	}
	for _, key := range []string{"$filter", "$select", "$top"} {
		if query.Get(key) != original.Get(key) {
			return errors.New("thread continuation omitted its conversation boundary")
		}
	}
	if query.Get("$skiptoken") == "" && query.Get("$skip") == "" {
		return errors.New("thread continuation lacks a page token")
	}
	return nil
}

func ReadThreadEvidence(ctx context.Context, client *graph.Client, account, message, cursor string) (*ThreadEvidence, error) {
	snapshot, err := ReadLabelSnapshot(ctx, client, account, message)
	if err != nil {
		return nil, err
	}
	var origin struct {
		Conversation string `json:"conversationId"`
	}
	if json.Unmarshal(snapshot.Message, &origin) != nil || origin.Conversation == "" || len(origin.Conversation) > 2048 {
		return nil, errors.New("message lacks a bounded conversation identity")
	}
	endpoint := threadEndpoint(account, origin.Conversation)
	if cursor != "" {
		if len(cursor) > 32768 {
			return nil, errors.New("thread cursor is too large")
		}
		data, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		var saved threadCursor
		if decodeErr != nil || json.Unmarshal(data, &saved) != nil || saved.Message != message || saved.Version != snapshot.ContentVersion || validateThreadNext(saved.Next, endpoint) != nil {
			return nil, errors.New("thread cursor does not match the unchanged account message")
		}
		endpoint = saved.Next
	}
	bounded := *client
	bounded.Context = ctx
	sent, err := bounded.DoRequest(http.MethodGet, graph.GraphAPIBaseURL+"/users/"+url.PathEscape(account)+"/mailFolders/sentitems?$select=id", nil)
	var folder struct {
		ID string `json:"id"`
	}
	if err != nil || json.Unmarshal(sent, &folder) != nil || !validID(folder.ID) {
		return nil, errors.New("account Sent folder could not be verified")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+client.AccessToken)
	request.Header.Set("Prefer", `IdType="ImmutableId", outlook.body-content-type="text", odata.maxpagesize=`+threadPageSize)
	transport := *client.HttpClient
	transport.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return errors.New("thread redirects are forbidden") }
	response, err := transport.Do(request)
	if err != nil {
		return nil, errors.New("thread page could not be read")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 || response.StatusCode != http.StatusOK {
		return nil, errors.New("thread page is unavailable, oversized or incomplete")
	}
	var page struct {
		Items []json.RawMessage `json:"value"`
		Next  string            `json:"@odata.nextLink"`
	}
	if json.Unmarshal(data, &page) != nil || page.Items == nil || len(page.Items) > 100 {
		return nil, errors.New("invalid thread page")
	}
	result := &ThreadEvidence{Protocol: "o365-thread/v1", Account: account, Message: message, ContentVersion: snapshot.ContentVersion, Conversation: origin.Conversation, SentFolder: folder.ID, Items: []json.RawMessage{}, Complete: page.Next == ""}
	for _, raw := range page.Items {
		raw = compactMessage(raw, false)
		if _, err := parseLabelMessage(raw); err != nil {
			return nil, errors.New("thread message lacks complete assessment data")
		}
		var item struct {
			Conversation string `json:"conversationId"`
			Draft        *bool  `json:"isDraft"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Conversation != origin.Conversation || item.Draft == nil {
			return nil, errors.New("thread response changed conversation or omitted draft status")
		}
		result.Items = append(result.Items, raw)
	}
	if page.Next != "" {
		if err := validateThreadNext(page.Next, threadEndpoint(account, origin.Conversation)); err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(threadCursor{Message: message, Version: snapshot.ContentVersion, Next: page.Next})
		result.Next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return result, nil
}
