package mail

import (
	"encoding/json"
	"fmt"

	"github.com/yourname/o365-cli/internal/graph"
)

// MessageRule represents an Outlook inbox rule
type MessageRule struct {
	ID          string                 `json:"id,omitempty"`
	DisplayName string                 `json:"displayName"`
	Sequence    int                    `json:"sequence,omitempty"`
	IsEnabled   bool                   `json:"isEnabled"`
	IsReadOnly  bool                   `json:"isReadOnly,omitempty"`
	Conditions  *MessageRulePredicates `json:"conditions,omitempty"`
	Actions     *MessageRuleActions    `json:"actions,omitempty"`
	Exceptions  *MessageRulePredicates `json:"exceptions,omitempty"`
}

// MessageRulePredicates contains conditions for matching messages
type MessageRulePredicates struct {
	// String matching
	SubjectContains       []string                       `json:"subjectContains,omitempty"`
	BodyContains          []string                       `json:"bodyContains,omitempty"`
	SenderContains        []string                       `json:"senderContains,omitempty"`
	RecipientContains     []string                       `json:"recipientContains,omitempty"`
	HeaderContains        []string                       `json:"headerContains,omitempty"`
	BodyOrSubjectContains []string                       `json:"bodyOrSubjectContains,omitempty"`
	FromAddresses         []graph.GraphEmailAddressWrapper `json:"fromAddresses,omitempty"`
	SentToAddresses       []graph.GraphEmailAddressWrapper `json:"sentToAddresses,omitempty"`
	// Boolean conditions
	HasAttachments         *bool `json:"hasAttachments,omitempty"`
	IsAutomaticForward     *bool `json:"isAutomaticForward,omitempty"`
	IsAutomaticReply       *bool `json:"isAutomaticReply,omitempty"`
	IsEncrypted            *bool `json:"isEncrypted,omitempty"`
	IsMeetingRequest       *bool `json:"isMeetingRequest,omitempty"`
	IsMeetingResponse      *bool `json:"isMeetingResponse,omitempty"`
	IsNonDeliveryReport    *bool `json:"isNonDeliveryReport,omitempty"`
	IsPermissionControlled *bool `json:"isPermissionControlled,omitempty"`
	IsReadReceipt          *bool `json:"isReadReceipt,omitempty"`
	IsSigned               *bool `json:"isSigned,omitempty"`
	IsVoicemail            *bool `json:"isVoicemail,omitempty"`
	SentOnlyToMe           *bool `json:"sentOnlyToMe,omitempty"`
	SentToMe               *bool `json:"sentToMe,omitempty"`
	SentCcMe               *bool `json:"sentCcMe,omitempty"`
	SentToOrCcMe           *bool `json:"sentToOrCcMe,omitempty"`
	// Enum conditions
	Importance        string `json:"importance,omitempty"`
	MessageActionFlag string `json:"messageActionFlag,omitempty"`
	Sensitivity       string `json:"sensitivity,omitempty"`
	// Range conditions
	WithinSizeRange *SizeRange `json:"withinSizeRange,omitempty"`
}

// SizeRange represents a size range for message filtering
type SizeRange struct {
	MinimumSize int `json:"minimumSize,omitempty"`
	MaximumSize int `json:"maximumSize,omitempty"`
}

// MessageRuleActions contains actions to perform on matching messages
type MessageRuleActions struct {
	AssignCategories      []string                       `json:"assignCategories,omitempty"`
	CopyToFolder          string                         `json:"copyToFolder,omitempty"`
	Delete                *bool                          `json:"delete,omitempty"`
	ForwardAsAttachmentTo []graph.GraphEmailAddressWrapper `json:"forwardAsAttachmentTo,omitempty"`
	ForwardTo             []graph.GraphEmailAddressWrapper `json:"forwardTo,omitempty"`
	MarkAsRead            *bool                          `json:"markAsRead,omitempty"`
	MarkImportance        string                         `json:"markImportance,omitempty"`
	MoveToFolder          string                         `json:"moveToFolder,omitempty"`
	PermanentDelete       *bool                          `json:"permanentDelete,omitempty"`
	RedirectTo            []graph.GraphEmailAddressWrapper `json:"redirectTo,omitempty"`
	StopProcessingRules   *bool                          `json:"stopProcessingRules,omitempty"`
}

// GraphRulesResponse represents the list response for message rules
type GraphRulesResponse struct {
	Value    []MessageRule `json:"value"`
	NextLink string        `json:"@odata.nextLink"`
}

// ListRules lists all inbox message rules
func (c *Client) ListRules() ([]MessageRule, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/inbox/messageRules", graph.GraphAPIBaseURL)

	resp, err := c.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result GraphRulesResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Value, nil
}

// GetRule gets a specific inbox message rule
func (c *Client) GetRule(ruleID string) (*MessageRule, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/inbox/messageRules/%s", graph.GraphAPIBaseURL, ruleID)

	resp, err := c.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var rule MessageRule
	if err := json.Unmarshal(resp, &rule); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &rule, nil
}

// CreateRule creates a new inbox message rule
func (c *Client) CreateRule(rule *MessageRule) (*MessageRule, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/inbox/messageRules", graph.GraphAPIBaseURL)

	jsonBody, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule: %w", err)
	}

	resp, err := c.DoRequest("POST", endpoint, jsonBody)
	if err != nil {
		return nil, err
	}

	var created MessageRule
	if err := json.Unmarshal(resp, &created); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &created, nil
}

// UpdateRule updates an existing inbox message rule
func (c *Client) UpdateRule(ruleID string, updates *MessageRule) (*MessageRule, error) {
	endpoint := fmt.Sprintf("%s/me/mailFolders/inbox/messageRules/%s", graph.GraphAPIBaseURL, ruleID)

	jsonBody, err := json.Marshal(updates)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updates: %w", err)
	}

	resp, err := c.DoRequest("PATCH", endpoint, jsonBody)
	if err != nil {
		return nil, err
	}

	var updated MessageRule
	if err := json.Unmarshal(resp, &updated); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &updated, nil
}

// DeleteRule deletes an inbox message rule
func (c *Client) DeleteRule(ruleID string) error {
	endpoint := fmt.Sprintf("%s/me/mailFolders/inbox/messageRules/%s", graph.GraphAPIBaseURL, ruleID)

	_, err := c.DoRequest("DELETE", endpoint, nil)
	return err
}

// EnableRule enables an inbox message rule
func (c *Client) EnableRule(ruleID string) (*MessageRule, error) {
	return c.UpdateRule(ruleID, &MessageRule{IsEnabled: true})
}

// DisableRule disables an inbox message rule
func (c *Client) DisableRule(ruleID string) (*MessageRule, error) {
	return c.UpdateRule(ruleID, &MessageRule{IsEnabled: false})
}

// ToEmailAddressWrapper creates email address wrapper from string
func ToEmailAddressWrapper(address string) graph.GraphEmailAddressWrapper {
	return graph.GraphEmailAddressWrapper{
		EmailAddress: graph.GraphEmailAddress{Address: graph.ParseEmail(address)},
	}
}

// ToEmailAddressWrappers creates multiple email address wrappers
func ToEmailAddressWrappers(addresses []string) []graph.GraphEmailAddressWrapper {
	result := make([]graph.GraphEmailAddressWrapper, len(addresses))
	for i, addr := range addresses {
		result[i] = ToEmailAddressWrapper(addr)
	}
	return result
}

// BoolPtr returns a pointer to a bool value
func BoolPtr(b bool) *bool {
	return &b
}
