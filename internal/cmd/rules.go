package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

var rulesCmd = &cobra.Command{
	Use:   "rules",
	Short: "Manage inbox rules",
	Long:  "Commands for managing Microsoft Graph inbox message rules (server-side rules).",
}

// List Command
var rulesListJSON bool

var rulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List inbox rules",
	Long: `Lists all inbox message rules.

Examples:
  o365-cli rules list
  o365-cli rules list --json`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.read"},
	RunE:        runRulesList,
}

// Get Command
var rulesGetJSON bool

var rulesGetCmd = &cobra.Command{
	Use:   "get [rule-id]",
	Short: "Get inbox rule details",
	Long: `Gets details of a specific inbox rule.

Examples:
  o365-cli rules get AQMkADAwATM0...
  o365-cli rules get AQMkADAwATM0... --json`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.read"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRulesGet,
}

// Create Command
var (
	createName             string
	createDisabled         bool
	createFromContains     []string
	createFromAddresses    []string
	createSubjectContains  []string
	createBodyContains     []string
	createSentToMe         bool
	createSentCcMe         bool
	createHasAttachments   bool
	createImportance       string
	createMoveToFolder     string
	createCopyToFolder     string
	createMarkRead         bool
	createDelete           bool
	createMarkImportance   string
	createForwardTo        []string
	createRedirectTo       []string
	createCategories       []string
	createStopProcessing   bool
	createJSONFile         string
	createOutputJSON       bool
)

var rulesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create inbox rule",
	Long: `Creates a new inbox message rule.

Examples:
  # Create rule to archive newsletters
  o365-cli rules create --name "Archive newsletters" \
    --from-contains "@newsletter.com" \
    --move-to "Archive" \
    --mark-read

  # Create rule from JSON file
  o365-cli rules create --json-file rule.json

  # Create rule to forward emails
  o365-cli rules create --name "Forward important" \
    --subject-contains "urgent" \
    --forward-to manager@example.com`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.manage"},
	RunE:        runRulesCreate,
}

// Update Command
var (
	updateName     string
	updateJSONFile string
	updateJSON     bool
)

var rulesUpdateCmd = &cobra.Command{
	Use:   "update [rule-id]",
	Short: "Update inbox rule",
	Long: `Updates an existing inbox rule.

Examples:
  o365-cli rules update AQMkADAwATM0... --name "New name"
  o365-cli rules update AQMkADAwATM0... --json-file updates.json`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.manage"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRulesUpdate,
}

// Delete Command
var rulesDeleteCmd = &cobra.Command{
	Use:   "delete [rule-id]",
	Short: "Delete inbox rule",
	Long: `Deletes an inbox message rule.

Examples:
  o365-cli rules delete AQMkADAwATM0...`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.manage"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRulesDelete,
}

// Enable Command
var rulesEnableCmd = &cobra.Command{
	Use:   "enable [rule-id]",
	Short: "Enable inbox rule",
	Long: `Enables an inbox message rule.

Examples:
  o365-cli rules enable AQMkADAwATM0...`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.manage"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRulesEnable,
}

// Disable Command
var rulesDisableCmd = &cobra.Command{
	Use:   "disable [rule-id]",
	Short: "Disable inbox rule",
	Long: `Disables an inbox message rule.

Examples:
  o365-cli rules disable AQMkADAwATM0...`,
	Annotations: map[string]string{profile.AnnotationKey: "rules.manage"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRulesDisable,
}

func init() {
	// List flags
	rulesListCmd.Flags().BoolVar(&rulesListJSON, "json", false, "Output as JSON")

	// Get flags
	rulesGetCmd.Flags().BoolVar(&rulesGetJSON, "json", false, "Output as JSON")

	// Create flags
	rulesCreateCmd.Flags().StringVar(&createName, "name", "", "Rule display name")
	rulesCreateCmd.Flags().BoolVar(&createDisabled, "disabled", false, "Create rule as disabled")
	rulesCreateCmd.Flags().StringArrayVar(&createFromContains, "from-contains", nil, "Sender contains strings")
	rulesCreateCmd.Flags().StringArrayVar(&createFromAddresses, "from-addresses", nil, "Exact sender email addresses")
	rulesCreateCmd.Flags().StringArrayVar(&createSubjectContains, "subject-contains", nil, "Subject contains strings")
	rulesCreateCmd.Flags().StringArrayVar(&createBodyContains, "body-contains", nil, "Body contains strings")
	rulesCreateCmd.Flags().BoolVar(&createSentToMe, "sent-to-me", false, "Sent directly to me")
	rulesCreateCmd.Flags().BoolVar(&createSentCcMe, "sent-cc-me", false, "Sent with me in CC")
	rulesCreateCmd.Flags().BoolVar(&createHasAttachments, "has-attachments", false, "Has attachments")
	rulesCreateCmd.Flags().StringVar(&createImportance, "importance", "", "Message importance (low/normal/high)")
	rulesCreateCmd.Flags().StringVar(&createMoveToFolder, "move-to", "", "Move to folder (name or ID)")
	rulesCreateCmd.Flags().StringVar(&createCopyToFolder, "copy-to", "", "Copy to folder (name or ID)")
	rulesCreateCmd.Flags().BoolVar(&createMarkRead, "mark-read", false, "Mark as read")
	rulesCreateCmd.Flags().BoolVar(&createDelete, "delete", false, "Delete message")
	rulesCreateCmd.Flags().StringVar(&createMarkImportance, "mark-importance", "", "Mark importance (low/normal/high)")
	rulesCreateCmd.Flags().StringArrayVar(&createForwardTo, "forward-to", nil, "Forward to addresses")
	rulesCreateCmd.Flags().StringArrayVar(&createRedirectTo, "redirect-to", nil, "Redirect to addresses")
	rulesCreateCmd.Flags().StringArrayVar(&createCategories, "categories", nil, "Assign categories")
	rulesCreateCmd.Flags().BoolVar(&createStopProcessing, "stop-processing", false, "Stop processing more rules")
	rulesCreateCmd.Flags().StringVar(&createJSONFile, "json-file", "", "Create from JSON file")
	rulesCreateCmd.Flags().BoolVar(&createOutputJSON, "output-json", false, "Output result as JSON")

	// Update flags
	rulesUpdateCmd.Flags().StringVar(&updateName, "name", "", "New display name")
	rulesUpdateCmd.Flags().StringVar(&updateJSONFile, "json-file", "", "Update from JSON file")
	rulesUpdateCmd.Flags().BoolVar(&updateJSON, "json", false, "Output result as JSON")

	rulesCmd.AddCommand(rulesListCmd)
	rulesCmd.AddCommand(rulesGetCmd)
	rulesCmd.AddCommand(rulesCreateCmd)
	rulesCmd.AddCommand(rulesUpdateCmd)
	rulesCmd.AddCommand(rulesDeleteCmd)
	rulesCmd.AddCommand(rulesEnableCmd)
	rulesCmd.AddCommand(rulesDisableCmd)
}

func runRulesList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Fetching inbox rules via Graph API")

	rules, err := client.ListRules()
	if err != nil {
		return err
	}

	if rulesListJSON {
		return outputJSON(rules)
	}

	if len(rules) == 0 {
		printInfo("No inbox rules configured.")
		return nil
	}

	fmt.Printf("\nInbox Rules (%d):\n", len(rules))
	fmt.Println(strings.Repeat("─", 70))

	for _, rule := range rules {
		status := "✓"
		if !rule.IsEnabled {
			status = "✗"
		}
		readonly := ""
		if rule.IsReadOnly {
			readonly = " [read-only]"
		}

		fmt.Printf("%s [%d] %s%s\n", status, rule.Sequence, rule.DisplayName, readonly)
		fmt.Printf("  ID: %s\n", rule.ID)

		// Show conditions
		if rule.Conditions != nil {
			conds := formatConditions(rule.Conditions)
			if len(conds) > 0 {
				fmt.Printf("  Conditions: %s\n", strings.Join(conds, "; "))
			}
		}

		// Show actions
		if rule.Actions != nil {
			acts := formatActions(rule.Actions)
			if len(acts) > 0 {
				fmt.Printf("  Actions: %s\n", strings.Join(acts, "; "))
			}
		}

		fmt.Println()
	}

	return nil
}

func runRulesGet(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	ruleID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	rule, err := client.GetRule(ruleID)
	if err != nil {
		return err
	}

	if rulesGetJSON {
		return outputJSON(rule)
	}

	fmt.Printf("\nRule: %s\n", rule.DisplayName)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("ID: %s\n", rule.ID)
	fmt.Printf("Enabled: %v\n", rule.IsEnabled)
	fmt.Printf("Sequence: %d\n", rule.Sequence)
	fmt.Printf("Read-only: %v\n", rule.IsReadOnly)

	if rule.Conditions != nil {
		fmt.Println("\nConditions:")
		condJSON, _ := json.MarshalIndent(rule.Conditions, "  ", "  ")
		fmt.Printf("  %s\n", string(condJSON))
	}

	if rule.Actions != nil {
		fmt.Println("\nActions:")
		actJSON, _ := json.MarshalIndent(rule.Actions, "  ", "  ")
		fmt.Printf("  %s\n", string(actJSON))
	}

	if rule.Exceptions != nil {
		fmt.Println("\nExceptions:")
		excJSON, _ := json.MarshalIndent(rule.Exceptions, "  ", "  ")
		fmt.Printf("  %s\n", string(excJSON))
	}

	return nil
}

func runRulesCreate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	var rule *mail.MessageRule

	if createJSONFile != "" {
		// Load from JSON file
		content, err := os.ReadFile(createJSONFile)
		if err != nil {
			return fmt.Errorf("failed to read JSON file: %w", err)
		}
		rule = &mail.MessageRule{}
		if err := json.Unmarshal(content, rule); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else {
		// Build from flags
		if createName == "" {
			return fmt.Errorf("--name is required (or use --json-file)")
		}

		rule = &mail.MessageRule{
			DisplayName: createName,
			IsEnabled:   !createDisabled,
		}

		// Build conditions
		conditions := &mail.MessageRulePredicates{}
		hasConditions := false

		if len(createFromContains) > 0 {
			conditions.SenderContains = createFromContains
			hasConditions = true
		}
		if len(createFromAddresses) > 0 {
			conditions.FromAddresses = mail.ToEmailAddressWrappers(createFromAddresses)
			hasConditions = true
		}
		if len(createSubjectContains) > 0 {
			conditions.SubjectContains = createSubjectContains
			hasConditions = true
		}
		if len(createBodyContains) > 0 {
			conditions.BodyContains = createBodyContains
			hasConditions = true
		}
		if createSentToMe {
			conditions.SentToMe = mail.BoolPtr(true)
			hasConditions = true
		}
		if createSentCcMe {
			conditions.SentCcMe = mail.BoolPtr(true)
			hasConditions = true
		}
		if createHasAttachments {
			conditions.HasAttachments = mail.BoolPtr(true)
			hasConditions = true
		}
		if createImportance != "" {
			conditions.Importance = createImportance
			hasConditions = true
		}

		if hasConditions {
			rule.Conditions = conditions
		}

		// Build actions
		actions := &mail.MessageRuleActions{}
		hasActions := false

		if createMoveToFolder != "" {
			// Resolve folder name to ID
			folderID, err := client.GetFolderByName(createMoveToFolder)
			if err != nil {
				return fmt.Errorf("failed to resolve folder '%s': %w", createMoveToFolder, err)
			}
			actions.MoveToFolder = folderID
			hasActions = true
		}
		if createCopyToFolder != "" {
			folderID, err := client.GetFolderByName(createCopyToFolder)
			if err != nil {
				return fmt.Errorf("failed to resolve folder '%s': %w", createCopyToFolder, err)
			}
			actions.CopyToFolder = folderID
			hasActions = true
		}
		if createMarkRead {
			actions.MarkAsRead = mail.BoolPtr(true)
			hasActions = true
		}
		if createDelete {
			actions.Delete = mail.BoolPtr(true)
			hasActions = true
		}
		if createMarkImportance != "" {
			actions.MarkImportance = createMarkImportance
			hasActions = true
		}
		if len(createForwardTo) > 0 {
			actions.ForwardTo = mail.ToEmailAddressWrappers(createForwardTo)
			hasActions = true
		}
		if len(createRedirectTo) > 0 {
			actions.RedirectTo = mail.ToEmailAddressWrappers(createRedirectTo)
			hasActions = true
		}
		if len(createCategories) > 0 {
			actions.AssignCategories = createCategories
			hasActions = true
		}
		if createStopProcessing {
			actions.StopProcessingRules = mail.BoolPtr(true)
			hasActions = true
		}

		if hasActions {
			rule.Actions = actions
		}
	}

	debugLog("Creating inbox rule via Graph API")

	created, err := client.CreateRule(rule)
	if err != nil {
		return err
	}

	if createOutputJSON {
		return outputJSON(created)
	}

	printSuccess("Rule created: %s", created.DisplayName)
	printInfo("  ID: %s", created.ID)
	printInfo("  Enabled: %v", created.IsEnabled)

	return nil
}

func runRulesUpdate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	ruleID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	var updates *mail.MessageRule

	if updateJSONFile != "" {
		content, err := os.ReadFile(updateJSONFile)
		if err != nil {
			return fmt.Errorf("failed to read JSON file: %w", err)
		}
		updates = &mail.MessageRule{}
		if err := json.Unmarshal(content, updates); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else if updateName != "" {
		updates = &mail.MessageRule{DisplayName: updateName}
	} else {
		return fmt.Errorf("provide --name or --json-file for updates")
	}

	debugLog("Updating inbox rule via Graph API")

	updated, err := client.UpdateRule(ruleID, updates)
	if err != nil {
		return err
	}

	if updateJSON {
		return outputJSON(updated)
	}

	printSuccess("Rule updated: %s", updated.DisplayName)
	printInfo("  ID: %s", updated.ID)

	return nil
}

func runRulesDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	ruleID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Deleting inbox rule via Graph API")

	if err := client.DeleteRule(ruleID); err != nil {
		return err
	}

	printSuccess("Rule deleted: %s", ruleID)
	return nil
}

func runRulesEnable(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	ruleID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Enabling inbox rule via Graph API")

	rule, err := client.EnableRule(ruleID)
	if err != nil {
		return err
	}

	printSuccess("Rule enabled: %s", rule.DisplayName)
	return nil
}

func runRulesDisable(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	ruleID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Disabling inbox rule via Graph API")

	rule, err := client.DisableRule(ruleID)
	if err != nil {
		return err
	}

	printSuccess("Rule disabled: %s", rule.DisplayName)
	return nil
}

// Helper functions for formatting

func formatConditions(c *mail.MessageRulePredicates) []string {
	var conds []string

	if len(c.FromAddresses) > 0 {
		addrs := make([]string, len(c.FromAddresses))
		for i, a := range c.FromAddresses {
			addrs[i] = a.EmailAddress.Address
		}
		conds = append(conds, fmt.Sprintf("from: %s", strings.Join(addrs, ", ")))
	}
	if len(c.SenderContains) > 0 {
		conds = append(conds, fmt.Sprintf("sender contains: %s", strings.Join(c.SenderContains, ", ")))
	}
	if len(c.SubjectContains) > 0 {
		conds = append(conds, fmt.Sprintf("subject contains: %s", strings.Join(c.SubjectContains, ", ")))
	}
	if len(c.BodyContains) > 0 {
		conds = append(conds, fmt.Sprintf("body contains: %s", strings.Join(c.BodyContains, ", ")))
	}
	if c.HasAttachments != nil && *c.HasAttachments {
		conds = append(conds, "has attachments")
	}
	if c.SentToMe != nil && *c.SentToMe {
		conds = append(conds, "sent to me")
	}
	if c.SentCcMe != nil && *c.SentCcMe {
		conds = append(conds, "sent cc me")
	}
	if c.Importance != "" {
		conds = append(conds, fmt.Sprintf("importance: %s", c.Importance))
	}

	return conds
}

func formatActions(a *mail.MessageRuleActions) []string {
	var acts []string

	if a.MoveToFolder != "" {
		acts = append(acts, fmt.Sprintf("move to: %s", a.MoveToFolder))
	}
	if a.CopyToFolder != "" {
		acts = append(acts, fmt.Sprintf("copy to: %s", a.CopyToFolder))
	}
	if a.MarkAsRead != nil && *a.MarkAsRead {
		acts = append(acts, "mark read")
	}
	if a.Delete != nil && *a.Delete {
		acts = append(acts, "delete")
	}
	if a.MarkImportance != "" {
		acts = append(acts, fmt.Sprintf("mark %s", a.MarkImportance))
	}
	if len(a.ForwardTo) > 0 {
		addrs := make([]string, len(a.ForwardTo))
		for i, addr := range a.ForwardTo {
			addrs[i] = addr.EmailAddress.Address
		}
		acts = append(acts, fmt.Sprintf("forward to: %s", strings.Join(addrs, ", ")))
	}
	if len(a.RedirectTo) > 0 {
		addrs := make([]string, len(a.RedirectTo))
		for i, addr := range a.RedirectTo {
			addrs[i] = addr.EmailAddress.Address
		}
		acts = append(acts, fmt.Sprintf("redirect to: %s", strings.Join(addrs, ", ")))
	}
	if len(a.AssignCategories) > 0 {
		acts = append(acts, fmt.Sprintf("categories: %s", strings.Join(a.AssignCategories, ", ")))
	}
	if a.StopProcessingRules != nil && *a.StopProcessingRules {
		acts = append(acts, "stop processing")
	}

	return acts
}
