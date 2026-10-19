package mail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

// LabelGate is supplied by the caller's policy, never by the message or a model.
// Inbox membership is always required, independently of the other predicates.
type LabelGate struct {
	RequireUnread bool       `json:"requireUnread"`
	RequireAll    []string   `json:"requireAll,omitempty"`
	RequireAny    [][]string `json:"requireAny,omitempty"`
	Forbid        []string   `json:"forbid,omitempty"`
	Exclusive     [][]string `json:"exclusive,omitempty"`
}

type LabelRequest struct {
	Account        string    `json:"account"`
	Message        string    `json:"message"`
	ContentVersion string    `json:"contentVersion"`
	OperationID    string    `json:"operationId"`
	Dimension      []string  `json:"dimension"`
	Category       string    `json:"category"`
	Gate           LabelGate `json:"gate"`
}

type LabelReceipt struct {
	Protocol   string         `json:"protocol"`
	Request    LabelRequest   `json:"request"`
	Outcome    string         `json:"outcome"`
	Reason     string         `json:"reason,omitempty"`
	Before     []string       `json:"before,omitempty"`
	Categories []string       `json:"categories,omitempty"`
	Provider   *WorkflowReply `json:"provider,omitempty"`
}

type LabelSnapshot struct {
	Protocol       string          `json:"protocol"`
	Account        string          `json:"account"`
	Message        json.RawMessage `json:"message"`
	ContentVersion string          `json:"contentVersion"`
	Inbox          string          `json:"inbox"`
}

type labelMessage struct {
	ID         string    `json:"id"`
	ChangeKey  string    `json:"changeKey"`
	Folder     string    `json:"parentFolderId"`
	IsRead     *bool     `json:"isRead"`
	Categories *[]string `json:"categories"`
	Body       struct {
		Content *string `json:"content"`
	} `json:"body"`
	BodyTruncated bool `json:"bodyTruncated"`
}

func validOperationID(value string) bool {
	return value != "" && len(value) <= 160 && !strings.ContainsAny(value, "\x00\r\n")
}

func (g LabelGate) Validate() error {
	for _, names := range [][]string{g.RequireAll, g.Forbid} {
		if names != nil && validCategories(names) != nil {
			return errors.New("invalid label gate names")
		}
	}
	if len(g.RequireAny) > 25 || len(g.Exclusive) > 25 {
		return errors.New("too many label gate groups")
	}
	for _, groups := range [][][]string{g.RequireAny, g.Exclusive} {
		for _, group := range groups {
			if len(group) == 0 || validCategories(group) != nil {
				return errors.New("invalid label gate group")
			}
		}
	}
	return nil
}

func (r LabelRequest) Validate() error {
	if err := (WorkflowRequest{Operation: "read", Account: r.Account, Message: r.Message}).Validate(); err != nil {
		return err
	}
	if !validOperationID(r.OperationID) {
		return errors.New("a stable operation id is required")
	}
	if !validDigest(r.ContentVersion, "assessment-v1:") {
		return errors.New("a complete label-snapshot assessment-v1 digest is required")
	}
	if len(r.Dimension) == 0 || validCategories(r.Dimension) != nil || !containsLabel(r.Dimension, r.Category) {
		return errors.New("category must be one of the declared dimension's unique names")
	}
	return r.Gate.Validate()
}

func validDigest(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(decoded) == 32
}

func parseLabelMessage(raw json.RawMessage) (*labelMessage, error) {
	var message labelMessage
	if json.Unmarshal(raw, &message) != nil || !validID(message.ID) || !validVersion(message.ChangeKey) || !validID(message.Folder) || message.IsRead == nil || message.Categories == nil || validCategories(*message.Categories) != nil || message.Body.Content == nil || message.BodyTruncated {
		return nil, errors.New("message lacks complete identity, unread state, categories or full body")
	}
	return &message, nil
}

// AssessmentContentVersion binds the full returned assessment data. ChangeKey,
// categories, unread state and folder are fresh eligibility, not assessment data.
func AssessmentContentVersion(raw json.RawMessage) (string, error) {
	if _, err := parseLabelMessage(raw); err != nil {
		return "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", err
	}
	for _, name := range []string{"changeKey", "categories", "isRead", "parentFolderId", "@odata.etag"} {
		delete(fields, name)
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return "assessment-v1:" + hex.EncodeToString(hash[:]), nil
}

func inboxID(ctx context.Context, client *graph.Client, account string) (string, error) {
	bounded := *client
	bounded.Context = ctx
	raw, err := bounded.DoRequest(http.MethodGet, graph.GraphAPIBaseURL+"/users/"+url.PathEscape(account)+"/mailFolders/inbox?$select=id", nil)
	var folder struct {
		ID string `json:"id"`
	}
	if err != nil || json.Unmarshal(raw, &folder) != nil || !validID(folder.ID) {
		return "", errors.New("account Inbox could not be verified")
	}
	return folder.ID, nil
}

func ReadLabelSnapshot(ctx context.Context, client *graph.Client, account, message string) (*LabelSnapshot, error) {
	read, err := Workflow(ctx, client, WorkflowRequest{Operation: "read", Account: account, Message: message})
	if err != nil {
		return nil, err
	}
	if read.Outcome != "confirmed" || len(read.Items) != 1 {
		return nil, errors.New("complete message could not be read")
	}
	version, err := AssessmentContentVersion(read.Items[0])
	if err != nil {
		return nil, err
	}
	inbox, err := inboxID(ctx, client, account)
	if err != nil {
		return nil, err
	}
	return &LabelSnapshot{Protocol: "o365-labels/v1", Account: account, Message: read.Items[0], ContentVersion: version, Inbox: inbox}, nil
}

func containsLabel(names []string, name string) bool {
	for _, present := range names {
		if strings.EqualFold(present, name) {
			return true
		}
	}
	return false
}
func labelCount(names, group []string) int {
	count := 0
	for _, name := range group {
		if containsLabel(names, name) {
			count++
		}
	}
	return count
}
func (g LabelGate) check(message *labelMessage, inbox string) (string, string) {
	names := *message.Categories
	for _, group := range g.Exclusive {
		if labelCount(names, group) > 1 {
			return "conflict", "mutually exclusive categories are present"
		}
	}
	if message.Folder != inbox {
		return "skipped", "message is no longer in the account Inbox"
	}
	if g.RequireUnread && *message.IsRead {
		return "skipped", "message is now read"
	}
	if labelCount(names, g.RequireAll) != len(g.RequireAll) {
		return "skipped", "required category is missing"
	}
	for _, group := range g.RequireAny {
		if labelCount(names, group) == 0 {
			return "skipped", "required category group is missing"
		}
	}
	if labelCount(names, g.Forbid) > 0 {
		return "skipped", "a veto category is present"
	}
	return "", ""
}

func sameLabelRequest(left, right LabelRequest) bool {
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return bytes.Equal(l, r)
}

func (j *archiveJournal) labelPrevious(request LabelRequest) (*LabelReceipt, error) {
	if previous := j.labels[request.OperationID]; previous != nil {
		if !sameLabelRequest(previous.Request, request) {
			return nil, errors.New("operation id belongs to a different label request")
		}
		return j.resolvedLabel(previous), nil
	}
	if err := j.messageHold(request.Message); err != nil {
		return nil, err
	}
	return nil, nil
}

func (j *archiveJournal) messageHold(message string) error {
	for _, receipt := range j.labels {
		if receipt.Request.Message == message && j.resolvedLabel(receipt).Outcome == "unknown" {
			return errors.New("an earlier label attempt awaits reconciliation")
		}
	}
	for _, receipt := range j.entries {
		if receipt.Request.Message == message && j.resolvedReceipt(receipt).Outcome == "unknown" {
			return errors.New("an earlier archive attempt awaits reconciliation")
		}
	}
	for _, record := range j.history {
		if record.Message == message && (record.State == "unknown" || record.State == "consumed") && j.resolutions[resolutionKey(record.Source, record.OperationID)].Outcome != "notApplied" {
			return errors.New("historical consumed or uncertain archive work holds this message")
		}
	}
	return nil
}

func (j *archiveJournal) saveLabel(receipt *LabelReceipt) error {
	j.labels[receipt.Request.OperationID] = receipt
	return j.persist()
}

func DecideLabel(ctx context.Context, client *graph.Client, root string, request LabelRequest) (*LabelReceipt, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if client.ReadOnly {
		return nil, errors.New("read-only connection cannot change categories")
	}
	result := &LabelReceipt{Protocol: "o365-labels/v1", Request: request, Outcome: "notApplied"}
	journal, err := openArchiveJournal(root, request.Account)
	if errors.Is(err, ErrMailboxBusy) {
		result.Outcome = "busy"
		result.Reason = err.Error()
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer journal.close()
	if previous, err := journal.labelPrevious(request); previous != nil || err != nil {
		return previous, err
	}
	snapshot, err := ReadLabelSnapshot(ctx, client, request.Account, request.Message)
	if err != nil {
		result.Reason = "preflight failed: " + err.Error()
		return result, nil
	}
	message, err := parseLabelMessage(snapshot.Message)
	if err != nil {
		return nil, err
	}
	result.Before = append([]string{}, (*message.Categories)...)
	if outcome, reason := request.Gate.check(message, snapshot.Inbox); outcome != "" {
		result.Outcome = outcome
		result.Reason = reason
		return result, nil
	}
	present := labelCount(*message.Categories, request.Dimension)
	if present > 1 {
		result.Outcome = "conflict"
		result.Reason = "multiple categories in the owned dimension"
		return result, nil
	}
	if present == 1 {
		result.Outcome = "skipped"
		result.Reason = "dimension already has a decision"
		return result, nil
	}
	if snapshot.ContentVersion != request.ContentVersion {
		result.Outcome = "deferred"
		result.Reason = "assessed content changed"
		return result, nil
	}
	result.Categories = append(append([]string{}, (*message.Categories)...), request.Category)
	if validCategories(result.Categories) != nil {
		result.Reason = "merged category list is invalid or exceeds the bound"
		return result, nil
	}
	if ctx.Err() != nil {
		result.Reason = "cancelled before dispatch"
		return result, nil
	}
	result.Outcome = "unknown"
	result.Reason = "dispatch intent has no confirmed readback"
	if err := journal.saveLabel(result); err != nil {
		return nil, err
	}
	written, writeErr := Workflow(ctx, client, WorkflowRequest{Operation: "categorize", Account: request.Account, Message: request.Message, Version: message.ChangeKey, Categories: result.Categories})
	result.Provider = written
	if writeErr != nil {
		result.Reason = fmt.Sprintf("provider invocation failed: %v", writeErr)
	} else if written.Outcome == "notApplied" {
		result.Outcome = "notApplied"
		result.Reason = written.Reason
	}
	if result.Outcome != "notApplied" {
		after, readErr := ReadLabelSnapshot(ctx, client, request.Account, request.Message)
		if readErr == nil {
			observed, parseErr := parseLabelMessage(after.Message)
			if parseErr == nil && after.ContentVersion == request.ContentVersion && observed.Folder == message.Folder && sameSet(*observed.Categories, result.Categories) {
				result.Outcome = "confirmed"
				result.Reason = ""
			} else {
				result.Reason = "readback did not confirm the exact merged categories and assessed content"
			}
		} else {
			result.Reason = "provider write has no confirmed readback"
		}
	}
	if err := journal.saveLabel(result); err != nil {
		return nil, err
	}
	return result, nil
}
