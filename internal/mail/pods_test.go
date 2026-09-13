package mail

import (
	"context"
	"fmt"
	"github.com/yourname/o365-cli/internal/graph"
	"net/http"
	"net/url"
	"testing"
)

func TestPodsPageContractScopesContinuationAndPreservesOldMovedMail(t *testing.T) {
	request := ReadRequest{Operation: "messages", Folder: "rules-folder"}
	endpoint, err := request.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	next, _ := url.Parse(endpoint)
	q := next.Query()
	q.Set("$skip", "10")
	next.RawQuery = q.Encode()
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-token")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.Header.Get("Prefer") != `IdType="ImmutableId"` || r.URL.Query().Get("$filter") != "" {
			t.Fatal("read was mutating, unstable, or filtered out moved old mail")
		}
		if r.URL.Query().Get("$skip") == "10" {
			return fixtureResponse(`{"value":[{"id":"stable-id","changeKey":"v2","receivedDateTime":"2020-01-01T00:00:00Z","parentFolderId":"rules-folder"}]}`), nil
		}
		return fixtureResponse(fmt.Sprintf(`{"value":[],"@odata.nextLink":%q}`, next.String())), nil
	})
	page, err := readPage(client, "pod@example.invalid", request)
	if err != nil || page.Complete || page.NextCursor == "" {
		t.Fatalf("first page: %v %v", page, err)
	}
	request.Cursor = page.NextCursor
	page, err = readPage(client, "pod@example.invalid", request)
	if err != nil || !page.Complete || len(page.Items) != 1 {
		t.Fatalf("last page: %v %v", page, err)
	}
	for _, cursor := range []string{"https://unassigned.invalid/x", "https://graph.microsoft.com/v1.0/me/mailFolders/sentitems/messages?" + q.Encode(), endpoint + "&$expand=attachments", endpoint + "&$select=id", endpoint + "&$top=1000"} {
		request.Cursor = cursor
		if _, err := request.Endpoint(); err == nil {
			t.Fatalf("unsafe cursor accepted: %s", cursor)
		}
	}
}
func TestPodsReadArgumentsFailClosed(t *testing.T) {
	for _, request := range []ReadRequest{{Operation: "send"}, {Operation: "messages", Folder: ".."}, {Operation: "messages", Folder: "inbox", Attachment: "x"}, {Operation: "attachments", Folder: "inbox", Message: "../x"}, {Operation: "attachment", Folder: "inbox", Message: "m", Attachment: "x", Cursor: "x"}} {
		if _, err := request.Endpoint(); err == nil {
			t.Fatalf("invalid read accepted: %+v", request)
		}
	}
}
func TestPodsForeignContinuationReportsIncompleteInventory(t *testing.T) {
	client := graph.NewReadOnlyClient(context.Background(), "synthetic-token")
	client.HttpClient.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return fixtureResponse(`{"value":[],"@odata.nextLink":"https://unassigned.invalid/page"}`), nil
	})
	if _, err := readPage(client, "pod@example.invalid", ReadRequest{Operation: "messages", Folder: "inbox"}); err == nil {
		t.Fatal("incomplete inventory returned success")
	}
}
