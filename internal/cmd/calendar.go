package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/calendar"
	"github.com/yourname/o365-cli/internal/profile"
)

var calendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "Manage calendar events",
	Long:  "Commands for listing, creating, and managing calendar events via Microsoft Graph API.",
}

// --- List ---

var (
	calListDays  int
	calListLimit int
	calListJSON  bool
)

var calendarListCmd = &cobra.Command{
	Use:   "list",
	Short: "List upcoming events",
	Long: `Lists calendar events in a time range.

Examples:
  o365-cli calendar list
  o365-cli calendar list --days 14
  o365-cli calendar list --limit 10 --json`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.read"},
	RunE:        runCalendarList,
}

// --- Today ---

var calTodayJSON bool

var calendarTodayCmd = &cobra.Command{
	Use:   "today",
	Short: "Show today's events",
	Long: `Lists all events for today.

Examples:
  o365-cli calendar today
  o365-cli calendar today --json`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.read"},
	RunE:        runCalendarToday,
}

// --- Get ---

var calGetJSON bool

var calendarGetCmd = &cobra.Command{
	Use:   "get [event-id]",
	Short: "Show event details",
	Long: `Shows the full details of a calendar event.

Examples:
  o365-cli calendar get AAMkAGI2...
  o365-cli calendar get AAMkAGI2... --json`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.read"},
	Args:        cobra.ExactArgs(1),
	RunE:        runCalendarGet,
}

// --- Create ---

var (
	calCreateSubject   string
	calCreateStart     string
	calCreateEnd       string
	calCreateLocation  string
	calCreateBody      string
	calCreateBodyFile  string
	calCreateAttendees []string
	calCreateAllDay    bool
	calCreateHTML      bool
)

var calendarCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new event",
	Long: `Creates a new calendar event.

Examples:
  o365-cli calendar create --subject "Meeting" --start "2026-04-01T10:00:00+02:00" --end "2026-04-01T11:00:00+02:00"
  o365-cli calendar create --subject "Workshop" --start "2026-04-01T09:00:00Z" --end "2026-04-01T17:00:00Z" --location "Room A" --attendees user@example.com`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.write"},
	RunE:        runCalendarCreate,
}

// --- Update ---

var (
	calUpdateSubject   string
	calUpdateStart     string
	calUpdateEnd       string
	calUpdateLocation  string
	calUpdateBody      string
	calUpdateBodyFile  string
	calUpdateAttendees []string
)

var calendarUpdateCmd = &cobra.Command{
	Use:   "update [event-id]",
	Short: "Update an event",
	Long: `Updates an existing calendar event. Only specified fields are changed.

Examples:
  o365-cli calendar update AAMkAGI2... --subject "New Title"
  o365-cli calendar update AAMkAGI2... --start "2026-04-01T14:00:00Z" --end "2026-04-01T15:00:00Z"`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.write"},
	Args:        cobra.ExactArgs(1),
	RunE:        runCalendarUpdate,
}

// --- Delete ---

var calendarDeleteCmd = &cobra.Command{
	Use:   "delete [event-id]",
	Short: "Delete an event",
	Long: `Deletes a calendar event.

Examples:
  o365-cli calendar delete AAMkAGI2...`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.delete"},
	Args:        cobra.ExactArgs(1),
	RunE:        runCalendarDelete,
}

// --- Accept / Decline / Tentative ---

var calRespondComment string

var calendarAcceptCmd = &cobra.Command{
	Use:   "accept [event-id]",
	Short: "Accept an event invitation",
	Long: `Accepts a calendar event invitation.

Examples:
  o365-cli calendar accept AAMkAGI2...
  o365-cli calendar accept AAMkAGI2... --comment "Looking forward to it!"`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.respond"},
	Args:        cobra.ExactArgs(1),
	RunE:        runCalendarAccept,
}

var calendarDeclineCmd = &cobra.Command{
	Use:   "decline [event-id]",
	Short: "Decline an event invitation",
	Long: `Declines a calendar event invitation.

Examples:
  o365-cli calendar decline AAMkAGI2...
  o365-cli calendar decline AAMkAGI2... --comment "Can't make it"`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.respond"},
	Args:        cobra.ExactArgs(1),
	RunE:        runCalendarDecline,
}

var calendarTentativeCmd = &cobra.Command{
	Use:   "tentative [event-id]",
	Short: "Tentatively accept an event",
	Long: `Tentatively accepts a calendar event invitation.

Examples:
  o365-cli calendar tentative AAMkAGI2...`,
	Annotations: map[string]string{profile.AnnotationKey: "calendar.respond"},
	Args:        cobra.ExactArgs(1),
	RunE:        runCalendarTentative,
}

func init() {
	// list
	calendarListCmd.Flags().IntVar(&calListDays, "days", 7, "Number of days to show")
	calendarListCmd.Flags().IntVar(&calListLimit, "limit", 25, "Maximum number of events")
	calendarListCmd.Flags().BoolVar(&calListJSON, "json", false, "Output as JSON")

	// today
	calendarTodayCmd.Flags().BoolVar(&calTodayJSON, "json", false, "Output as JSON")

	// get
	calendarGetCmd.Flags().BoolVar(&calGetJSON, "json", false, "Output as JSON")

	// create
	calendarCreateCmd.Flags().StringVar(&calCreateSubject, "subject", "", "Event subject (required)")
	calendarCreateCmd.Flags().StringVar(&calCreateStart, "start", "", "Start time in RFC3339 format (required)")
	calendarCreateCmd.Flags().StringVar(&calCreateEnd, "end", "", "End time in RFC3339 format (required)")
	calendarCreateCmd.Flags().StringVar(&calCreateLocation, "location", "", "Event location")
	calendarCreateCmd.Flags().StringVar(&calCreateBody, "body", "", "Event body/description")
	calendarCreateCmd.Flags().StringVar(&calCreateBodyFile, "body-file", "", "Read body from file")
	calendarCreateCmd.Flags().StringSliceVar(&calCreateAttendees, "attendees", nil, "Attendee email addresses")
	calendarCreateCmd.Flags().BoolVar(&calCreateAllDay, "all-day", false, "All-day event")
	calendarCreateCmd.Flags().BoolVar(&calCreateHTML, "html", false, "Body is HTML")
	_ = calendarCreateCmd.MarkFlagRequired("subject")
	_ = calendarCreateCmd.MarkFlagRequired("start")
	_ = calendarCreateCmd.MarkFlagRequired("end")

	// update
	calendarUpdateCmd.Flags().StringVar(&calUpdateSubject, "subject", "", "New subject")
	calendarUpdateCmd.Flags().StringVar(&calUpdateStart, "start", "", "New start time (RFC3339)")
	calendarUpdateCmd.Flags().StringVar(&calUpdateEnd, "end", "", "New end time (RFC3339)")
	calendarUpdateCmd.Flags().StringVar(&calUpdateLocation, "location", "", "New location")
	calendarUpdateCmd.Flags().StringVar(&calUpdateBody, "body", "", "New body")
	calendarUpdateCmd.Flags().StringVar(&calUpdateBodyFile, "body-file", "", "Read new body from file")
	calendarUpdateCmd.Flags().StringSliceVar(&calUpdateAttendees, "attendees", nil, "New attendee list")

	// respond
	calendarAcceptCmd.Flags().StringVar(&calRespondComment, "comment", "", "Response comment")
	calendarDeclineCmd.Flags().StringVar(&calRespondComment, "comment", "", "Response comment")
	calendarTentativeCmd.Flags().StringVar(&calRespondComment, "comment", "", "Response comment")

	// Register subcommands
	calendarCmd.AddCommand(calendarListCmd)
	calendarCmd.AddCommand(calendarTodayCmd)
	calendarCmd.AddCommand(calendarGetCmd)
	calendarCmd.AddCommand(calendarCreateCmd)
	calendarCmd.AddCommand(calendarUpdateCmd)
	calendarCmd.AddCommand(calendarDeleteCmd)
	calendarCmd.AddCommand(calendarAcceptCmd)
	calendarCmd.AddCommand(calendarDeclineCmd)
	calendarCmd.AddCommand(calendarTentativeCmd)
}

func getCalendarClient(ctx context.Context) (*calendar.Client, error) {
	accessToken, err := getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return calendar.NewClient(accessToken), nil
}

func runCalendarList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	endTime := now.AddDate(0, 0, calListDays)

	events, err := client.ListEvents(now, endTime, calListLimit)
	if err != nil {
		return err
	}

	if calListJSON {
		return calOutputJSON(events)
	}

	return printEventTable(events)
}

func runCalendarToday(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.AddDate(0, 0, 1)

	events, err := client.ListEvents(startOfDay, endOfDay, 50)
	if err != nil {
		return err
	}

	if calTodayJSON {
		return calOutputJSON(events)
	}

	if len(events) == 0 {
		printInfo("No events today.")
		return nil
	}

	fmt.Printf("\nEvents for %s:\n", now.Format("Monday, 2 January 2006"))
	fmt.Println(strings.Repeat("─", 90))

	for _, ev := range events {
		if ev.IsAllDay {
			fmt.Printf("  All day    %-40s %s\n", truncate(ev.Subject, 40), ev.Location)
		} else {
			start := ev.Start.Local().Format("15:04")
			end := ev.End.Local().Format("15:04")
			fmt.Printf("  %s-%s  %-40s %s\n", start, end, truncate(ev.Subject, 40), ev.Location)
		}
	}

	fmt.Printf("\n%d event(s)\n", len(events))
	return nil
}

func runCalendarGet(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	eventID := args[0]

	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	event, err := client.GetEvent(eventID)
	if err != nil {
		return err
	}

	if calGetJSON {
		return calOutputJSON(event)
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf("Subject:   %s\n", event.Subject)
	if event.IsAllDay {
		fmt.Printf("Date:      %s (all day)\n", event.Start.Local().Format("Monday, 2 January 2006"))
	} else {
		fmt.Printf("Start:     %s\n", event.Start.Local().Format("Monday, 2 January 2006 15:04"))
		fmt.Printf("End:       %s\n", event.End.Local().Format("Monday, 2 January 2006 15:04"))
		duration := event.End.Sub(event.Start)
		fmt.Printf("Duration:  %s\n", formatDuration(duration))
	}
	if event.Location != "" {
		fmt.Printf("Location:  %s\n", event.Location)
	}
	fmt.Printf("Organizer: %s\n", event.Organizer)
	fmt.Printf("Status:    %s\n", event.ShowAs)
	fmt.Printf("Response:  %s\n", event.ResponseStatus)
	if event.IsOnline && event.OnlineURL != "" {
		fmt.Printf("Online:    %s\n", event.OnlineURL)
	}

	if len(event.Attendees) > 0 {
		fmt.Println("\nAttendees:")
		for _, att := range event.Attendees {
			name := att.Email
			if att.Name != "" {
				name = fmt.Sprintf("%s <%s>", att.Name, att.Email)
			}
			fmt.Printf("  [%s] %s (%s)\n", att.Response, name, att.Type)
		}
	}

	if event.Body != "" {
		fmt.Println("═══════════════════════════════════════════════════════════════")
		fmt.Println()
		fmt.Println(event.Body)
	}

	return nil
}

func runCalendarCreate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	startTime, err := parseTime(calCreateStart)
	if err != nil {
		return fmt.Errorf("invalid --start: %w", err)
	}

	endTime, err := parseTime(calCreateEnd)
	if err != nil {
		return fmt.Errorf("invalid --end: %w", err)
	}

	body := calCreateBody
	if calCreateBodyFile != "" {
		content, err := os.ReadFile(calCreateBodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		body = string(content)
	}

	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	opts := calendar.CreateEventOptions{
		Subject:   calCreateSubject,
		Body:      body,
		IsHTML:    calCreateHTML,
		Start:     startTime,
		End:       endTime,
		Location:  calCreateLocation,
		Attendees: calCreateAttendees,
		IsAllDay:  calCreateAllDay,
	}

	event, err := client.CreateEvent(opts)
	if err != nil {
		return fmt.Errorf("create failed: %w", err)
	}

	printSuccess("Event created: %s", event.Subject)
	printInfo("ID: %s", event.ID)
	return nil
}

func runCalendarUpdate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	eventID := args[0]

	opts := calendar.UpdateEventOptions{}

	if cmd.Flags().Changed("subject") {
		opts.Subject = &calUpdateSubject
	}
	if cmd.Flags().Changed("location") {
		opts.Location = &calUpdateLocation
	}
	if cmd.Flags().Changed("start") {
		t, err := parseTime(calUpdateStart)
		if err != nil {
			return fmt.Errorf("invalid --start: %w", err)
		}
		opts.Start = &t
	}
	if cmd.Flags().Changed("end") {
		t, err := parseTime(calUpdateEnd)
		if err != nil {
			return fmt.Errorf("invalid --end: %w", err)
		}
		opts.End = &t
	}

	body := calUpdateBody
	if calUpdateBodyFile != "" {
		content, err := os.ReadFile(calUpdateBodyFile)
		if err != nil {
			return fmt.Errorf("could not read body file: %w", err)
		}
		body = string(content)
	}
	if cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file") {
		opts.Body = &body
	}

	if cmd.Flags().Changed("attendees") {
		opts.Attendees = calUpdateAttendees
	}

	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	event, err := client.UpdateEvent(eventID, opts)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	printSuccess("Event updated: %s", event.Subject)
	return nil
}

func runCalendarDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	eventID := args[0]

	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	if err := client.DeleteEvent(eventID); err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	printSuccess("Event deleted")
	return nil
}

func runCalendarAccept(cmd *cobra.Command, args []string) error {
	return runCalendarRespond(args[0], calendar.ResponseAccept)
}

func runCalendarDecline(cmd *cobra.Command, args []string) error {
	return runCalendarRespond(args[0], calendar.ResponseDecline)
}

func runCalendarTentative(cmd *cobra.Command, args []string) error {
	return runCalendarRespond(args[0], calendar.ResponseTentative)
}

func runCalendarRespond(eventID string, response calendar.ResponseType) error {
	ctx := context.Background()

	client, err := getCalendarClient(ctx)
	if err != nil {
		return err
	}

	if err := client.RespondToEvent(eventID, response, calRespondComment); err != nil {
		return fmt.Errorf("respond failed: %w", err)
	}

	printSuccess("Response sent: %s", string(response))
	return nil
}

// --- Helpers ---

func printEventTable(events []calendar.Event) error {
	if len(events) == 0 {
		printInfo("No events found.")
		return nil
	}

	fmt.Printf("\n%-12s %-13s %-8s %-35s %s\n", "DATE", "TIME", "DURATION", "SUBJECT", "LOCATION")
	fmt.Println(strings.Repeat("─", 90))

	for _, ev := range events {
		date := ev.Start.Local().Format("2006-01-02")
		var timeStr, durStr string
		if ev.IsAllDay {
			timeStr = "all day"
			durStr = ""
		} else {
			timeStr = ev.Start.Local().Format("15:04") + "-" + ev.End.Local().Format("15:04")
			durStr = formatDuration(ev.End.Sub(ev.Start))
		}
		subject := truncate(ev.Subject, 33)
		location := truncate(ev.Location, 20)

		fmt.Printf("%-12s %-13s %-8s %-35s %s\n", date, timeStr, durStr, subject, location)
	}

	fmt.Printf("\n%d event(s)\n", len(events))
	return nil
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 && minutes > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", minutes)
}

func parseTime(s string) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try common formats
	layouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse time: %s (use RFC3339 format like 2026-04-01T10:00:00+02:00)", s)
}

func calOutputJSON(data interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
