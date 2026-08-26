package mail

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourname/o365-cli/internal/graph"
)

// Client wraps the generic Graph client with mail-specific operations.
type Client struct {
	*graph.Client
}

// NewClient creates a new mail client.
func NewClient(accessToken string) *Client {
	return &Client{Client: graph.NewClient(accessToken)}
}

// Email represents an email message
type Email struct {
	ID        string `json:"id"`
	Account   string `json:"account,omitempty"`
	MessageID string `json:"message_id"`
	// InternetMessageID is the RFC 5322 Message-ID. Unlike MessageID (the Graph
	// item id) it survives moving the mail to another folder, so it is the only
	// usable key for tracking a message across a mailbox.
	InternetMessageID string       `json:"internet_message_id,omitempty"`
	Subject           string       `json:"subject"`
	From              string       `json:"from"`
	To                []string     `json:"to"`
	Cc                []string     `json:"cc,omitempty"`
	Date              time.Time    `json:"date"`
	Body              string       `json:"body,omitempty"`
	Preview           string       `json:"preview,omitempty"`
	Unread            bool         `json:"unread"`
	HasAttachments    bool         `json:"has_attachments,omitempty"`
	Attachments       []Attachment `json:"attachments,omitempty"`
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Inline      bool   `json:"inline,omitempty"`
	SavedPath   string `json:"saved_path,omitempty"`
}

// SendOptions contains options for sending an email
type SendOptions struct {
	To      []string
	Cc      []string
	Bcc     []string
	Subject string
	Body    string
	HTML    bool
}

// GraphMessageResponse represents a message from Graph API
type GraphMessageResponse struct {
	ID                string                           `json:"id"`
	Subject           string                           `json:"subject"`
	BodyPreview       string                           `json:"bodyPreview"`
	Body              graph.GraphBodyResponse          `json:"body"`
	ReceivedDateTime  string                           `json:"receivedDateTime"`
	IsRead            bool                             `json:"isRead"`
	From              *graph.GraphEmailAddressWrapper  `json:"from"`
	ToRecipients      []graph.GraphEmailAddressWrapper `json:"toRecipients"`
	CcRecipients      []graph.GraphEmailAddressWrapper `json:"ccRecipients"`
	HasAttachments    bool                             `json:"hasAttachments"`
	InternetMessageId string                           `json:"internetMessageId"`
	ParentFolderId    string                           `json:"parentFolderId"`
	Attachments       []GraphAttachmentResponse        `json:"attachments"`
}

// GraphMessagesResponse represents the list response
type GraphMessagesResponse struct {
	Value    []GraphMessageResponse `json:"value"`
	NextLink string                 `json:"@odata.nextLink"`
}

// GraphFolderResponse represents a mail folder
type GraphFolderResponse struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ParentFolderId   string `json:"parentFolderId"`
	ChildFolderCount int    `json:"childFolderCount"`
	UnreadItemCount  int    `json:"unreadItemCount"`
	TotalItemCount   int    `json:"totalItemCount"`
}

// GraphFoldersResponse represents the folders list response
type GraphFoldersResponse struct {
	Value    []GraphFolderResponse `json:"value"`
	NextLink string                `json:"@odata.nextLink"`
}

// GraphAttachmentResponse represents an attachment
type GraphAttachmentResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	Size         int    `json:"size"`
	IsInline     bool   `json:"isInline"`
	ContentBytes string `json:"contentBytes"`
}

// GraphAttachmentsResponse represents the attachments list response
type GraphAttachmentsResponse struct {
	Value []GraphAttachmentResponse `json:"value"`
}

// Folder represents a mail folder
type Folder struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	UnreadCount      int    `json:"unread_count"`
	TotalCount       int    `json:"total_count"`
	ChildFolderCount int    `json:"child_folder_count"`
}

// GraphMessage for sending
type GraphMessage struct {
	Subject       string                           `json:"subject"`
	Body          GraphBody                        `json:"body"`
	ToRecipients  []graph.GraphEmailAddressWrapper `json:"toRecipients"`
	CcRecipients  []graph.GraphEmailAddressWrapper `json:"ccRecipients,omitempty"`
	BccRecipients []graph.GraphEmailAddressWrapper `json:"bccRecipients,omitempty"`
}

// GraphBody represents the email body
type GraphBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

// ListEmails lists emails from a folder. With withBody the full body and the
// attachment metadata come along, which is roughly fifteen times faster than
// fetching each message individually when exporting a whole folder.
func (c *Client) ListEmails(folderID string, limit int, unreadOnly bool, withBody bool) ([]Email, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", graph.GraphAPIBaseURL, url.PathEscape(folderID))

	pageSize := limit
	if pageSize > 100 {
		pageSize = 100
	}
	params := url.Values{}
	params.Set("$top", fmt.Sprintf("%d", pageSize))
	params.Set("$orderby", "receivedDateTime desc")
	params.Set("$select", "id,subject,bodyPreview,receivedDateTime,isRead,from,toRecipients,hasAttachments,internetMessageId")

	if withBody {
		params.Set("$select", "id,subject,body,receivedDateTime,isRead,from,toRecipients,ccRecipients,hasAttachments,internetMessageId")
		params.Set("$expand", "attachments($select=name,size,contentType,isInline)")
	}

	if unreadOnly {
		params.Set("$filter", "isRead eq false")
	}

	var allEmails []Email
	currentEndpoint := endpoint + "?" + params.Encode()

	for currentEndpoint != "" {
		resp, err := c.DoRequest("GET", currentEndpoint, nil)
		if err != nil {
			return nil, err
		}

		var result GraphMessagesResponse
		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		for _, msg := range result.Value {
			allEmails = append(allEmails, graphMessageToEmail(msg))
			if len(allEmails) >= limit {
				return allEmails, nil
			}
		}

		currentEndpoint = result.NextLink
	}

	return allEmails, nil
}

// GetEmail fetches a single email with full body
func (c *Client) GetEmail(folderID string, messageID string) (*Email, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages/%s", graph.GraphAPIBaseURL, url.PathEscape(folderID), messageID)
	params := url.Values{}
	params.Set("$select", "id,subject,body,receivedDateTime,isRead,from,toRecipients,ccRecipients,hasAttachments,internetMessageId")
	params.Set("$expand", "attachments($select=name,size,contentType,isInline)")
	endpoint += "?" + params.Encode()

	resp, err := c.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var msg GraphMessageResponse
	if err := json.Unmarshal(resp, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	email := graphMessageToEmail(msg)

	return &email, nil
}

// MarkAsRead marks an email as read
func (c *Client) MarkAsRead(folderID string, messageID string) error {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages/%s", graph.GraphAPIBaseURL, url.PathEscape(folderID), messageID)
	body := map[string]interface{}{"isRead": true}

	jsonBody, _ := json.Marshal(body)
	_, err := c.DoRequest("PATCH", endpoint, jsonBody)
	return err
}

// MarkAsUnread marks an email as unread
func (c *Client) MarkAsUnread(folderID string, messageID string) error {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages/%s", graph.GraphAPIBaseURL, url.PathEscape(folderID), messageID)
	body := map[string]interface{}{"isRead": false}

	jsonBody, _ := json.Marshal(body)
	_, err := c.DoRequest("PATCH", endpoint, jsonBody)
	return err
}

// MoveEmail moves an email to another folder
func (c *Client) MoveEmail(folderID string, messageID string, destinationFolderID string) error {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages/%s/move", graph.GraphAPIBaseURL, url.PathEscape(folderID), messageID)
	body := map[string]string{"destinationId": destinationFolderID}

	jsonBody, _ := json.Marshal(body)
	_, err := c.DoRequest("POST", endpoint, jsonBody)
	return err
}

// TrashEmail moves an email to the deleted items folder
func (c *Client) TrashEmail(folderID string, messageID string) error {
	return c.MoveEmail(folderID, messageID, "deleteditems")
}

// ListEmailsFromSenders lists all emails from specific sender addresses (exact match)
func (c *Client) ListEmailsFromSenders(folderID string, senderAddresses []string, limit int) ([]Email, error) {
	if len(senderAddresses) == 0 {
		return nil, fmt.Errorf("at least one sender address required")
	}

	normalizedAddrs := make(map[string]bool)
	for _, addr := range senderAddresses {
		normalizedAddrs[strings.ToLower(addr)] = true
	}

	var allEmails []Email
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", graph.GraphAPIBaseURL, url.PathEscape(folderID))

	params := url.Values{}
	params.Set("$top", "100")
	params.Set("$orderby", "receivedDateTime desc")
	params.Set("$select", "id,subject,bodyPreview,receivedDateTime,isRead,from,toRecipients,hasAttachments,internetMessageId")

	currentEndpoint := endpoint + "?" + params.Encode()

	for currentEndpoint != "" {
		resp, err := c.DoRequest("GET", currentEndpoint, nil)
		if err != nil {
			return nil, err
		}

		var result GraphMessagesResponse
		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		for _, msg := range result.Value {
			if msg.From != nil {
				fromAddr := strings.ToLower(msg.From.EmailAddress.Address)
				if normalizedAddrs[fromAddr] {
					allEmails = append(allEmails, graphMessageToEmail(msg))
					if limit > 0 && len(allEmails) >= limit {
						return allEmails, nil
					}
				}
			}
		}

		currentEndpoint = result.NextLink
	}

	return allEmails, nil
}

// SearchEmails searches emails by criteria
func (c *Client) SearchEmails(folderID string, from, subject string, since time.Time, limit int) ([]Email, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", graph.GraphAPIBaseURL, url.PathEscape(folderID))

	pageSize := limit
	if pageSize > 100 {
		pageSize = 100
	}
	params := url.Values{}
	params.Set("$top", fmt.Sprintf("%d", pageSize))
	params.Set("$orderby", "receivedDateTime desc")
	params.Set("$select", "id,subject,bodyPreview,receivedDateTime,isRead,from,toRecipients,hasAttachments,internetMessageId")

	var filters []string
	if from != "" {
		filters = append(filters, fmt.Sprintf("contains(from/emailAddress/address,'%s')", from))
	}
	if subject != "" {
		filters = append(filters, fmt.Sprintf("contains(subject,'%s')", subject))
	}
	if !since.IsZero() {
		filters = append(filters, fmt.Sprintf("receivedDateTime ge %s", since.Format(time.RFC3339)))
	}

	if len(filters) > 0 {
		params.Set("$filter", strings.Join(filters, " and "))
	}

	var allEmails []Email
	currentEndpoint := endpoint + "?" + params.Encode()

	for currentEndpoint != "" {
		resp, err := c.DoRequest("GET", currentEndpoint, nil)
		if err != nil {
			return nil, err
		}

		var result GraphMessagesResponse
		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		for _, msg := range result.Value {
			allEmails = append(allEmails, graphMessageToEmail(msg))
			if len(allEmails) >= limit {
				return allEmails, nil
			}
		}

		currentEndpoint = result.NextLink
	}

	return allEmails, nil
}

// SearchEmailsKQL searches emails using KQL (Keyword Query Language) via the $search parameter
func (c *Client) SearchEmailsKQL(folderID, query string, limit int) ([]Email, error) {
	var endpoint string
	if folderID == "" {
		endpoint = fmt.Sprintf("%s/me/messages", graph.GraphAPIBaseURL)
	} else {
		endpoint = fmt.Sprintf("%s/me/mailFolders/%s/messages", graph.GraphAPIBaseURL, url.PathEscape(folderID))
	}

	pageSize := limit
	if pageSize > 100 {
		pageSize = 100
	}
	params := url.Values{}
	params.Set("$top", fmt.Sprintf("%d", pageSize))
	params.Set("$search", fmt.Sprintf("%q", query))
	params.Set("$select", "id,subject,bodyPreview,receivedDateTime,isRead,from,toRecipients,hasAttachments,internetMessageId")

	var allEmails []Email
	currentEndpoint := endpoint + "?" + params.Encode()

	for currentEndpoint != "" {
		resp, err := c.DoRequest("GET", currentEndpoint, nil)
		if err != nil {
			return nil, err
		}

		var result GraphMessagesResponse
		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		for _, msg := range result.Value {
			allEmails = append(allEmails, graphMessageToEmail(msg))
			if len(allEmails) >= limit {
				return allEmails, nil
			}
		}

		currentEndpoint = result.NextLink
	}

	return allEmails, nil
}

// GetAttachments downloads attachments from an email
func (c *Client) GetAttachments(folderID string, messageID string, saveDir string) ([]Attachment, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages/%s/attachments", graph.GraphAPIBaseURL, url.PathEscape(folderID), messageID)

	resp, err := c.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result GraphAttachmentsResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var attachments []Attachment
	for _, att := range result.Value {
		attachment := Attachment{
			Filename:    att.Name,
			ContentType: att.ContentType,
			Size:        att.Size,
		}

		if saveDir != "" && att.ContentBytes != "" {
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}

			content, err := base64.StdEncoding.DecodeString(att.ContentBytes)
			if err != nil {
				continue
			}

			savePath := filepath.Join(saveDir, att.Name)
			if err := os.WriteFile(savePath, content, 0644); err != nil {
				return nil, fmt.Errorf("failed to save attachment: %w", err)
			}
			attachment.SavedPath = savePath
		}

		attachments = append(attachments, attachment)
	}

	return attachments, nil
}

// ListFolders lists all mail folders
func (c *Client) ListFolders() ([]Folder, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders?$top=100", graph.GraphAPIBaseURL)

	var allFolders []Folder

	for endpoint != "" {
		resp, err := c.DoRequest("GET", endpoint, nil)
		if err != nil {
			return nil, err
		}

		var result GraphFoldersResponse
		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		for _, f := range result.Value {
			allFolders = append(allFolders, Folder{
				ID:               f.ID,
				Name:             f.DisplayName,
				UnreadCount:      f.UnreadItemCount,
				TotalCount:       f.TotalItemCount,
				ChildFolderCount: f.ChildFolderCount,
			})

			if f.ChildFolderCount > 0 {
				children, err := c.listChildFolders(f.ID, f.DisplayName)
				if err == nil {
					allFolders = append(allFolders, children...)
				}
			}
		}

		endpoint = result.NextLink
	}

	return allFolders, nil
}

// listChildFolders recursively lists child folders
func (c *Client) listChildFolders(parentID, parentPath string) ([]Folder, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/childFolders", graph.GraphAPIBaseURL, parentID)

	resp, err := c.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result GraphFoldersResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	var folders []Folder
	for _, f := range result.Value {
		fullPath := parentPath + "/" + f.DisplayName
		folders = append(folders, Folder{
			ID:               f.ID,
			Name:             fullPath,
			UnreadCount:      f.UnreadItemCount,
			TotalCount:       f.TotalItemCount,
			ChildFolderCount: f.ChildFolderCount,
		})

		if f.ChildFolderCount > 0 {
			children, err := c.listChildFolders(f.ID, fullPath)
			if err == nil {
				folders = append(folders, children...)
			}
		}
	}

	return folders, nil
}

// GetFolderByName finds a folder by name and returns its ID
func (c *Client) GetFolderByName(name string) (string, error) {
	wellKnown := map[string]string{
		"inbox":        "inbox",
		"drafts":       "drafts",
		"sentitems":    "sentitems",
		"deleteditems": "deleteditems",
		"junkemail":    "junkemail",
		"archive":      "archive",
	}

	lower := strings.ToLower(name)
	if id, ok := wellKnown[lower]; ok {
		return id, nil
	}

	folders, err := c.ListFolders()
	if err != nil {
		return "", err
	}

	for _, f := range folders {
		if strings.EqualFold(f.Name, name) {
			return f.ID, nil
		}
	}

	return "", fmt.Errorf("folder '%s' not found", name)
}

// CreateFolder creates a new mail folder
func (c *Client) CreateFolder(name string, parentFolderID string) error {
	var endpoint string
	if parentFolderID != "" {
		endpoint = fmt.Sprintf("%s/me/mailFolders/%s/childFolders", graph.GraphAPIBaseURL, parentFolderID)
	} else {
		endpoint = fmt.Sprintf("%s/me/mailFolders", graph.GraphAPIBaseURL)
	}

	body := map[string]string{"displayName": name}
	jsonBody, _ := json.Marshal(body)

	_, err := c.DoRequest("POST", endpoint, jsonBody)
	return err
}

// DeleteFolder deletes a mail folder
func (c *Client) DeleteFolder(folderID string) error {
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s", graph.GraphAPIBaseURL, folderID)
	_, err := c.DoRequest("DELETE", endpoint, nil)
	return err
}

// Send sends an email
func (c *Client) Send(opts SendOptions) error {
	toRecipients := make([]graph.GraphEmailAddressWrapper, len(opts.To))
	for i, to := range opts.To {
		toRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(to)},
		}
	}

	ccRecipients := make([]graph.GraphEmailAddressWrapper, len(opts.Cc))
	for i, cc := range opts.Cc {
		ccRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(cc)},
		}
	}

	bccRecipients := make([]graph.GraphEmailAddressWrapper, len(opts.Bcc))
	for i, bcc := range opts.Bcc {
		bccRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(bcc)},
		}
	}

	contentType := "text"
	if opts.HTML {
		contentType = "html"
	}

	message := map[string]interface{}{
		"subject": opts.Subject,
		"body": map[string]string{
			"contentType": contentType,
			"content":     opts.Body,
		},
		"toRecipients": toRecipients,
	}

	if len(ccRecipients) > 0 {
		message["ccRecipients"] = ccRecipients
	}
	if len(bccRecipients) > 0 {
		message["bccRecipients"] = bccRecipients
	}

	request := map[string]interface{}{
		"message":         message,
		"saveToSentItems": true,
	}

	jsonBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	_, err = c.DoRequest("POST", graph.GraphAPIBaseURL+"/me/sendMail", jsonBody)
	return err
}

// Reply sends a reply using native Graph API
func (c *Client) Reply(messageID string, comment string, replyAll bool) error {
	action := "reply"
	if replyAll {
		action = "replyAll"
	}
	endpoint := fmt.Sprintf("%s/me/messages/%s/%s", graph.GraphAPIBaseURL, messageID, action)

	body := map[string]interface{}{}
	if comment != "" {
		body["comment"] = comment
	}

	jsonBody, _ := json.Marshal(body)
	_, err := c.DoRequest("POST", endpoint, jsonBody)
	return err
}

// Forward forwards an email using native Graph API
func (c *Client) Forward(messageID string, to []string, comment string) error {
	endpoint := fmt.Sprintf("%s/me/messages/%s/forward", graph.GraphAPIBaseURL, messageID)

	toRecipients := make([]graph.GraphEmailAddressWrapper, len(to))
	for i, addr := range to {
		toRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(addr)},
		}
	}

	body := map[string]interface{}{
		"toRecipients": toRecipients,
	}
	if comment != "" {
		body["comment"] = comment
	}

	jsonBody, _ := json.Marshal(body)
	_, err := c.DoRequest("POST", endpoint, jsonBody)
	return err
}

// SaveDraft saves an email as draft and returns the draft ID
func (c *Client) SaveDraft(to, cc []string, subject, body string, html bool) (string, error) {
	toRecipients := make([]graph.GraphEmailAddressWrapper, len(to))
	for i, addr := range to {
		toRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(addr)},
		}
	}

	ccRecipients := make([]graph.GraphEmailAddressWrapper, len(cc))
	for i, addr := range cc {
		ccRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(addr)},
		}
	}

	contentType := "text"
	if html {
		contentType = "html"
	}

	message := map[string]interface{}{
		"subject": subject,
		"body": map[string]string{
			"contentType": contentType,
			"content":     body,
		},
		"toRecipients": toRecipients,
	}

	if len(ccRecipients) > 0 {
		message["ccRecipients"] = ccRecipients
	}

	jsonBody, err := json.Marshal(message)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.DoRequest("POST", graph.GraphAPIBaseURL+"/me/messages", jsonBody)
	if err != nil {
		return "", err
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.ID, nil
}

// ListDrafts lists draft emails
func (c *Client) ListDrafts(limit int) ([]Email, error) {
	return c.ListEmails("drafts", limit, false, false)
}

// SetDraftBcc patches the bccRecipients of an existing draft. Useful when a
// draft was created without bcc (e.g., via SaveDraft) and bcc needs to be set
// before sending.
func (c *Client) SetDraftBcc(messageID string, bcc []string) error {
	if len(bcc) == 0 {
		return nil
	}
	bccRecipients := make([]graph.GraphEmailAddressWrapper, len(bcc))
	for i, addr := range bcc {
		bccRecipients[i] = graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(addr)},
		}
	}
	body := map[string]interface{}{
		"bccRecipients": bccRecipients,
	}
	jsonBody, _ := json.Marshal(body)
	endpoint := fmt.Sprintf("%s/me/messages/%s", graph.GraphAPIBaseURL, messageID)
	_, err := c.DoRequest("PATCH", endpoint, jsonBody)
	return err
}

// SendDraft sends a draft and deletes it
func (c *Client) SendDraft(messageID string) error {
	endpoint := fmt.Sprintf("%s/me/messages/%s/send", graph.GraphAPIBaseURL, messageID)
	_, err := c.DoRequest("POST", endpoint, nil)
	return err
}

// DeleteDraft deletes a draft
func (c *Client) DeleteDraft(messageID string) error {
	endpoint := fmt.Sprintf("%s/me/messages/%s", graph.GraphAPIBaseURL, messageID)
	_, err := c.DoRequest("DELETE", endpoint, nil)
	return err
}

// CreateReplyDraft creates a reply draft using Graph's createReply / createReplyAll
// endpoint. The original message body is auto-quoted (with native Outlook formatting,
// including inline images). The provided `comment` is inserted at the top.
//
// Optional `to`/`cc`/`bcc` are ADDITIVE: the auto-populated recipients from
// createReply/createReplyAll are preserved, and the addresses passed here are
// appended (deduplicated, case-insensitive). This is implemented as a two-step
// operation: createReply, then PATCH the resulting draft with the merged
// recipient lists.
//
// Returns the new draft's message ID.
func (c *Client) CreateReplyDraft(messageID, comment string, replyAll bool, to, cc, bcc []string) (string, error) {
	action := "createReply"
	if replyAll {
		action = "createReplyAll"
	}
	endpoint := fmt.Sprintf("%s/me/messages/%s/%s", graph.GraphAPIBaseURL, messageID, action)

	body := map[string]interface{}{}
	if comment != "" {
		body["comment"] = comment
	}

	jsonBody, _ := json.Marshal(body)
	resp, err := c.DoRequest("POST", endpoint, jsonBody)
	if err != nil {
		return "", err
	}

	var created struct {
		ID            string                           `json:"id"`
		ToRecipients  []graph.GraphEmailAddressWrapper `json:"toRecipients"`
		CcRecipients  []graph.GraphEmailAddressWrapper `json:"ccRecipients"`
		BccRecipients []graph.GraphEmailAddressWrapper `json:"bccRecipients"`
	}
	if err := json.Unmarshal(resp, &created); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// If no extra recipients to add, we're done.
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return created.ID, nil
	}

	// Merge auto-populated recipients with extra addresses (deduplicated).
	mergedTo := mergeRecipients(created.ToRecipients, to)
	mergedCc := mergeRecipients(created.CcRecipients, cc)
	mergedBcc := mergeRecipients(created.BccRecipients, bcc)

	patchBody := map[string]interface{}{}
	if len(to) > 0 {
		patchBody["toRecipients"] = mergedTo
	}
	if len(cc) > 0 {
		patchBody["ccRecipients"] = mergedCc
	}
	if len(bcc) > 0 {
		patchBody["bccRecipients"] = mergedBcc
	}

	patchJSON, _ := json.Marshal(patchBody)
	patchEndpoint := fmt.Sprintf("%s/me/messages/%s", graph.GraphAPIBaseURL, created.ID)
	if _, err := c.DoRequest("PATCH", patchEndpoint, patchJSON); err != nil {
		return created.ID, fmt.Errorf("created draft %s but failed to patch recipients: %w", created.ID, err)
	}

	return created.ID, nil
}

// mergeRecipients returns the union of `existing` and `extras` (case-insensitive
// dedup on email address), preserving existing order followed by new additions.
func mergeRecipients(existing []graph.GraphEmailAddressWrapper, extras []string) []graph.GraphEmailAddressWrapper {
	seen := make(map[string]bool, len(existing)+len(extras))
	merged := make([]graph.GraphEmailAddressWrapper, 0, len(existing)+len(extras))
	for _, r := range existing {
		key := strings.ToLower(r.EmailAddress.Address)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, r)
	}
	for _, addr := range extras {
		parsed := graph.ParseEmail(addr)
		key := strings.ToLower(parsed)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, graph.GraphEmailAddressWrapper{
			EmailAddress: graph.GraphEmailAddress{Address: parsed},
		})
	}
	return merged
}

// CreateForwardDraft creates a forward draft using Graph's createForward endpoint.
// `to`/`cc`/`bcc` set recipients on the new draft (all optional — caller can leave
// blank and edit later in Outlook). `comment` is inserted at the top of the body.
func (c *Client) CreateForwardDraft(messageID, comment string, to, cc, bcc []string) (string, error) {
	endpoint := fmt.Sprintf("%s/me/messages/%s/createForward", graph.GraphAPIBaseURL, messageID)

	body := map[string]interface{}{}
	if comment != "" {
		body["comment"] = comment
	}

	message := map[string]interface{}{}
	addRecipients := func(field string, addrs []string) {
		if len(addrs) == 0 {
			return
		}
		recipients := make([]graph.GraphEmailAddressWrapper, len(addrs))
		for i, addr := range addrs {
			recipients[i] = graph.GraphEmailAddressWrapper{
				EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(addr)},
			}
		}
		message[field] = recipients
	}
	addRecipients("toRecipients", to)
	addRecipients("ccRecipients", cc)
	addRecipients("bccRecipients", bcc)

	if len(message) > 0 {
		body["message"] = message
	}

	jsonBody, _ := json.Marshal(body)
	resp, err := c.DoRequest("POST", endpoint, jsonBody)
	if err != nil {
		return "", err
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.ID, nil
}

// graphMessageToEmail converts a Graph API message to our Email struct
func graphMessageToEmail(msg GraphMessageResponse) Email {
	email := Email{
		MessageID:         msg.ID,
		InternetMessageID: msg.InternetMessageId,
		Subject:           msg.Subject,
		Preview:           msg.BodyPreview,
		Body:              msg.Body.Content,
		Unread:            !msg.IsRead,
		HasAttachments:    msg.HasAttachments,
	}

	if t, err := time.Parse(time.RFC3339, msg.ReceivedDateTime); err == nil {
		email.Date = t
	}

	if msg.From != nil {
		email.From = graph.FormatGraphAddress(msg.From.EmailAddress)
	}

	for _, to := range msg.ToRecipients {
		email.To = append(email.To, graph.FormatGraphAddress(to.EmailAddress))
	}

	for _, cc := range msg.CcRecipients {
		email.Cc = append(email.Cc, graph.FormatGraphAddress(cc.EmailAddress))
	}

	for _, att := range msg.Attachments {
		email.Attachments = append(email.Attachments, Attachment{
			Filename:    att.Name,
			ContentType: att.ContentType,
			Size:        att.Size,
			Inline:      att.IsInline,
		})
	}

	return email
}
