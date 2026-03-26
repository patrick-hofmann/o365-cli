package calendar

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/yourname/o365-cli/internal/graph"
)

// Client wraps the generic Graph client with calendar-specific operations.
type Client struct {
	*graph.Client
}

// NewClient creates a new calendar client.
func NewClient(accessToken string) *Client {
	return &Client{Client: graph.NewClient(accessToken)}
}

// ListEvents lists calendar events in a time range.
func (c *Client) ListEvents(startTime, endTime time.Time, limit int) ([]Event, error) {
	endpoint := fmt.Sprintf("%s/me/calendarView", graph.GraphAPIBaseURL)

	params := url.Values{}
	params.Set("startDateTime", startTime.UTC().Format(time.RFC3339))
	params.Set("endDateTime", endTime.UTC().Format(time.RFC3339))
	params.Set("$orderby", "start/dateTime")
	params.Set("$select", "id,subject,start,end,location,organizer,attendees,isAllDay,isOnlineMeeting,onlineMeetingUrl,showAs,responseStatus")
	if limit > 0 {
		params.Set("$top", fmt.Sprintf("%d", limit))
	}

	var allEvents []Event
	currentEndpoint := endpoint + "?" + params.Encode()

	for currentEndpoint != "" {
		resp, err := c.DoRequest("GET", currentEndpoint, nil)
		if err != nil {
			return nil, err
		}

		var result GraphEventsResponse
		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		for _, ev := range result.Value {
			allEvents = append(allEvents, graphEventToEvent(ev))
			if limit > 0 && len(allEvents) >= limit {
				return allEvents, nil
			}
		}

		currentEndpoint = result.NextLink
	}

	return allEvents, nil
}

// GetEvent fetches a single event with full details.
func (c *Client) GetEvent(eventID string) (*Event, error) {
	endpoint := fmt.Sprintf("%s/me/events/%s", graph.GraphAPIBaseURL, url.PathEscape(eventID))
	params := url.Values{}
	params.Set("$select", "id,subject,start,end,location,body,organizer,attendees,isAllDay,isOnlineMeeting,onlineMeetingUrl,showAs,responseStatus")
	endpoint += "?" + params.Encode()

	resp, err := c.DoRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var ev GraphEventResponse
	if err := json.Unmarshal(resp, &ev); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	event := graphEventToEvent(ev)
	event.Body = ev.Body.Content

	return &event, nil
}

// CreateEvent creates a new calendar event.
func (c *Client) CreateEvent(opts CreateEventOptions) (*Event, error) {
	endpoint := fmt.Sprintf("%s/me/events", graph.GraphAPIBaseURL)

	body := map[string]interface{}{
		"subject": opts.Subject,
		"start":   toGraphDateTime(opts.Start),
		"end":     toGraphDateTime(opts.End),
	}

	if opts.Location != "" {
		body["location"] = map[string]string{"displayName": opts.Location}
	}

	if opts.Body != "" {
		contentType := "text"
		if opts.IsHTML {
			contentType = "html"
		}
		body["body"] = map[string]string{
			"contentType": contentType,
			"content":     opts.Body,
		}
	}

	if opts.IsAllDay {
		body["isAllDay"] = true
	}

	if len(opts.Attendees) > 0 {
		attendees := make([]map[string]interface{}, len(opts.Attendees))
		for i, addr := range opts.Attendees {
			attendees[i] = map[string]interface{}{
				"emailAddress": map[string]string{
					"address": graph.ParseEmail(addr),
				},
				"type": "required",
			}
		}
		body["attendees"] = attendees
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.DoRequest("POST", endpoint, jsonBody)
	if err != nil {
		return nil, err
	}

	var ev GraphEventResponse
	if err := json.Unmarshal(resp, &ev); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	event := graphEventToEvent(ev)
	return &event, nil
}

// UpdateEvent updates an existing calendar event.
func (c *Client) UpdateEvent(eventID string, opts UpdateEventOptions) (*Event, error) {
	endpoint := fmt.Sprintf("%s/me/events/%s", graph.GraphAPIBaseURL, url.PathEscape(eventID))

	body := map[string]interface{}{}

	if opts.Subject != nil {
		body["subject"] = *opts.Subject
	}
	if opts.Start != nil {
		body["start"] = toGraphDateTime(*opts.Start)
	}
	if opts.End != nil {
		body["end"] = toGraphDateTime(*opts.End)
	}
	if opts.Location != nil {
		body["location"] = map[string]string{"displayName": *opts.Location}
	}
	if opts.Body != nil {
		contentType := "text"
		if opts.IsHTML {
			contentType = "html"
		}
		body["body"] = map[string]string{
			"contentType": contentType,
			"content":     *opts.Body,
		}
	}
	if opts.Attendees != nil {
		attendees := make([]map[string]interface{}, len(opts.Attendees))
		for i, addr := range opts.Attendees {
			attendees[i] = map[string]interface{}{
				"emailAddress": map[string]string{
					"address": graph.ParseEmail(addr),
				},
				"type": "required",
			}
		}
		body["attendees"] = attendees
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.DoRequest("PATCH", endpoint, jsonBody)
	if err != nil {
		return nil, err
	}

	var ev GraphEventResponse
	if err := json.Unmarshal(resp, &ev); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	event := graphEventToEvent(ev)
	return &event, nil
}

// DeleteEvent deletes a calendar event.
func (c *Client) DeleteEvent(eventID string) error {
	endpoint := fmt.Sprintf("%s/me/events/%s", graph.GraphAPIBaseURL, url.PathEscape(eventID))
	_, err := c.DoRequest("DELETE", endpoint, nil)
	return err
}

// RespondToEvent accepts, declines, or tentatively accepts an event invitation.
func (c *Client) RespondToEvent(eventID string, response ResponseType, comment string) error {
	endpoint := fmt.Sprintf("%s/me/events/%s/%s", graph.GraphAPIBaseURL, url.PathEscape(eventID), string(response))

	body := map[string]interface{}{
		"sendResponse": true,
	}
	if comment != "" {
		body["comment"] = comment
	}

	jsonBody, _ := json.Marshal(body)
	_, err := c.DoRequest("POST", endpoint, jsonBody)
	return err
}

// graphEventToEvent converts a Graph API event to our Event struct.
func graphEventToEvent(ev GraphEventResponse) Event {
	event := Event{
		ID:             ev.ID,
		Subject:        ev.Subject,
		Location:       ev.Location.DisplayName,
		IsAllDay:       ev.IsAllDay,
		IsOnline:       ev.IsOnlineMeeting,
		OnlineURL:      ev.OnlineMeetingUrl,
		ShowAs:         ev.ShowAs,
		ResponseStatus: ev.ResponseStatus.Response,
	}

	event.Start = parseGraphDateTime(ev.Start)
	event.End = parseGraphDateTime(ev.End)

	event.Organizer = graph.FormatGraphAddress(ev.Organizer.EmailAddress)

	for _, att := range ev.Attendees {
		event.Attendees = append(event.Attendees, Attendee{
			Name:     att.EmailAddress.Name,
			Email:    att.EmailAddress.Address,
			Type:     att.Type,
			Response: att.Status.Response,
		})
	}

	return event
}

// parseGraphDateTime parses a GraphDateTimeTimeZone into a time.Time.
func parseGraphDateTime(dt GraphDateTimeTimeZone) time.Time {
	loc := resolveTimezone(dt.TimeZone)

	// Graph API returns datetime like "2024-01-15T10:00:00.0000000"
	layouts := []string{
		"2006-01-02T15:04:05.0000000",
		"2006-01-02T15:04:05",
		time.RFC3339,
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, dt.DateTime, loc); err == nil {
			return t
		}
	}

	// Fallback: try parsing as-is
	if t, err := time.Parse(time.RFC3339, dt.DateTime); err == nil {
		return t
	}

	return time.Time{}
}

// toGraphDateTime converts a time.Time to a GraphDateTimeTimeZone.
func toGraphDateTime(t time.Time) GraphDateTimeTimeZone {
	return GraphDateTimeTimeZone{
		DateTime: t.Format("2006-01-02T15:04:05"),
		TimeZone: t.Location().String(),
	}
}

// resolveTimezone maps a timezone name (including Windows names) to a Go location.
func resolveTimezone(tz string) *time.Location {
	if tz == "" || strings.EqualFold(tz, "UTC") {
		return time.UTC
	}

	// Try IANA name first
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}

	// Windows timezone name mapping
	windowsToIANA := map[string]string{
		"W. Europe Standard Time":       "Europe/Berlin",
		"Central European Standard Time": "Europe/Budapest",
		"Romance Standard Time":          "Europe/Paris",
		"Central Europe Standard Time":   "Europe/Prague",
		"GMT Standard Time":              "Europe/London",
		"Eastern Standard Time":          "America/New_York",
		"Central Standard Time":          "America/Chicago",
		"Mountain Standard Time":         "America/Denver",
		"Pacific Standard Time":          "America/Los_Angeles",
		"AUS Eastern Standard Time":      "Australia/Sydney",
		"Tokyo Standard Time":            "Asia/Tokyo",
		"China Standard Time":            "Asia/Shanghai",
		"India Standard Time":            "Asia/Kolkata",
		"Arabian Standard Time":          "Asia/Dubai",
		"Russian Standard Time":          "Europe/Moscow",
		"New Zealand Standard Time":      "Pacific/Auckland",
		"Singapore Standard Time":        "Asia/Singapore",
		"Korea Standard Time":            "Asia/Seoul",
		"Taipei Standard Time":           "Asia/Taipei",
		"SE Asia Standard Time":          "Asia/Bangkok",
		"E. Africa Standard Time":        "Africa/Nairobi",
		"South Africa Standard Time":     "Africa/Johannesburg",
		"Israel Standard Time":           "Asia/Jerusalem",
		"Turkey Standard Time":           "Europe/Istanbul",
		"FLE Standard Time":              "Europe/Helsinki",
		"E. Europe Standard Time":        "Europe/Chisinau",
		"Hawaiian Standard Time":         "Pacific/Honolulu",
		"Alaskan Standard Time":          "America/Anchorage",
		"Atlantic Standard Time":         "America/Halifax",
		"SA Pacific Standard Time":       "America/Bogota",
		"SA Eastern Standard Time":       "America/Cayenne",
		"E. South America Standard Time": "America/Sao_Paulo",
	}

	if iana, ok := windowsToIANA[tz]; ok {
		if loc, err := time.LoadLocation(iana); err == nil {
			return loc
		}
	}

	return time.UTC
}
