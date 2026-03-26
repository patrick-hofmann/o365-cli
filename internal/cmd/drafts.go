package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/profile"
)

var draftsCmd = &cobra.Command{
	Use:   "drafts",
	Short: "Manage email drafts",
	Long:  "Commands for managing email drafts.",
}

// Draft create command
var (
	draftTo       []string
	draftCc       []string
	draftSubject  string
	draftBody     string
	draftBodyFile string
	draftHTML     bool
)

var draftCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a draft email",
	Long: `Creates a draft email and saves it to the Drafts folder.

Examples:
  o365-cli mail drafts create --to user@example.com --subject "Test" --body "Hello!"
  o365-cli mail drafts create --to user@example.com --subject "Report" --body-file draft.txt`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.create"},
	RunE:        runDraftCreate,
}

// Draft list command
var draftListJSON bool

var draftListCmd = &cobra.Command{
	Use:   "list",
	Short: "List drafts",
	Long: `Lists all draft emails.

Examples:
  o365-cli mail drafts list
  o365-cli mail drafts list --json`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.list"},
	RunE:        runDraftList,
}

// Draft send command
var draftSendCmd = &cobra.Command{
	Use:   "send [message-id]",
	Short: "Send a draft",
	Long: `Sends a draft email and removes it from the Drafts folder.

Examples:
  o365-cli mail drafts send AAMkAGI2...`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.send"},
	Args:        cobra.ExactArgs(1),
	RunE:        runDraftSend,
}

// Draft delete command
var draftDeleteCmd = &cobra.Command{
	Use:   "delete [message-id]",
	Short: "Delete a draft",
	Long: `Deletes a draft email from the Drafts folder.

Examples:
  o365-cli mail drafts delete AAMkAGI2...`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.delete"},
	Args:        cobra.ExactArgs(1),
	RunE:        runDraftDelete,
}

func init() {
	// Draft create flags
	draftCreateCmd.Flags().StringArrayVar(&draftTo, "to", nil, "Recipients")
	draftCreateCmd.Flags().StringArrayVar(&draftCc, "cc", nil, "CC recipients")
	draftCreateCmd.Flags().StringVar(&draftSubject, "subject", "", "Subject")
	draftCreateCmd.Flags().StringVar(&draftBody, "body", "", "Message body")
	draftCreateCmd.Flags().StringVar(&draftBodyFile, "body-file", "", "Read body from file")
	draftCreateCmd.Flags().BoolVar(&draftHTML, "html", false, "Body is HTML")

	draftCreateCmd.MarkFlagRequired("to")
	draftCreateCmd.MarkFlagRequired("subject")

	// Draft list flags
	draftListCmd.Flags().BoolVar(&draftListJSON, "json", false, "Output as JSON")

	// Add subcommands
	draftsCmd.AddCommand(draftCreateCmd)
	draftsCmd.AddCommand(draftListCmd)
	draftsCmd.AddCommand(draftSendCmd)
	draftsCmd.AddCommand(draftDeleteCmd)

	// Add drafts command to mail
	mailCmd.AddCommand(draftsCmd)
}

func runDraftCreate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Validation
	if len(draftTo) == 0 {
		return fmt.Errorf("at least one recipient (--to) required")
	}

	// Body from file or direct
	body := draftBody
	if draftBodyFile != "" {
		content, err := os.ReadFile(draftBodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		body = string(content)
	}

	if body == "" {
		return fmt.Errorf("message body required (--body or --body-file)")
	}

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Creating draft via Graph API")

	draftID, err := client.SaveDraft(draftTo, draftCc, draftSubject, body, draftHTML)
	if err != nil {
		return fmt.Errorf("failed to save draft: %w", err)
	}

	printSuccess("Draft saved (ID: %s)", draftID)
	return nil
}

func runDraftList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Fetching drafts via Graph API")

	drafts, err := client.ListDrafts(50)
	if err != nil {
		return err
	}

	// Output
	if draftListJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(drafts)
	}

	if len(drafts) == 0 {
		printInfo("No drafts found.")
		return nil
	}

	fmt.Printf("\n%-50s %-20s %-25s %s\n", "ID", "Date", "To", "Subject")
	fmt.Println(strings.Repeat("-", 120))

	for _, draft := range drafts {
		to := ""
		if len(draft.To) > 0 {
			to = truncate(draft.To[0], 23)
		}
		subject := truncate(draft.Subject, 35)
		date := draft.Date.Local().Format("2006-01-02 15:04")
		id := truncate(draft.ID, 48)

		fmt.Printf("%-50s %-20s %-25s %s\n", id, date, to, subject)
	}

	fmt.Printf("\n%d draft(s) found\n", len(drafts))

	return nil
}

func runDraftSend(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Sending draft via Graph API")

	if err := client.SendDraft(messageID); err != nil {
		return fmt.Errorf("failed to send draft: %w", err)
	}

	printSuccess("Draft sent successfully")
	return nil
}

func runDraftDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Deleting draft via Graph API")

	if err := client.DeleteDraft(messageID); err != nil {
		return fmt.Errorf("failed to delete draft: %w", err)
	}

	printSuccess("Draft deleted")
	return nil
}
