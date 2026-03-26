package calendar

import (
	"time"

	"github.com/yourname/o365-cli/internal/graph"
)

// Event represents a calendar event in the application layer.
type Event struct {
	ID             string     `json:"id"`
	Account        string     `json:"account,omitempty"`
	Subject        string     `json:"subject"`
	Start          time.Time  `json:"start"`
	End            time.Time  `json:"end"`
	Location       string     `json:"location,omitempty"`
	Body           string     `json:"body,omitempty"`
	Organizer      string     `json:"organizer,omitempty"`
	Attendees      []Attendee `json:"attendees,omitempty"`
	IsAllDay       bool       `json:"is_all_day"`
	IsOnline       bool       `json:"is_online"`
	OnlineURL      string     `json:"online_url,omitempty"`
	ShowAs         string     `json:"show_as,omitempty"`
	ResponseStatus string     `json:"response_status,omitempty"`
}

// Attendee represents an event attendee.
type Attendee struct {
	Name     string `json:"name,omitempty"`
	Email    string `json:"email"`
	Type     string `json:"type"`
	Response string `json:"response"`
}

// CreateEventOptions contains options for creating an event.
type CreateEventOptions struct {
	Subject   string
	Body      string
	IsHTML    bool
	Start     time.Time
	End       time.Time
	Location  string
	Attendees []string
	IsAllDay  bool
}

// UpdateEventOptions contains options for updating an event.
// Pointer fields: nil means "don't update", non-nil means "set to this value".
type UpdateEventOptions struct {
	Subject   *string
	Body      *string
	IsHTML    bool
	Start     *time.Time
	End       *time.Time
	Location  *string
	Attendees []string
}

// ResponseType represents the type of response to an event invitation.
type ResponseType string

const (
	ResponseAccept    ResponseType = "accept"
	ResponseDecline   ResponseType = "decline"
	ResponseTentative ResponseType = "tentativelyAccept"
)

// --- Graph API response types ---

// GraphEventResponse represents an event from Graph API.
type GraphEventResponse struct {
	ID               string                       `json:"id"`
	Subject          string                       `json:"subject"`
	Start            GraphDateTimeTimeZone        `json:"start"`
	End              GraphDateTimeTimeZone        `json:"end"`
	Location         GraphLocation                `json:"location"`
	Body             graph.GraphBodyResponse      `json:"body"`
	Organizer        graph.GraphEmailAddressWrapper `json:"organizer"`
	Attendees        []GraphAttendee              `json:"attendees"`
	IsAllDay         bool                         `json:"isAllDay"`
	IsOnlineMeeting  bool                         `json:"isOnlineMeeting"`
	OnlineMeetingUrl string                       `json:"onlineMeetingUrl"`
	ShowAs           string                       `json:"showAs"`
	ResponseStatus   GraphResponseStatus          `json:"responseStatus"`
}

// GraphEventsResponse represents the list response for events.
type GraphEventsResponse struct {
	Value    []GraphEventResponse `json:"value"`
	NextLink string               `json:"@odata.nextLink"`
}

// GraphDateTimeTimeZone represents a date/time with timezone from Graph API.
type GraphDateTimeTimeZone struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// GraphLocation represents a location from Graph API.
type GraphLocation struct {
	DisplayName string `json:"displayName"`
}

// GraphAttendee represents an attendee from Graph API.
type GraphAttendee struct {
	EmailAddress graph.GraphEmailAddress `json:"emailAddress"`
	Type         string                  `json:"type"`
	Status       GraphResponseStatus     `json:"status"`
}

// GraphResponseStatus represents a response status from Graph API.
type GraphResponseStatus struct {
	Response string `json:"response"`
	Time     string `json:"time"`
}
