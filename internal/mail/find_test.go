package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yourname/o365-cli/internal/graph"
)

func TestFindBuildsOneServerSideCategoryQuery(t *testing.T) {
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-secret")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		want := "receivedDateTime ge 1900-01-01T00:00:00Z and hasAttachments eq true and (categories/any(c:c eq 'Alarm: offen') or categories/any(c:c eq 'Alarm: Fehler')) and not(categories/any(c:c eq 'O''Brien'))"
		if r.URL.Path != "/v1.0/users/pod@example.invalid/mailFolders/inbox/messages" || q.Get("$filter") != want || q.Get("$orderby") != "receivedDateTime asc" || q.Get("$top") != "2" || r.Header.Get("Prefer") != `IdType="ImmutableId"` {
			t.Fatalf("unexpected query %s %v", r.URL.Path, q)
		}
		return fixtureResponse(`{"value":[{"id":"m1","receivedDateTime":"2026-10-21T10:00:00Z","hasAttachments":true}]}`), nil
	})
	reply, err := FindMessages(context.Background(), client, FindRequest{Account: "pod@example.invalid", With: []string{"Alarm: offen", "Alarm: Fehler"}, Without: []string{"O'Brien"}, HasAttachments: true, Oldest: true, Top: 2})
	if err != nil || reply.Outcome != "confirmed" || len(reply.Items) != 1 {
		t.Fatalf("find: %+v %v", reply, err)
	}
	var found FoundMessage
	if json.Unmarshal(reply.Items[0], &found) != nil || found.ID != "m1" || found.Categories == nil {
		t.Fatalf("item: %s", reply.Items[0])
	}
	for _, invalid := range []FindRequest{{Account: "pod@example.invalid", Top: 0}, {Account: "pod@example.invalid", Top: 51}, {Account: "not an address", Top: 1}, {Account: "pod@example.invalid", Top: 1, Without: []string{" "}}} {
		if _, err := invalid.endpoint(); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
	}
}
