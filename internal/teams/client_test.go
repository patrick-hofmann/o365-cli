package teams

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeGraph map[string]string

func (f fakeGraph) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := f[r.URL.Path]
	status := http.StatusOK
	if !ok {
		status, body = http.StatusNotFound, "{}"
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func newFake(f fakeGraph) *Client {
	c := NewClient("token")
	c.HttpClient.Transport = f
	return c
}

func TestHTMLToText(t *testing.T) {
	got := HTMLToText(`<p>Hi &amp; hello<br>next&nbsp;line</p><div><at id="0">Anna</at></div>`)
	if want := "Hi & hello\nnext line\nAnna"; got != want {
		t.Errorf("HTMLToText = %q, want %q", got, want)
	}
}

func TestListChatsAndMessages(t *testing.T) {
	c := newFake(fakeGraph{
		"/v1.0/me/chats": `{"value":[{"id":"19:a","topic":null,"chatType":"oneOnOne","members":[{"displayName":"Anna"},{"displayName":"Ben"}],"lastMessagePreview":{"createdDateTime":"2026-11-27T10:00:00Z"}}]}`,
		"/v1.0/me/chats/19:a/messages": `{"value":[
			{"id":"2","messageType":"message","createdDateTime":"2026-11-27T10:00:00Z","from":{"user":{"displayName":"Anna"}},"body":{"contentType":"html","content":"<p>Hallo</p><attachment id=\"x\"></attachment>"},"attachments":[{"name":"a.pdf"}]},
			{"id":"1","messageType":"systemEventMessage","createdDateTime":"2026-11-27T09:00:00Z","from":null,"body":{"contentType":"html","content":"<systemEventMessage/>"}}]}`,
	})
	chats, err := c.ListChats(5)
	if err != nil || len(chats) != 1 || strings.Join(chats[0].Members, ",") != "Anna,Ben" || chats[0].LastMessageTime == nil {
		t.Fatalf("ListChats = %+v, %v", chats, err)
	}
	msgs, err := c.ListMessages("19:a", 5)
	if err != nil || len(msgs) != 1 || msgs[0].Sender != "Anna" || msgs[0].Text != "Hallo" || strings.Join(msgs[0].Attachments, ",") != "a.pdf" {
		t.Fatalf("ListMessages = %+v, %v", msgs, err)
	}
}
