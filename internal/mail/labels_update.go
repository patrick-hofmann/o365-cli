package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

// LabelUpdate removes and adds named categories in one write. The resulting category
// set is its only idempotency state, so a repeated or resumed update writes nothing twice.
type LabelUpdate struct {
	Account        string   `json:"account"`
	Message        string   `json:"message"`
	ContentVersion string   `json:"contentVersion,omitempty"`
	Remove         []string `json:"remove,omitempty"`
	Add            []string `json:"add,omitempty"`
}

type LabelUpdateReceipt struct {
	Protocol   string         `json:"protocol"`
	Request    LabelUpdate    `json:"request"`
	Outcome    string         `json:"outcome"`
	Reason     string         `json:"reason,omitempty"`
	Before     []string       `json:"before,omitempty"`
	Categories []string       `json:"categories,omitempty"`
	Provider   *WorkflowReply `json:"provider,omitempty"`
}

func (u LabelUpdate) Validate() error {
	if err := (WorkflowRequest{Operation: "read", Account: u.Account, Message: u.Message}).Validate(); err != nil {
		return err
	}
	if len(u.Remove) == 0 && len(u.Add) == 0 {
		return errors.New("at least one --remove or --add category is required")
	}
	for _, names := range [][]string{u.Remove, u.Add} {
		if len(names) > 0 && validCategories(names) != nil {
			return errors.New("category names are unique, trimmed and contain no line breaks or commas")
		}
	}
	for _, name := range u.Add {
		if containsLabel(u.Remove, name) {
			return fmt.Errorf("category %q cannot be removed and added at once", name)
		}
	}
	if u.ContentVersion != "" && !validDigest(u.ContentVersion, "assessment-v1:") && !validDigest(u.ContentVersion, "assessment-v2:") {
		return errors.New("content version must be a complete assessment-v1 or assessment-v2 digest")
	}
	return nil
}

// UpdateLabels writes (current − remove) ∪ add with one PATCH while holding the shared mailbox lock.
func UpdateLabels(ctx context.Context, client *graph.Client, lockRoot string, update LabelUpdate) (*LabelUpdateReceipt, error) {
	if err := update.Validate(); err != nil {
		return nil, err
	}
	if client.ReadOnly {
		return nil, errors.New("read-only connection cannot change categories")
	}
	path, err := mailboxPath(lockRoot, update.Account)
	if err != nil {
		return nil, err
	}
	result := &LabelUpdateReceipt{Protocol: "o365-labels/v2", Request: update, Outcome: "notApplied"}
	lock, err := lockMailbox(path)
	if errors.Is(err, ErrMailboxBusy) {
		result.Outcome, result.Reason = "busy", err.Error()
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer lock.Close()

	snapshot, err := ReadLabelSnapshot(ctx, client, update.Account, update.Message)
	if err != nil {
		result.Reason = "preflight failed: " + err.Error()
		return result, nil
	}
	message, err := parseLabelMessage(snapshot.Message)
	if err != nil {
		return nil, err
	}
	current := *message.Categories
	result.Before = append([]string{}, current...)
	target := updatedCategories(current, update)
	if sameSet(target, current) {
		result.Outcome, result.Categories = "skipped", target
		return result, nil
	}
	if message.Folder != snapshot.Inbox {
		result.Outcome, result.Reason = "deferred", "message is no longer in the account Inbox"
		return result, nil
	}
	if update.ContentVersion != "" && update.ContentVersion != snapshot.versionLike(update.ContentVersion) {
		result.Outcome, result.Reason = "deferred", "assessed content changed"
		return result, nil
	}
	if validCategories(target) != nil {
		result.Reason = "resulting category list is invalid or exceeds 25 names"
		return result, nil
	}
	if ctx.Err() != nil {
		result.Reason = "cancelled before dispatch"
		return result, nil
	}
	written, writeErr := Workflow(ctx, client, WorkflowRequest{Operation: "categorize", Account: update.Account, Message: update.Message, Version: message.ChangeKey, Categories: target})
	result.Provider = written
	if writeErr == nil && written.Outcome == "notApplied" {
		result.Reason = written.Reason
		return result, nil
	}
	result.Categories = target
	result.Outcome, result.Reason = "unknown", "provider write has no confirmed readback"
	if writeErr != nil {
		result.Reason = "provider invocation failed: " + writeErr.Error()
	}
	after, err := ReadLabelSnapshot(ctx, client, update.Account, update.Message)
	if err != nil {
		return result, nil
	}
	observed, err := parseLabelMessage(after.Message)
	if err != nil || observed.Folder != message.Folder || !applied(*observed.Categories, update) {
		result.Reason = "readback did not show the requested labels"
		return result, nil
	}
	result.Outcome, result.Reason, result.Categories = "confirmed", "", *observed.Categories
	return result, nil
}

// AddCategoryLocked is AddCategory under the shared mailbox lock; a busy lock is not dispatched.
func AddCategoryLocked(ctx context.Context, client *graph.Client, lockRoot, account, message, category string) (*WorkflowReply, error) {
	path, err := mailboxPath(lockRoot, account)
	if err != nil {
		return nil, err
	}
	lock, err := lockMailbox(path)
	if errors.Is(err, ErrMailboxBusy) {
		return &WorkflowReply{Protocol: "pods-mail/v1", Account: account, Operation: "add-category", Outcome: "notApplied", Reason: err.Error()}, nil
	}
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	return AddCategory(ctx, client, account, message, category)
}

func (s *LabelSnapshot) versionLike(version string) string {
	if strings.HasPrefix(version, "assessment-v2:") {
		return s.AssessmentVersion
	}
	return s.ContentVersion
}

func updatedCategories(current []string, update LabelUpdate) []string {
	target := []string{}
	for _, name := range current {
		if !containsLabel(update.Remove, name) {
			target = append(target, name)
		}
	}
	for _, name := range update.Add {
		if !containsLabel(target, name) {
			target = append(target, name)
		}
	}
	return target
}

func applied(categories []string, update LabelUpdate) bool {
	for _, name := range update.Add {
		if !containsLabel(categories, name) {
			return false
		}
	}
	for _, name := range update.Remove {
		if containsLabel(categories, name) {
			return false
		}
	}
	return true
}
