package mail

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/yourname/o365-cli/internal/graph"
)

type MessageCategories struct {
	MessageID  string   `json:"message_id"`
	Categories []string `json:"categories"`
}

type CategoryChange struct {
	Account    string   `json:"account,omitempty"`
	MessageID  string   `json:"message_id"`
	Before     []string `json:"before"`
	Categories []string `json:"categories"`
	Changed    bool     `json:"changed"`
	Applied    bool     `json:"applied"`
	DryRun     bool     `json:"dry_run"`
}

func categoryEndpoint(messageID string) (string, error) {
	if !validID(messageID) {
		return "", fmt.Errorf("a valid message ID is required")
	}
	return graph.GraphAPIBaseURL + "/me/messages/" + url.PathEscape(messageID), nil
}

func decodeMessageCategories(data []byte, messageID string) (*MessageCategories, error) {
	var response struct {
		ID         string    `json:"id"`
		Categories *[]string `json:"categories"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("invalid category response: %w", err)
	}
	if response.ID != messageID || response.Categories == nil {
		return nil, fmt.Errorf("category response has a different message ID or missing categories")
	}
	return &MessageCategories{MessageID: response.ID, Categories: *response.Categories}, nil
}

func (c *Client) GetMessageCategories(messageID string) (*MessageCategories, error) {
	endpoint, err := categoryEndpoint(messageID)
	if err != nil {
		return nil, err
	}
	data, err := c.DoRequest(http.MethodGet, endpoint+"?$select=id,categories", nil)
	if err != nil {
		return nil, err
	}
	return decodeMessageCategories(data, messageID)
}

func categoryNames(names []string) ([]string, error) {
	result := []string{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "\r\n\x00") {
			return nil, fmt.Errorf("category names must be nonempty and contain no line breaks or NUL bytes")
		}
		if !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	return result, nil
}

func ValidateCategoryChange(operation string, names []string) error {
	switch operation {
	case "add", "remove", "set":
		if len(names) == 0 {
			return fmt.Errorf("%s requires at least one category", operation)
		}
	case "clear":
		if len(names) != 0 {
			return fmt.Errorf("clear does not accept category names")
		}
	default:
		return fmt.Errorf("unknown category operation %q", operation)
	}
	_, err := categoryNames(names)
	return err
}

func changedCategories(before []string, operation string, names []string) []string {
	if operation == "set" {
		return names
	}
	result := []string{}
	if operation == "clear" {
		return result
	}
	for _, name := range before {
		if operation == "remove" && slices.Contains(names, name) {
			continue
		}
		if !slices.Contains(result, name) {
			result = append(result, name)
		}
	}
	if operation == "add" {
		for _, name := range names {
			if !slices.Contains(result, name) {
				result = append(result, name)
			}
		}
	}
	return result
}

func sameCategories(a, b []string) bool {
	left, right := slices.Clone(a), slices.Clone(b)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func (c *Client) ChangeMessageCategories(messageID, operation string, names []string, dryRun bool) (*CategoryChange, error) {
	if err := ValidateCategoryChange(operation, names); err != nil {
		return nil, err
	}
	names, err := categoryNames(names)
	if err != nil {
		return nil, err
	}
	current, err := c.GetMessageCategories(messageID)
	if err != nil {
		return nil, err
	}
	next := changedCategories(current.Categories, operation, names)
	result := &CategoryChange{MessageID: messageID, Before: current.Categories, Categories: next, Changed: !sameCategories(current.Categories, next), DryRun: dryRun}
	if dryRun || !result.Changed {
		return result, nil
	}
	body, err := json.Marshal(struct {
		Categories []string `json:"categories"`
	}{next})
	if err != nil {
		return nil, err
	}
	endpoint, err := categoryEndpoint(messageID)
	if err != nil {
		return nil, err
	}
	data, err := c.DoRequest(http.MethodPatch, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("category update failed; read the current categories before retrying: %w", err)
	}
	updated, err := decodeMessageCategories(data, messageID)
	if err != nil {
		return nil, fmt.Errorf("category update response could not be verified; read current categories: %w", err)
	}
	if !sameCategories(updated.Categories, next) {
		return nil, fmt.Errorf("category update returned unexpected categories; read current categories")
	}
	result.Categories = updated.Categories
	result.Applied = true
	return result, nil
}
