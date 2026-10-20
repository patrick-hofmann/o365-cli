package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yourname/o365-cli/internal/graph"
)

// AddCategory uses the current category set as its only idempotency state.
func AddCategory(ctx context.Context, client *graph.Client, account, message, category string) (*WorkflowReply, error) {
	request := WorkflowRequest{Operation: "read", Account: account, Message: message}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := validCategories([]string{category}); err != nil {
		return nil, err
	}
	if client.ReadOnly {
		return nil, errors.New("read-only connection cannot change categories")
	}
	before, err := Workflow(ctx, client, request)
	if err != nil {
		return nil, fmt.Errorf("add-category read: %w", err)
	}
	before.Operation = "add-category"
	if before.Outcome != "confirmed" {
		before.Reason = "add-category read: " + before.Reason
		return before, nil
	}
	var current struct {
		ID         string    `json:"id"`
		Version    string    `json:"changeKey"`
		Categories *[]string `json:"categories"`
	}
	if len(before.Items) != 1 || json.Unmarshal(before.Items[0], &current) != nil || current.ID != message || !validVersion(current.Version) || current.Categories == nil {
		return nil, errors.New("add-category read: missing message identity, version or categories")
	}
	for _, existing := range *current.Categories {
		if existing == category {
			return before, nil
		}
	}
	request.Operation = "categorize"
	request.Version = current.Version
	request.Categories = append(append([]string{}, (*current.Categories)...), category)
	result, err := Workflow(ctx, client, request)
	if err != nil {
		return nil, fmt.Errorf("add-category write: %w", err)
	}
	result.Operation = "add-category"
	return result, nil
}
