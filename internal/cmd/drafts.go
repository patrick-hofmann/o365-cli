package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
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
	draftTo          []string
	draftCc          []string
	draftSubject     string
	draftBody        string
	draftBodyFile    string
	draftHTML        bool
	draftAttachments []string // --attach (repeatable) — used by all draft commands
)

var draftCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a draft email",
	Long: `Creates a draft email and saves it to the Drafts folder.

Examples:
  o365-cli mail drafts create --to user@example.com --subject "Test" --body "Hello!"
  o365-cli mail drafts create --to user@example.com --subject "Report" --body-file draft.txt
  o365-cli mail drafts create --to user@example.com --subject "Report" \
    --body-file draft.txt --attach report.xlsx --attach summary.pdf`,
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

// Draft create-reply / create-reply-all / create-forward commands
var (
	draftReplyTo  []string
	draftReplyCc  []string
	draftReplyBcc []string
)

var draftCreateReplyCmd = &cobra.Command{
	Use:   "create-reply [message-id]",
	Short: "Create a reply draft with auto-quoted original",
	Long: `Creates a reply draft using Microsoft Graph's createReply endpoint.

The original message is auto-embedded with native Outlook formatting (bold
Von:/Datum:/An:/Betreff: headers, German date formatting, inline images,
quote layout). Your --body text is inserted ABOVE the auto-quote (TOFU).

Examples:
  o365-cli mail drafts create-reply AAMkAGI2... --body "Vielen Dank, mein Kennzeichen ist LB-323GP."
  o365-cli mail drafts create-reply AAMkAGI2... --body-file reply.txt
  o365-cli mail drafts create-reply AAMkAGI2... --body "Anbei das Update." --attach report.xlsx`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.create-reply"},
	Args:        cobra.ExactArgs(1),
	RunE:        func(cmd *cobra.Command, args []string) error { return runDraftCreateReply(cmd, args, false) },
}

var draftCreateReplyAllCmd = &cobra.Command{
	Use:   "create-reply-all [message-id]",
	Short: "Create a reply-all draft with auto-quoted original",
	Long: `Creates a reply-all draft using Microsoft Graph's createReplyAll endpoint.

Same auto-formatting as create-reply, but addresses all original recipients
(To + Cc) instead of just the sender.

Examples:
  o365-cli mail drafts create-reply-all AAMkAGI2... --body "Hi all, ..."
  o365-cli mail drafts create-reply-all AAMkAGI2... --body "Anbei." --attach minutes.pdf`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.create-reply-all"},
	Args:        cobra.ExactArgs(1),
	RunE:        func(cmd *cobra.Command, args []string) error { return runDraftCreateReply(cmd, args, true) },
}

var draftCreateForwardCmd = &cobra.Command{
	Use:   "create-forward [message-id]",
	Short: "Create a forward draft with auto-quoted original",
	Long: `Creates a forward draft using Microsoft Graph's createForward endpoint.

The original message is auto-embedded with native Outlook formatting. Use
--to/--cc/--bcc to set recipients on the new draft (or leave blank and
edit later in Outlook). Your --body text is inserted ABOVE the auto-quote.

Examples:
  o365-cli mail drafts create-forward AAMkAGI2... --to user@example.com --body "FYI"
  o365-cli mail drafts create-forward AAMkAGI2... --to a@x.com --cc b@y.com --body "Bitte sehen"
  o365-cli mail drafts create-forward AAMkAGI2... --to user@example.com --body "FYI" --attach addendum.pdf`,
	Annotations: map[string]string{profile.AnnotationKey: "drafts.create-forward"},
	Args:        cobra.ExactArgs(1),
	RunE:        runDraftCreateForward,
}

func init() {
	// Draft create flags
	draftCreateCmd.Flags().StringArrayVar(&draftTo, "to", nil, "Recipients")
	draftCreateCmd.Flags().StringArrayVar(&draftCc, "cc", nil, "CC recipients")
	draftCreateCmd.Flags().StringVar(&draftSubject, "subject", "", "Subject")
	draftCreateCmd.Flags().StringVar(&draftBody, "body", "", "Message body")
	draftCreateCmd.Flags().StringVar(&draftBodyFile, "body-file", "", "Read body from file")
	draftCreateCmd.Flags().BoolVar(&draftHTML, "html", false, "Body is HTML")
	draftCreateCmd.Flags().StringArrayVar(&draftAttachments, "attach", nil,
		"Path to attachment file (repeatable). Auto-detects size; > 3 MB use upload session.")

	draftCreateCmd.MarkFlagRequired("to")
	draftCreateCmd.MarkFlagRequired("subject")

	// Draft list flags
	draftListCmd.Flags().BoolVar(&draftListJSON, "json", false, "Output as JSON")

	// Draft create-reply / create-reply-all flags (share --body / --body-file / --html / recipient overrides)
	for _, c := range []*cobra.Command{draftCreateReplyCmd, draftCreateReplyAllCmd} {
		c.Flags().StringVar(&draftBody, "body", "", "Message body (inserted ABOVE auto-quote)")
		c.Flags().StringVar(&draftBodyFile, "body-file", "", "Read body from file")
		c.Flags().BoolVar(&draftHTML, "html", false, "Body is HTML (default: plain text)")
		c.Flags().StringArrayVar(&draftReplyTo, "to", nil, "Override To recipients (default: original sender / all)")
		c.Flags().StringArrayVar(&draftReplyCc, "cc", nil, "Override or extend Cc recipients")
		c.Flags().StringArrayVar(&draftReplyBcc, "bcc", nil, "Override or extend Bcc recipients")
		c.Flags().StringArrayVar(&draftAttachments, "attach", nil,
			"Path to attachment file (repeatable). Auto-detects size; > 3 MB use upload session.")
	}

	// Draft create-forward flags
	draftCreateForwardCmd.Flags().StringArrayVar(&draftReplyTo, "to", nil, "Recipients (forward target)")
	draftCreateForwardCmd.Flags().StringArrayVar(&draftReplyCc, "cc", nil, "CC recipients")
	draftCreateForwardCmd.Flags().StringArrayVar(&draftReplyBcc, "bcc", nil, "BCC recipients")
	draftCreateForwardCmd.Flags().StringVar(&draftBody, "body", "", "Message body (inserted ABOVE auto-quote)")
	draftCreateForwardCmd.Flags().StringVar(&draftBodyFile, "body-file", "", "Read body from file")
	draftCreateForwardCmd.Flags().BoolVar(&draftHTML, "html", false, "Body is HTML (default: plain text)")
	draftCreateForwardCmd.Flags().StringArrayVar(&draftAttachments, "attach", nil,
		"Path to attachment file (repeatable). Auto-detects size; > 3 MB use upload session.")

	// Add subcommands
	draftsCmd.AddCommand(draftCreateCmd)
	draftsCmd.AddCommand(draftListCmd)
	draftsCmd.AddCommand(draftSendCmd)
	draftsCmd.AddCommand(draftDeleteCmd)
	draftsCmd.AddCommand(draftCreateReplyCmd)
	draftsCmd.AddCommand(draftCreateReplyAllCmd)
	draftsCmd.AddCommand(draftCreateForwardCmd)

	// Add drafts command to mail
	mailCmd.AddCommand(draftsCmd)
}

func runDraftCreate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Validation
	if len(draftTo) == 0 {
		return fmt.Errorf("at least one recipient (--to) required")
	}

	// Validate attachment paths early so we don't create a draft we can't fully populate.
	if err := validateAttachmentPaths(draftAttachments); err != nil {
		return err
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

	if err := attachFilesToDraft(client, draftID, draftAttachments); err != nil {
		return err
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

// formatCommentForGraph prepares the body text for Graph's createReply/createForward
// `comment` field. The field is interpreted as HTML — plain text with embedded
// newlines collapses to whitespace unless we convert it to HTML ourselves.
//   - If isHTML is true (--html flag): pass through unchanged.
//   - Otherwise: HTML-escape special chars and turn \n into <br>.
func formatCommentForGraph(body string, isHTML bool) string {
	if body == "" || isHTML {
		return body
	}
	escaped := html.EscapeString(body)
	return strings.ReplaceAll(escaped, "\n", "<br>")
}

func resolveDraftBody() (string, error) {
	if draftBodyFile != "" {
		content, err := os.ReadFile(draftBodyFile)
		if err != nil {
			return "", fmt.Errorf("could not read body file: %w", err)
		}
		return string(content), nil
	}
	return draftBody, nil
}

func runDraftCreateReply(cmd *cobra.Command, args []string, replyAll bool) error {
	ctx := context.Background()
	messageID := args[0]

	if err := validateAttachmentPaths(draftAttachments); err != nil {
		return err
	}

	body, err := resolveDraftBody()
	if err != nil {
		return err
	}

	// Graph's createReply treats the `comment` field as HTML. For plain text
	// input we must escape HTML special chars and convert newlines to <br>,
	// otherwise newlines collapse to spaces. With --html the caller is trusted
	// to pass valid HTML.
	body = formatCommentForGraph(body, draftHTML)

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	action := "create-reply"
	if replyAll {
		action = "create-reply-all"
	}
	debugLog("Creating %s draft via Graph API", action)

	draftID, err := client.CreateReplyDraft(messageID, body, replyAll, draftReplyTo, draftReplyCc, draftReplyBcc)
	if err != nil {
		return fmt.Errorf("failed to create %s draft: %w", action, err)
	}

	if err := attachFilesToDraft(client, draftID, draftAttachments); err != nil {
		return err
	}

	printSuccess("Draft saved (ID: %s)", draftID)
	return nil
}

func runDraftCreateForward(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	if err := validateAttachmentPaths(draftAttachments); err != nil {
		return err
	}

	body, err := resolveDraftBody()
	if err != nil {
		return err
	}

	body = formatCommentForGraph(body, draftHTML)

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Creating create-forward draft via Graph API")

	draftID, err := client.CreateForwardDraft(messageID, body, draftReplyTo, draftReplyCc, draftReplyBcc)
	if err != nil {
		return fmt.Errorf("failed to create forward draft: %w", err)
	}

	if err := attachFilesToDraft(client, draftID, draftAttachments); err != nil {
		return err
	}

	printSuccess("Draft saved (ID: %s)", draftID)
	return nil
}

// validateAttachmentPaths fails fast if any --attach path doesn't exist or is a
// directory. Catches typos before we POST a draft we can't complete.
func validateAttachmentPaths(paths []string) error {
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("attachment %q: %w", p, err)
		}
		if info.IsDir() {
			return fmt.Errorf("attachment %q is a directory", p)
		}
	}
	return nil
}

// attachFilesToDraft uploads each file as attachment to the given draft.
// Reports progress per file. Aborts on first failure (the draft itself is
// kept; user can re-run or finish in Outlook).
func attachFilesToDraft(client interface {
	AddAttachment(messageID, filePath string) (string, error)
}, draftID string, paths []string) error {
	for _, p := range paths {
		info, _ := os.Stat(p)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		debugLog("Attaching %s (%d bytes) to draft %s", p, size, draftID)
		if _, err := client.AddAttachment(draftID, p); err != nil {
			return fmt.Errorf("attach %q: %w", p, err)
		}
		printInfo("Attached: %s (%s)", p, humanSize(size))
	}
	return nil
}

// humanSize renders a byte count as a short human-readable string.
func humanSize(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
	)
	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
