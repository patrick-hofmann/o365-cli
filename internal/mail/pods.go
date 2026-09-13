package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

const messageFields = "id,changeKey,internetMessageId,conversationId,parentFolderId,subject,body,bodyPreview,receivedDateTime,sentDateTime,lastModifiedDateTime,isRead,from,toRecipients,ccRecipients,hasAttachments"

type ReadRequest struct {
	Operation  string
	Folder     string
	Message    string
	Attachment string
	Cursor     string
}

type ReadPage struct {
	Version    int               `json:"version"`
	Operation  string            `json:"operation"`
	Account    string            `json:"account"`
	Folder     string            `json:"folder,omitempty"`
	Message    string            `json:"message,omitempty"`
	Items      []json.RawMessage `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
	Complete   bool              `json:"complete"`
}

func validID(value string) bool {
	return value != "" && len(value) <= 2048 && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

// Endpoint accepts provider pagination only within the exact selected collection.
func (r ReadRequest) Endpoint() (string, error) {
	base := graph.GraphAPIBaseURL + "/me/mailFolders"
	query := url.Values{"$top": {"10"}}
	switch r.Operation {
	case "folders":
		if r.Message != "" || r.Attachment != "" {
			return "", errors.New("unexpected folder read arguments")
		}
		if r.Folder != "" {
			if !validID(r.Folder) {
				return "", errors.New("invalid folder id")
			}
			base += "/" + url.PathEscape(r.Folder) + "/childFolders"
		}
		query.Set("$select", "id,displayName,parentFolderId,childFolderCount,totalItemCount,unreadItemCount")
	case "messages", "attachments", "attachment":
		if !validID(r.Folder) {
			return "", errors.New("explicit folder id required")
		}
		base += "/" + url.PathEscape(r.Folder) + "/messages"
		if r.Operation == "messages" {
			if r.Message != "" || r.Attachment != "" {
				return "", errors.New("unexpected message read arguments")
			}
			query.Set("$select", messageFields)
			query.Set("$orderby", "receivedDateTime asc")
		} else {
			if !validID(r.Message) {
				return "", errors.New("explicit message id required")
			}
			base += "/" + url.PathEscape(r.Message) + "/attachments"
			query.Set("$select", "id,name,contentType,size,isInline")
			if r.Operation == "attachment" {
				if !validID(r.Attachment) || r.Cursor != "" {
					return "", errors.New("explicit attachment id and no cursor required")
				}
				base += "/" + url.PathEscape(r.Attachment)
				query = url.Values{}
			} else if r.Attachment != "" {
				return "", errors.New("unexpected attachment id")
			}
		}
	default:
		return "", errors.New("unknown read operation")
	}
	if r.Cursor == "" {
		return base + "?" + query.Encode(), nil
	}
	if len(r.Cursor) > 16384 {
		return "", errors.New("pagination cursor is too large")
	}
	if err := graph.ValidateEndpoint(r.Cursor); err != nil {
		return "", err
	}
	cursor, err := url.Parse(r.Cursor)
	if err != nil {
		return "", errors.New("invalid pagination cursor")
	}
	expected, _ := url.Parse(base)
	if cursor.EscapedPath() != expected.EscapedPath() {
		return "", errors.New("pagination cursor changed the assigned collection")
	}
	values, err := url.ParseQuery(cursor.RawQuery)
	if err != nil {
		return "", errors.New("invalid pagination query")
	}
	for key, vals := range values {
		if len(vals) != 1 {
			return "", errors.New("duplicate pagination parameter")
		}
		if key == "$skip" || key == "$skiptoken" {
			continue
		}
		if vals[0] != query.Get(key) || !query.Has(key) {
			return "", errors.New("pagination cursor changed the read contract")
		}
	}
	for key, vals := range query {
		if values.Get(key) != vals[0] {
			return "", errors.New("pagination cursor omitted the read contract")
		}
	}
	return r.Cursor, nil
}

func Read(ctx context.Context, token, account string, request ReadRequest) (*ReadPage, error) {
	return readPage(graph.NewReadOnlyClient(ctx, token), account, request)
}

func readPage(client *graph.Client, account string, request ReadRequest) (*ReadPage, error) {
	endpoint, err := request.Endpoint()
	if err != nil {
		return nil, err
	}
	body, err := client.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	page := &ReadPage{Version: 1, Operation: request.Operation, Account: account, Folder: request.Folder, Message: request.Message, Items: []json.RawMessage{}}
	if request.Operation == "attachment" {
		var attachment GraphAttachmentResponse
		if err := json.Unmarshal(body, &attachment); err != nil || attachment.ID != request.Attachment {
			return nil, errors.New("invalid attachment response")
		}
		page.Items = append(page.Items, json.RawMessage(body))
	} else {
		var response struct {
			Value    []json.RawMessage `json:"value"`
			NextLink string            `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &response); err != nil || response.Value == nil || len(response.Value) > 100 {
			return nil, errors.New("invalid or oversized read page")
		}
		page.Items = response.Value
		page.NextCursor = response.NextLink
		if response.NextLink != "" {
			next := request
			next.Cursor = response.NextLink
			if _, err := next.Endpoint(); err != nil {
				return nil, fmt.Errorf("incomplete inventory: %w", err)
			}
			if next.Cursor == request.Cursor {
				return nil, errors.New("incomplete inventory: repeated cursor")
			}
		}
	}
	page.Complete = page.NextCursor == ""
	return page, nil
}
