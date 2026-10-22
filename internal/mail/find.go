package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

// FindRequest selects Inbox messages on the server by exact category names, so a Pod needs one call instead of paging the Inbox.
type FindRequest struct {
	Account        string
	With           []string // at least one of these categories (empty: no requirement)
	Without        []string // none of these categories
	HasAttachments bool
	Oldest         bool
	Top            int
	Snapshot       bool // also return the label snapshot of the first message, saving the Pod a second call
}

type FoundMessage struct {
	ID               string   `json:"id"`
	ReceivedDateTime string   `json:"receivedDateTime"`
	Categories       []string `json:"categories"`
	HasAttachments   bool     `json:"hasAttachments"`
}

func (r FindRequest) endpoint() (string, error) {
	if _, err := (WorkflowRequest{Operation: "delta", Account: r.Account}).endpoint(); err != nil {
		return "", err
	}
	if r.Top < 1 || r.Top > 50 || len(r.With) > 25 || len(r.Without) > 25 {
		return "", errors.New("find needs --top 1..50 and at most 25 --with and 25 --without categories")
	}
	quote := func(name string) (string, error) {
		if strings.TrimSpace(name) == "" || len(name) > 255 || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 }) {
			return "", errors.New("category names must be non-empty printable text up to 255 characters")
		}
		return "'" + strings.ReplaceAll(name, "'", "''") + "'", nil
	}
	// Graph accepts $orderby with $filter only when the ordered property comes first in the filter.
	clauses := []string{"receivedDateTime ge 1900-01-01T00:00:00Z"}
	if r.HasAttachments {
		clauses = append(clauses, "hasAttachments eq true")
	}
	var any []string
	for _, name := range r.With {
		value, err := quote(name)
		if err != nil {
			return "", err
		}
		any = append(any, "categories/any(c:c eq "+value+")")
	}
	if len(any) > 0 {
		clauses = append(clauses, "("+strings.Join(any, " or ")+")")
	}
	for _, name := range r.Without {
		value, err := quote(name)
		if err != nil {
			return "", err
		}
		clauses = append(clauses, "not(categories/any(c:c eq "+value+"))")
	}
	order := "receivedDateTime desc"
	if r.Oldest {
		order = "receivedDateTime asc"
	}
	query := url.Values{"$filter": {strings.Join(clauses, " and ")}, "$orderby": {order}, "$top": {fmt.Sprint(r.Top)}, "$select": {"id,receivedDateTime,categories,hasAttachments"}}
	return graph.GraphAPIBaseURL + "/users/" + url.PathEscape(r.Account) + "/mailFolders/inbox/messages?" + query.Encode(), nil
}

func (r FindRequest) Validate() error { _, err := r.endpoint(); return err }

// FindMessages returns at most Top Inbox messages matching the category filter, newest first unless Oldest is set.
func FindMessages(ctx context.Context, client *graph.Client, r FindRequest) (*WorkflowReply, error) {
	endpoint, err := r.endpoint()
	if err != nil {
		return nil, err
	}
	body, err := client.DoRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		Value []FoundMessage `json:"value"`
	}
	if json.Unmarshal(body, &page) != nil {
		return nil, errors.New("Graph returned an unreadable message list")
	}
	operation := "find"
	if r.Snapshot {
		operation = "find-first"
	}
	reply := &WorkflowReply{Protocol: "pods-mail/v1", Account: r.Account, Operation: operation, Outcome: "confirmed", Items: []json.RawMessage{}}
	for _, message := range page.Value {
		if message.ID == "" {
			return nil, errors.New("Graph returned a message without id")
		}
		if message.Categories == nil {
			message.Categories = []string{}
		}
		item, _ := json.Marshal(message)
		reply.Items = append(reply.Items, item)
	}
	if r.Snapshot && len(page.Value) > 0 {
		if reply.Snapshot, err = ReadLabelSnapshot(ctx, client, r.Account, page.Value[0].ID); err != nil {
			return nil, err
		}
	}
	return reply, nil
}
