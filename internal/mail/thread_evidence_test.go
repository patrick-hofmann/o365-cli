package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

func TestThreadEvidenceHasFullBodiesImmutableIDsSentFolderAndBoundedPages(t *testing.T) {
	longBody := strings.Repeat("x", 30000)
	firstItem := strings.Replace(labelFixture, "Complete body", longBody, 1)
	secondItem := strings.Replace(labelFixture, `"id":"immutable-before"`, `"id":"immutable-sent"`, 1)
	secondItem = strings.Replace(secondItem, `"parentFolderId":"inbox-id"`, `"parentFolderId":"sent-id"`, 1)
	next := threadEndpoint("pod@example.invalid", "conversation-1") + "&%24skiptoken=next"
	var paths []string
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasPrefix(r.URL.Path, "/v1.0/users/pod@example.invalid/") {
			t.Fatal("wrong account")
		}
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/mailFolders/sentitems") {
			return fixtureResponse(`{"id":"sent-id"}`), nil
		}
		if strings.Contains(r.URL.Path, "/mailFolders/inbox") {
			return fixtureResponse(`{"id":"inbox-id"}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/messages/immutable-before") {
			return fixtureResponse(labelFixture), nil
		}
		if !strings.Contains(r.Header.Get("Prefer"), `IdType="ImmutableId"`) || !strings.Contains(r.Header.Get("Prefer"), `outlook.body-content-type="text"`) {
			t.Fatal("thread omitted immutable/fulltext preference")
		}
		if r.URL.Query().Get("$filter") != "conversationId eq 'conversation-1'" {
			t.Fatal("unbounded thread filter")
		}
		if r.URL.Query().Get("$skiptoken") == "next" {
			return fixtureResponse(`{"value":[` + secondItem + `]}`), nil
		}
		page, _ := json.Marshal(map[string]any{"value": []json.RawMessage{json.RawMessage(firstItem)}, "@odata.nextLink": next})
		return fixtureResponse(string(page)), nil
	})
	first, err := ReadThreadEvidence(context.Background(), client, "pod@example.invalid", "immutable-before", "")
	if err != nil || first.Complete || first.Next == "" || first.SentFolder != "sent-id" || !strings.Contains(string(first.Items[0]), longBody) {
		t.Fatalf("bad first page %+v %v", first, err)
	}
	second, err := ReadThreadEvidence(context.Background(), client, "pod@example.invalid", "immutable-before", first.Next)
	if err != nil || !second.Complete || second.Next != "" || !strings.Contains(string(second.Items[0]), `"parentFolderId":"sent-id"`) {
		t.Fatalf("bad last page %+v %v", second, err)
	}
	for _, scenario := range []string{"account", "message", "content", "filter", "origin", "fields"} {
		data, _ := base64.RawURLEncoding.DecodeString(first.Next)
		var saved threadCursor
		_ = json.Unmarshal(data, &saved)
		switch scenario {
		case "account":
			saved.Next = strings.Replace(saved.Next, "pod@example.invalid", "other@example.invalid", 1)
		case "message":
			saved.Message = "other-message"
		case "content":
			saved.Version = "assessment-v1:" + strings.Repeat("a", 64)
		case "filter":
			u, _ := url.Parse(saved.Next)
			q := u.Query()
			q.Set("$filter", "conversationId eq 'other'")
			u.RawQuery = q.Encode()
			saved.Next = u.String()
		case "origin":
			saved.Next = strings.Replace(saved.Next, "graph.microsoft.com", "attacker.invalid", 1)
		case "fields":
			u, _ := url.Parse(saved.Next)
			q := u.Query()
			q.Set("$select", "id,body")
			u.RawQuery = q.Encode()
			saved.Next = u.String()
		}
		altered, _ := json.Marshal(saved)
		if _, err := ReadThreadEvidence(context.Background(), client, "pod@example.invalid", "immutable-before", base64.RawURLEncoding.EncodeToString(altered)); err == nil {
			t.Fatal("untrusted continuation accepted", scenario)
		}
	}
}

func TestThreadEvidenceRefusesTruncatedDraftUnknownOrWrongConversation(t *testing.T) {
	for _, scenario := range []string{"truncated", "draft-omitted", "wrong-conversation", "unsafe-next"} {
		t.Run(scenario, func(t *testing.T) {
			item := labelFixture
			switch scenario {
			case "truncated":
				item = strings.TrimSuffix(item, "}") + `,"bodyTruncated":true}`
			case "draft-omitted":
				item = strings.Replace(item, `"isDraft":false,`, "", 1)
			case "wrong-conversation":
				item = strings.Replace(item, "conversation-1", "other", 1)
			}
			client := graph.NewClient("synthetic-secret")
			client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "/mailFolders/") {
					return fixtureResponse(`{"id":"folder-id"}`), nil
				}
				if strings.HasSuffix(r.URL.Path, "/messages/immutable-before") {
					return fixtureResponse(labelFixture), nil
				}
				page := map[string]any{"value": []json.RawMessage{json.RawMessage(item)}}
				if scenario == "unsafe-next" {
					page["@odata.nextLink"] = "https://attacker.invalid/next"
				}
				raw, _ := json.Marshal(page)
				return fixtureResponse(string(raw)), nil
			})
			if _, err := ReadThreadEvidence(context.Background(), client, "pod@example.invalid", "immutable-before", ""); err == nil {
				t.Fatal("incomplete evidence accepted")
			}
		})
	}
}
