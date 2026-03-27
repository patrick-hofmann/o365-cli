package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/mail"
	"github.com/yourname/o365-cli/internal/profile"
)

var mailCmd = &cobra.Command{
	Use:   "mail",
	Short: "Manage emails",
	Long:  "Commands for reading and sending emails via Microsoft Graph API.",
}

// List Command
var (
	listFolder     string
	listLimit      int
	listUnreadOnly bool
	listJSON       bool
)

var mailListCmd = &cobra.Command{
	Use:   "list",
	Short: "List emails",
	Long: `Lists emails from a folder.

Examples:
  o365-cli mail list
  o365-cli mail list --folder "Sent Items" --limit 20
  o365-cli mail list --unread
  o365-cli mail list --json`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.read"},
	RunE:        runMailList,
}

// Read Command
var readFolder string

var readCmd = &cobra.Command{
	Use:   "read [message-id]",
	Short: "Read email",
	Long: `Shows the content of an email.

Find the message ID in the output of 'mail list --json'.

Examples:
  o365-cli mail read AAMkAGI2...
  o365-cli mail read AAMkAGI2... --folder "Sent Items"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.read"},
	Args:        cobra.ExactArgs(1),
	RunE:        runRead,
}

// Send Command
var (
	sendTo       []string
	sendCc       []string
	sendBcc      []string
	sendSubject  string
	sendBody     string
	sendBodyFile string
	sendHTML     bool
)

var sendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send email",
	Long: `Sends an email via Microsoft Graph API.

Examples:
  o365-cli mail send --to user@example.com --subject "Test" --body "Hello!"
  o365-cli mail send --to user@example.com --subject "Report" --body-file report.txt
  o365-cli mail send --to user@example.com --cc boss@example.com --subject "Info" --body "Text"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.send"},
	RunE:        runSend,
}

// Mark-read Command
var markReadFolder string

var markReadCmd = &cobra.Command{
	Use:   "mark-read [message-id]",
	Short: "Mark email as read",
	Long: `Marks an email as read.

Examples:
  o365-cli mail mark-read AAMkAGI2...
  o365-cli mail mark-read AAMkAGI2... --folder "Archive"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.modify"},
	Args:        cobra.ExactArgs(1),
	RunE:        runMarkRead,
}

// Mark-unread Command
var markUnreadFolder string

var markUnreadCmd = &cobra.Command{
	Use:   "mark-unread [message-id]",
	Short: "Mark email as unread",
	Long: `Marks an email as unread.

Examples:
  o365-cli mail mark-unread AAMkAGI2...
  o365-cli mail mark-unread AAMkAGI2... --folder "Archive"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.modify"},
	Args:        cobra.ExactArgs(1),
	RunE:        runMarkUnread,
}

// Move Command
var (
	moveFromFolder string
	moveToFolder   string
)

var moveCmd = &cobra.Command{
	Use:   "move [message-id]",
	Short: "Move email to folder",
	Long: `Moves an email to another folder.

Examples:
  o365-cli mail move AAMkAGI2... --to "Archive"
  o365-cli mail move AAMkAGI2... --folder "Sent Items" --to "Archive"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.move"},
	Args:        cobra.ExactArgs(1),
	RunE:        runMove,
}

// Trash Command
var trashFolder string

var trashCmd = &cobra.Command{
	Use:   "trash [message-id]",
	Short: "Move email to Trash",
	Long: `Moves an email to the Deleted Items folder.
This is a safe delete - the email can be recovered from Trash.

Examples:
  o365-cli mail trash AAMkAGI2...
  o365-cli mail trash AAMkAGI2... --folder "Spam"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.delete"},
	Args:        cobra.ExactArgs(1),
	RunE:        runTrash,
}

// Search Command
var (
	searchFolder  string
	searchFrom    string
	searchSubject string
	searchSince   string
	searchLimit   int
	searchJSON    bool
)

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search emails",
	Long: `Searches emails by various criteria.

Examples:
  o365-cli mail search --from "sender@example.com"
  o365-cli mail search --subject "important"
  o365-cli mail search --since 24h
  o365-cli mail search --from "boss@company.com" --since 7d --json`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.read"},
	RunE:        runSearch,
}

// Query Command
var (
	queryFolder string
	queryLimit  int
	queryJSON   bool
)

var queryCmd = &cobra.Command{
	Use:   "query [search-text]",
	Short: "Full-text search emails using KQL",
	Long: `Searches emails using KQL (Keyword Query Language) via the Exchange search index.
Supports full-text search across subject, body, and other fields.

KQL examples:
  Simple keyword:         test
  Phrase:                 "quarterly report"
  Specific field:         from:sender@example.com
  Multiple fields:        from:boss@company.com subject:urgent
  Boolean:                budget AND 2024

Examples:
  o365-cli mail query "test"
  o365-cli mail query "from:sender@example.com"
  o365-cli mail query "subject:urgent" --folder all
  o365-cli mail query "quarterly report" --limit 20 --json`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.read"},
	Args:        cobra.ExactArgs(1),
	RunE:        runQuery,
}

// Attachments Command
var (
	attachFolder string
	attachSaveTo string
)

var attachmentsCmd = &cobra.Command{
	Use:   "attachments [message-id]",
	Short: "Download email attachments",
	Long: `Downloads attachments from an email.

Examples:
  o365-cli mail attachments AAMkAGI2... --save-to ./downloads
  o365-cli mail attachments AAMkAGI2... --folder "Sent Items" --save-to /tmp/attachments`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.read"},
	Args:        cobra.ExactArgs(1),
	RunE:        runAttachments,
}

// Reply Command
var (
	replyFolder   string
	replyBody     string
	replyBodyFile string
	replyAll      bool
)

var replyCmd = &cobra.Command{
	Use:   "reply [message-id]",
	Short: "Reply to an email",
	Long: `Replies to an email.

Examples:
  o365-cli mail reply AAMkAGI2... --body "Thank you for your email!"
  o365-cli mail reply AAMkAGI2... --body-file response.txt
  o365-cli mail reply AAMkAGI2... --body "Thanks!" --reply-all`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.send"},
	Args:        cobra.ExactArgs(1),
	RunE:        runReply,
}

// Forward Command
var (
	forwardFolder   string
	forwardTo       []string
	forwardBody     string
	forwardBodyFile string
)

var forwardCmd = &cobra.Command{
	Use:   "forward [message-id]",
	Short: "Forward an email",
	Long: `Forwards an email to new recipients.

Examples:
  o365-cli mail forward AAMkAGI2... --to colleague@example.com
  o365-cli mail forward AAMkAGI2... --to colleague@example.com --body "FYI - please review"`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.send"},
	Args:        cobra.ExactArgs(1),
	RunE:        runForward,
}

// Archive-from Command
var (
	archiveFromFolder string
	archiveFromDryRun bool
)

var archiveFromCmd = &cobra.Command{
	Use:   "archive-from [email-address...]",
	Short: "Archive all emails from specific sender(s)",
	Long: `Archives all emails from one or more exact email addresses.
Uses exact matching on the sender's email address.

Examples:
  o365-cli mail archive-from notifications@example.com
  o365-cli mail archive-from noreply@service.com alerts@monitor.com
  o365-cli mail archive-from spam@example.com --dry-run`,
	Annotations: map[string]string{profile.AnnotationKey: "mail.move"},
	Args:        cobra.MinimumNArgs(1),
	RunE:        runArchiveFrom,
}

func init() {
	// List flags
	mailListCmd.Flags().StringVar(&listFolder, "folder", "inbox", "Folder to read from")
	mailListCmd.Flags().IntVar(&listLimit, "limit", 10, "Maximum number of emails")
	mailListCmd.Flags().BoolVar(&listUnreadOnly, "unread", false, "Only unread emails")
	mailListCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")

	// Read flags
	readCmd.Flags().StringVar(&readFolder, "folder", "inbox", "Folder of the email")

	// Send flags
	sendCmd.Flags().StringArrayVar(&sendTo, "to", nil, "Recipients (can be specified multiple times)")
	sendCmd.Flags().StringArrayVar(&sendCc, "cc", nil, "CC recipients")
	sendCmd.Flags().StringArrayVar(&sendBcc, "bcc", nil, "BCC recipients")
	sendCmd.Flags().StringVar(&sendSubject, "subject", "", "Subject")
	sendCmd.Flags().StringVar(&sendBody, "body", "", "Message body")
	sendCmd.Flags().StringVar(&sendBodyFile, "body-file", "", "Read message body from file")
	sendCmd.Flags().BoolVar(&sendHTML, "html", false, "Send body as HTML")

	sendCmd.MarkFlagRequired("to")
	sendCmd.MarkFlagRequired("subject")

	// Mark-read flags
	markReadCmd.Flags().StringVar(&markReadFolder, "folder", "inbox", "Folder of the email")

	// Mark-unread flags
	markUnreadCmd.Flags().StringVar(&markUnreadFolder, "folder", "inbox", "Folder of the email")

	// Move flags
	moveCmd.Flags().StringVar(&moveFromFolder, "folder", "inbox", "Source folder")
	moveCmd.Flags().StringVar(&moveToFolder, "to", "", "Destination folder")
	moveCmd.MarkFlagRequired("to")

	// Trash flags
	trashCmd.Flags().StringVar(&trashFolder, "folder", "inbox", "Folder of the email")

	// Search flags
	searchCmd.Flags().StringVar(&searchFolder, "folder", "inbox", "Folder to search")
	searchCmd.Flags().StringVar(&searchFrom, "from", "", "Filter by sender")
	searchCmd.Flags().StringVar(&searchSubject, "subject", "", "Filter by subject")
	searchCmd.Flags().StringVar(&searchSince, "since", "", "Filter emails since (e.g., 24h, 7d, 30d)")
	searchCmd.Flags().IntVar(&searchLimit, "limit", 50, "Maximum results")
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Output as JSON")

	// Attachments flags
	attachmentsCmd.Flags().StringVar(&attachFolder, "folder", "inbox", "Folder of the email")
	attachmentsCmd.Flags().StringVar(&attachSaveTo, "save-to", "", "Directory to save attachments")
	attachmentsCmd.MarkFlagRequired("save-to")

	// Reply flags
	replyCmd.Flags().StringVar(&replyFolder, "folder", "inbox", "Folder of the email")
	replyCmd.Flags().StringVar(&replyBody, "body", "", "Reply message body")
	replyCmd.Flags().StringVar(&replyBodyFile, "body-file", "", "Read reply body from file")
	replyCmd.Flags().BoolVar(&replyAll, "reply-all", false, "Reply to all recipients")

	// Forward flags
	forwardCmd.Flags().StringVar(&forwardFolder, "folder", "inbox", "Folder of the email")
	forwardCmd.Flags().StringArrayVar(&forwardTo, "to", nil, "Recipients (can be specified multiple times)")
	forwardCmd.Flags().StringVar(&forwardBody, "body", "", "Additional message body")
	forwardCmd.Flags().StringVar(&forwardBodyFile, "body-file", "", "Read additional body from file")
	forwardCmd.MarkFlagRequired("to")

	// Query flags
	queryCmd.Flags().StringVar(&queryFolder, "folder", "inbox", "Folder to search (use \"all\" for all folders)")
	queryCmd.Flags().IntVar(&queryLimit, "limit", 50, "Maximum results")
	queryCmd.Flags().BoolVar(&queryJSON, "json", false, "Output as JSON")

	// Archive-from flags
	archiveFromCmd.Flags().StringVar(&archiveFromFolder, "folder", "inbox", "Folder to search in")
	archiveFromCmd.Flags().BoolVar(&archiveFromDryRun, "dry-run", false, "Show what would be archived without actually moving")

	mailCmd.AddCommand(mailListCmd)
	mailCmd.AddCommand(readCmd)
	mailCmd.AddCommand(sendCmd)
	mailCmd.AddCommand(markReadCmd)
	mailCmd.AddCommand(markUnreadCmd)
	mailCmd.AddCommand(moveCmd)
	mailCmd.AddCommand(trashCmd)
	mailCmd.AddCommand(searchCmd)
	mailCmd.AddCommand(attachmentsCmd)
	mailCmd.AddCommand(replyCmd)
	mailCmd.AddCommand(forwardCmd)
	mailCmd.AddCommand(queryCmd)
	mailCmd.AddCommand(archiveFromCmd)
}

// getGraphClient creates a Graph API client with authentication
func getGraphClient(ctx context.Context) (*mail.Client, error) {
	accessToken, err := getAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	return mail.NewClient(accessToken), nil
}

func runMailList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	tokens, err := getFilteredAccessTokens(ctx)
	if err != nil {
		return err
	}

	type result struct {
		emails []mail.Email
		email  string
		err    error
	}

	results := make([]result, len(tokens))
	var wg sync.WaitGroup

	for i, at := range tokens {
		if at.Error != nil {
			results[i] = result{email: at.Email, err: at.Error}
			continue
		}
		wg.Add(1)
		go func(idx int, at AccountToken) {
			defer wg.Done()
			client := mail.NewClient(at.AccessToken)
			folderID, err := client.GetFolderByName(listFolder)
			if err != nil {
				results[idx] = result{email: at.Email, err: err}
				return
			}
			emails, err := client.ListEmails(folderID, listLimit, listUnreadOnly)
			results[idx] = result{emails: emails, email: at.Email, err: err}
		}(i, at)
	}

	wg.Wait()

	var allEmails []mail.Email
	for _, r := range results {
		if r.err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s: %v\n", r.email, r.err)
			continue
		}
		for i := range r.emails {
			r.emails[i].Account = r.email
			allEmails = append(allEmails, r.emails[i])
		}
	}

	sort.Slice(allEmails, func(i, j int) bool {
		return allEmails[i].Date.After(allEmails[j].Date)
	})

	if listJSON {
		return outputJSON(allEmails)
	}

	if len(allEmails) == 0 {
		printInfo("No emails found.")
		return nil
	}

	multi := isMultiAccount(ctx)

	if multi {
		fmt.Printf("\n%-22s %-20s %-20s %-25s %s\n", "ACCOUNT", "ID", "Date", "From", "Subject")
		fmt.Println(strings.Repeat("─", 130))

		for _, email := range allEmails {
			unreadMarker := " "
			if email.Unread {
				unreadMarker = "●"
			}
			fmt.Printf("%s %-21s %-19s %-20s %-25s %s\n", unreadMarker, truncate(email.Account, 20), truncate(email.MessageID, 18), email.Date.Local().Format("2006-01-02 15:04"), truncate(email.From, 23), truncate(email.Subject, 25))
		}
	} else {
		fmt.Printf("\n%-40s %-20s %-25s %s\n", "ID", "Date", "From", "Subject")
		fmt.Println(strings.Repeat("─", 110))

		for _, email := range allEmails {
			unreadMarker := " "
			if email.Unread {
				unreadMarker = "●"
			}
			fmt.Printf("%s %-39s %-20s %-25s %s\n", unreadMarker, truncate(email.MessageID, 38), email.Date.Local().Format("2006-01-02 15:04"), truncate(email.From, 23), truncate(email.Subject, 30))
		}
	}

	fmt.Printf("\n%d emails shown\n", len(allEmails))

	return nil
}

func runRead(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	folderID, err := client.GetFolderByName(readFolder)
	if err != nil {
		return err
	}

	email, err := client.GetEmail(folderID, messageID)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf("From:    %s\n", email.From)
	fmt.Printf("To:      %s\n", strings.Join(email.To, ", "))
	fmt.Printf("Subject: %s\n", email.Subject)
	fmt.Printf("Date:    %s\n", email.Date.Local().Format(time.RFC1123))
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println(email.Body)

	return nil
}

func runSend(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if len(sendTo) == 0 {
		return fmt.Errorf("at least one recipient (--to) required")
	}

	body := sendBody
	if sendBodyFile != "" {
		content, err := os.ReadFile(sendBodyFile)
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

	debugLog("Sending email via Microsoft Graph API")

	opts := mail.SendOptions{
		To:      sendTo,
		Cc:      sendCc,
		Bcc:     sendBcc,
		Subject: sendSubject,
		Body:    body,
		HTML:    sendHTML,
	}

	if err := client.Send(opts); err != nil {
		return fmt.Errorf("send failed: %w", err)
	}

	printSuccess("Email sent to %s", strings.Join(sendTo, ", "))
	if len(sendCc) > 0 {
		printInfo("CC: %s", strings.Join(sendCc, ", "))
	}

	return nil
}

func runMarkRead(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	folderID, err := client.GetFolderByName(markReadFolder)
	if err != nil {
		return err
	}

	if err := client.MarkAsRead(folderID, messageID); err != nil {
		return err
	}

	printSuccess("Email marked as read")
	return nil
}

func runMarkUnread(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	folderID, err := client.GetFolderByName(markUnreadFolder)
	if err != nil {
		return err
	}

	if err := client.MarkAsUnread(folderID, messageID); err != nil {
		return err
	}

	printSuccess("Email marked as unread")
	return nil
}

func runMove(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	srcFolderID, err := client.GetFolderByName(moveFromFolder)
	if err != nil {
		return err
	}

	dstFolderID, err := client.GetFolderByName(moveToFolder)
	if err != nil {
		return err
	}

	if err := client.MoveEmail(srcFolderID, messageID, dstFolderID); err != nil {
		return err
	}

	printSuccess("Email moved from '%s' to '%s'", moveFromFolder, moveToFolder)
	return nil
}

func runTrash(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	folderID, err := client.GetFolderByName(trashFolder)
	if err != nil {
		return err
	}

	if err := client.TrashEmail(folderID, messageID); err != nil {
		return err
	}

	printSuccess("Email moved to Trash")
	return nil
}

func runSearch(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if searchFrom == "" && searchSubject == "" && searchSince == "" {
		return fmt.Errorf("at least one search criterion required (--from, --subject, or --since)")
	}

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	folderID, err := client.GetFolderByName(searchFolder)
	if err != nil {
		return err
	}

	var since time.Time
	if searchSince != "" {
		duration, err := parseDuration(searchSince)
		if err != nil {
			return fmt.Errorf("invalid --since value: %w", err)
		}
		since = time.Now().Add(-duration)
	}

	debugLog("Searching emails via Graph API")

	emails, err := client.SearchEmails(folderID, searchFrom, searchSubject, since, searchLimit)
	if err != nil {
		return err
	}

	if searchJSON {
		return outputJSON(emails)
	}

	if len(emails) == 0 {
		printInfo("No emails found matching criteria.")
		return nil
	}

	fmt.Printf("\n%-40s %-20s %-25s %s\n", "ID", "Date", "From", "Subject")
	fmt.Println(strings.Repeat("─", 110))

	for _, email := range emails {
		unreadMarker := " "
		if email.Unread {
			unreadMarker = "●"
		}

		id := truncate(email.MessageID, 38)
		from := truncate(email.From, 23)
		subject := truncate(email.Subject, 30)
		date := email.Date.Local().Format("2006-01-02 15:04")

		fmt.Printf("%s %-39s %-20s %-25s %s\n", unreadMarker, id, date, from, subject)
	}

	fmt.Printf("\n%d emails found\n", len(emails))

	return nil
}

func runQuery(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	query := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	var folderID string
	if strings.ToLower(queryFolder) != "all" {
		folderID, err = client.GetFolderByName(queryFolder)
		if err != nil {
			return err
		}
	}

	debugLog("Searching emails via KQL: %s", query)

	emails, err := client.SearchEmailsKQL(folderID, query, queryLimit)
	if err != nil {
		return err
	}

	if queryJSON {
		return outputJSON(emails)
	}

	if len(emails) == 0 {
		printInfo("No emails found matching query.")
		return nil
	}

	fmt.Printf("\n%-40s %-20s %-25s %s\n", "ID", "Date", "From", "Subject")
	fmt.Println(strings.Repeat("─", 110))

	for _, email := range emails {
		unreadMarker := " "
		if email.Unread {
			unreadMarker = "●"
		}

		id := truncate(email.MessageID, 38)
		from := truncate(email.From, 23)
		subject := truncate(email.Subject, 30)
		date := email.Date.Local().Format("2006-01-02 15:04")

		fmt.Printf("%s %-39s %-20s %-25s %s\n", unreadMarker, id, date, from, subject)
	}

	fmt.Printf("\n%d emails found\n", len(emails))

	return nil
}

func runAttachments(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	folderID, err := client.GetFolderByName(attachFolder)
	if err != nil {
		return err
	}

	attachments, err := client.GetAttachments(folderID, messageID, attachSaveTo)
	if err != nil {
		return err
	}

	if len(attachments) == 0 {
		printInfo("No attachments found in this email")
		return nil
	}

	printSuccess("Downloaded %d attachment(s) to %s:", len(attachments), attachSaveTo)
	for _, att := range attachments {
		fmt.Printf("  - %s (%s, %d bytes)\n", att.Filename, att.ContentType, att.Size)
	}

	return nil
}

func runReply(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	comment := replyBody
	if replyBodyFile != "" {
		content, err := os.ReadFile(replyBodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		comment = string(content)
	}

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Sending reply via Microsoft Graph API")

	if err := client.Reply(messageID, comment, replyAll); err != nil {
		return fmt.Errorf("reply failed: %w", err)
	}

	if replyAll {
		printSuccess("Reply-all sent")
	} else {
		printSuccess("Reply sent")
	}

	return nil
}

func runForward(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	messageID := args[0]

	if len(forwardTo) == 0 {
		return fmt.Errorf("at least one recipient (--to) required")
	}

	comment := forwardBody
	if forwardBodyFile != "" {
		content, err := os.ReadFile(forwardBodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		comment = string(content)
	}

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Forwarding email via Microsoft Graph API")

	if err := client.Forward(messageID, forwardTo, comment); err != nil {
		return fmt.Errorf("forward failed: %w", err)
	}

	printSuccess("Email forwarded to %s", strings.Join(forwardTo, ", "))

	return nil
}

// Helper functions

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	if strings.HasSuffix(s, "d") {
		days := s[:len(s)-1]
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return 0, fmt.Errorf("invalid days: %s", s)
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}

	return time.ParseDuration(s)
}

func outputJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func runArchiveFrom(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	senderAddresses := args

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	srcFolderID, err := client.GetFolderByName(archiveFromFolder)
	if err != nil {
		return err
	}

	debugLog("Searching for emails from: %s", strings.Join(senderAddresses, ", "))

	// Find all emails from the specified senders (no limit - get all)
	emails, err := client.ListEmailsFromSenders(srcFolderID, senderAddresses, 0)
	if err != nil {
		return fmt.Errorf("failed to list emails: %w", err)
	}

	if len(emails) == 0 {
		printInfo("No emails found from the specified sender(s)")
		return nil
	}

	fmt.Printf("Found %d email(s) from: %s\n", len(emails), strings.Join(senderAddresses, ", "))

	if archiveFromDryRun {
		fmt.Println("\nDry run - would archive:")
		for _, email := range emails {
			date := email.Date.Local().Format("2006-01-02")
			fmt.Printf("  • [%s] %s - %s\n", date, truncate(email.From, 30), truncate(email.Subject, 40))
		}
		return nil
	}

	// Get archive folder ID
	archiveFolderID, err := client.GetFolderByName("Archive")
	if err != nil {
		return fmt.Errorf("failed to get Archive folder: %w", err)
	}

	// Move each email to archive
	archived := 0
	for _, email := range emails {
		if err := client.MoveEmail(srcFolderID, email.MessageID, archiveFolderID); err != nil {
			fmt.Printf("✗ Failed to archive: %s\n", truncate(email.Subject, 50))
			continue
		}
		archived++
	}

	printSuccess("Archived %d email(s) from %s", archived, strings.Join(senderAddresses, ", "))
	return nil
}
