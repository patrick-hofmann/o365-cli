package mail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/yourname/o365-cli/internal/graph"
)

// ArchiveRequest is the complete reviewed object. Every field belongs in the
// exact command approved by ape-shell; display text never supplies authority.
type ArchiveRequest struct {
	Account        string `json:"account"`
	Message        string `json:"message"`
	ContentVersion string `json:"contentVersion"`
	Sender         string `json:"sender"`
	Subject        string `json:"subject"`
	Folder         string `json:"folder"`
	Destination    string `json:"destination"`
	OperationID    string `json:"operationId"`
}

type ArchiveReceipt struct {
	Protocol string         `json:"protocol"`
	Request  ArchiveRequest `json:"request"`
	Outcome  string         `json:"outcome"`
	Reason   string         `json:"reason,omitempty"`
	Provider *WorkflowReply `json:"provider,omitempty"`
}

type archiveMessage struct {
	ID        string            `json:"id"`
	ChangeKey string            `json:"changeKey"`
	Folder    string            `json:"parentFolderId"`
	From      json.RawMessage   `json:"from"`
	To        []json.RawMessage `json:"toRecipients"`
	CC        []json.RawMessage `json:"ccRecipients"`
	Subject   *string           `json:"subject"`
	Received  *string           `json:"receivedDateTime"`
	Body      struct {
		Content *string `json:"content"`
	} `json:"body"`
	Attachments bool `json:"hasAttachments"`
}

func mailAddress(raw json.RawMessage) string {
	var value struct {
		EmailAddress struct {
			Address string `json:"address"`
		} `json:"emailAddress"`
	}
	_ = json.Unmarshal(raw, &value)
	return strings.ToLower(strings.TrimSpace(value.EmailAddress.Address))
}

func mailAddresses(raw []json.RawMessage) []string {
	result := make([]string, len(raw))
	for i, value := range raw {
		result[i] = mailAddress(value)
	}
	return result
}

func displayMail(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r <= 0x1f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return ' '
		}
		return r
	}, value)
	units := utf16.Encode([]rune(value))
	if len(units) > limit {
		units = units[:limit]
	}
	return string(utf16.Decode(units))
}

// Content versions intentionally ignore read, category, flag and changeKey
// changes. The v2 tag specifies Go JSON encoding (HTML escaping disabled), so
// historical JavaScript content hashes cannot silently authorize new work.
func ArchiveContentVersion(raw json.RawMessage) (string, error) {
	var message archiveMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return "", err
	}
	if message.ID == "" || message.ChangeKey == "" || message.Folder == "" {
		return "", errors.New("incomplete message identity")
	}
	fields := []any{mailAddress(message.From), mailAddresses(message.To), mailAddresses(message.CC), message.Subject, message.Received, message.Body.Content, message.Attachments}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(fields); err != nil {
		return "", err
	}
	hash := sha256.Sum256(bytes.TrimSuffix(body.Bytes(), []byte("\n")))
	return "content-v2:" + hex.EncodeToString(hash[:]), nil
}

func (r ArchiveRequest) Validate() error {
	if err := (WorkflowRequest{Operation: "move", Account: r.Account, Message: r.Message, Version: "preflight", Folder: r.Folder, Destination: r.Destination}).Validate(); err != nil {
		return err
	}
	if !strings.HasPrefix(r.ContentVersion, "content-v2:") || len(r.ContentVersion) != len("content-v2:")+64 {
		return errors.New("archive requires a reviewed content-v2 digest")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(r.ContentVersion, "content-v2:"))
	if err != nil || len(decoded) != 32 || strings.ToLower(r.ContentVersion) != r.ContentVersion {
		return errors.New("invalid content version")
	}
	if r.OperationID == "" || len(r.OperationID) > 160 || strings.ContainsAny(r.OperationID, "\x00\r\n") {
		return errors.New("archive requires a stable operation id")
	}
	if r.Sender != displayMail(r.Sender, 120) || r.Subject != displayMail(r.Subject, 160) {
		return errors.New("invalid reviewed sender or subject")
	}
	return nil
}

// ProtectedArchive holds one mailbox-wide journal lock across preflight and
// dispatch. Intent is fsynced before the provider call; crashes and ambiguous
// replies remain blocked across invocations, versions and Pod identities.
func ProtectedArchive(ctx context.Context, client *graph.Client, journalRoot string, request ArchiveRequest) (*ArchiveReceipt, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	journal, err := openArchiveJournal(journalRoot, request.Account)
	if err != nil {
		return nil, err
	}
	defer journal.close()
	if previous, err := journal.previous(request); previous != nil || err != nil {
		return previous, err
	}
	result := &ArchiveReceipt{Protocol: "o365-archive/v1", Request: request, Outcome: "notApplied"}
	before, err := Workflow(ctx, client, WorkflowRequest{Operation: "read", Account: request.Account, Message: request.Message})
	if err != nil || before.Outcome != "confirmed" || len(before.Items) != 1 {
		result.Reason = "preflight read failed"
		return result, nil
	}
	var message archiveMessage
	version, err := ArchiveContentVersion(before.Items[0])
	if json.Unmarshal(before.Items[0], &message) != nil || err != nil || message.ID != request.Message || message.Folder != request.Folder || version != request.ContentVersion {
		result.Reason = "message identity, folder or content changed after review"
		return result, nil
	}
	subject := ""
	if message.Subject != nil {
		subject = *message.Subject
	}
	if displayMail(mailAddress(message.From), 120) != request.Sender || displayMail(subject, 160) != request.Subject {
		result.Reason = "message no longer matches the reviewed sender and subject"
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		result.Reason = "cancelled before dispatch"
		return result, nil
	}
	result.Outcome = "unknown"
	result.Reason = "dispatch intent has no confirmed receipt"
	if err := journal.save(result); err != nil {
		return nil, err
	}
	// Workflow repeats the exact changeKey/folder preflight, performs one POST,
	// verifies the account Archive folder and retains the provider request id.
	moved, err := Workflow(ctx, client, WorkflowRequest{Operation: "move", Account: request.Account, Message: request.Message, Version: message.ChangeKey, Folder: message.Folder, Destination: request.Destination})
	if err != nil {
		result.Reason = fmt.Sprintf("provider invocation failed: %v", err)
	} else {
		result.Provider = moved
		result.Reason = moved.Reason
		switch moved.Outcome {
		case "confirmed":
			result.Outcome = "archived"
		case "notApplied":
			result.Outcome = "notApplied"
		}
	}
	if err := journal.save(result); err != nil {
		return nil, err
	}
	return result, nil
}

// ReviewArchive returns the canonical object shown in an exact approval. It
// creates no authorization and does not mutate the journal or mailbox.
func ReviewArchive(raw json.RawMessage, account, operation string) (*ArchiveRequest, error) {
	version, err := ArchiveContentVersion(raw)
	if err != nil {
		return nil, err
	}
	var message archiveMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	subject := ""
	if message.Subject != nil {
		subject = *message.Subject
	}
	request := &ArchiveRequest{Account: account, Message: message.ID, ContentVersion: version, Sender: displayMail(mailAddress(message.From), 120), Subject: displayMail(subject, 160), Folder: message.Folder, Destination: "archive", OperationID: operation}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return request, nil
}
