package mail

import (
	"context"
	"errors"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

// ArchiveLabelled moves one Inbox message to Archive only while it carries the required label and its assessed content is unchanged.
// A message that is no longer in the Inbox returns skipped, so an interrupted run can simply repeat.
func ArchiveLabelled(ctx context.Context, client *graph.Client, lockRoot, account, message, label, contentVersion string) (*WorkflowReply, error) {
	result := &WorkflowReply{Protocol: "pods-mail/v1", Account: account, Operation: "archive-labelled", Outcome: "notApplied"}
	if err := (WorkflowRequest{Operation: "read", Account: account, Message: message}).Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(label) == "" || len(label) > 255 {
		return nil, errors.New("--label must name the category that permits archiving")
	}
	if !strings.HasPrefix(contentVersion, "assessment-v1:") && !strings.HasPrefix(contentVersion, "assessment-v2:") {
		return nil, errors.New("--content-version must be the assessment-v1 or assessment-v2 digest the decision was made on")
	}
	if client.ReadOnly {
		return nil, errors.New("read-only connection cannot move messages")
	}
	path, err := mailboxPath(lockRoot, account)
	if err != nil {
		return nil, err
	}
	lock, err := lockMailbox(path)
	if errors.Is(err, ErrMailboxBusy) {
		result.Outcome, result.Reason = "busy", err.Error()
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	snapshot, err := ReadLabelSnapshot(ctx, client, account, message)
	if err != nil {
		result.Reason = "preflight failed: " + err.Error()
		return result, nil
	}
	current, err := parseLabelMessage(snapshot.Message)
	if err != nil {
		return nil, err
	}
	switch {
	case current.Folder != snapshot.Inbox:
		result.Outcome, result.Reason = "skipped", "message is no longer in the Inbox"
		return result, nil
	case !contains(*current.Categories, label):
		result.Reason = "message does not carry the required label"
		return result, nil
	case contentVersion != snapshot.versionLike(contentVersion):
		result.Outcome, result.Reason = "deferred", "assessed content changed"
		return result, nil
	}
	moved, err := Workflow(ctx, client, WorkflowRequest{Operation: "move", Account: account, Message: message, Version: current.ChangeKey, Folder: snapshot.Inbox, Destination: "archive"})
	if err != nil {
		return nil, err
	}
	moved.Operation = "archive-labelled"
	return moved, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
