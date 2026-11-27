// Package teams reads Microsoft Teams chats, teams and channels through Microsoft Graph.
package teams

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/yourname/o365-cli/internal/graph"
)

// Client wraps the generic Graph client with Teams read operations.
type Client struct {
	*graph.Client
}

// NewClient creates a Teams client for a token from the Teams login.
func NewClient(accessToken string) *Client {
	return &Client{Client: graph.NewClient(accessToken)}
}

type Chat struct {
	ID              string     `json:"id"`
	Topic           string     `json:"topic"`
	ChatType        string     `json:"chatType"`
	Members         []string   `json:"members"`
	LastMessageTime *time.Time `json:"lastMessageTime"`
}

type Message struct {
	ID     string    `json:"id"`
	Sender string    `json:"sender"`
	Time   time.Time `json:"time"`
	Text   string    `json:"text"`
	// Attachments names files and cards; a message with only an attachment has an empty text.
	Attachments []string `json:"attachments"`
}

// ChannelMessage is a top-level channel post; Replies counts its thread replies.
type ChannelMessage struct {
	Message
	Replies int `json:"replies"`
}

type Team struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Channel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	MembershipType string `json:"membershipType"`
}

type identity struct {
	DisplayName string `json:"displayName"`
}

// list follows @odata.nextLink pages and decodes up to limit items (0 = all) of path into out.
func list[T any](c *Client, path string, limit int) ([]T, error) {
	var items []T
	next := graph.GraphAPIBaseURL + path
	for next != "" {
		resp, err := c.DoRequest("GET", next, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Value    []T    `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(resp, &page); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		for _, item := range page.Value {
			items = append(items, item)
			if limit > 0 && len(items) >= limit {
				return items, nil
			}
		}
		next = page.NextLink
	}
	return items, nil
}

// pageSize keeps one Graph page within the limit; chat endpoints accept at most 50.
func pageSize(limit int) int {
	if limit <= 0 || limit > 50 {
		return 50
	}
	return limit
}

// ListChats returns the user's chats, most recent message first.
func (c *Client) ListChats(limit int) ([]Chat, error) {
	raw, err := list[struct {
		ID                  string `json:"id"`
		Topic               string `json:"topic"`
		ChatType            string `json:"chatType"`
		Members             []identity
		LastMessagePreview  *struct{ CreatedDateTime time.Time } `json:"lastMessagePreview"`
		LastUpdatedDateTime *time.Time                           `json:"lastUpdatedDateTime"`
	}](c, fmt.Sprintf("/me/chats?$expand=members,lastMessagePreview&$orderby=lastMessagePreview/createdDateTime%%20desc&$top=%d", pageSize(limit)), limit)
	if err != nil {
		return nil, err
	}
	chats := make([]Chat, 0, len(raw))
	for _, r := range raw {
		chat := Chat{ID: r.ID, Topic: r.Topic, ChatType: r.ChatType, Members: []string{}, LastMessageTime: r.LastUpdatedDateTime}
		if r.LastMessagePreview != nil {
			chat.LastMessageTime = &r.LastMessagePreview.CreatedDateTime
		}
		for _, m := range r.Members {
			chat.Members = append(chat.Members, m.DisplayName)
		}
		chats = append(chats, chat)
	}
	return chats, nil
}

type rawMessage struct {
	ID              string    `json:"id"`
	MessageType     string    `json:"messageType"`
	CreatedDateTime time.Time `json:"createdDateTime"`
	From            *struct {
		User        *identity `json:"user"`
		Application *identity `json:"application"`
	} `json:"from"`
	Body struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Attachments []struct {
		Name string `json:"name"`
	} `json:"attachments"`
}

func (r rawMessage) message() Message {
	msg := Message{ID: r.ID, Time: r.CreatedDateTime, Text: r.Body.Content, Attachments: []string{}}
	for _, a := range r.Attachments {
		msg.Attachments = append(msg.Attachments, a.Name)
	}
	if r.Body.ContentType == "html" {
		msg.Text = HTMLToText(r.Body.Content)
	}
	if r.From != nil && r.From.User != nil {
		msg.Sender = r.From.User.DisplayName
	} else if r.From != nil && r.From.Application != nil {
		msg.Sender = r.From.Application.DisplayName
	}
	return msg
}

// ListMessages returns the newest limit messages of a chat, newest first.
// System events (members added, calls) count towards limit but are left out.
func (c *Client) ListMessages(chatID string, limit int) ([]Message, error) {
	raw, err := list[rawMessage](c, fmt.Sprintf("/me/chats/%s/messages?$orderby=createdDateTime%%20desc&$top=%d", url.PathEscape(chatID), pageSize(limit)), limit)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	for _, r := range raw {
		if r.MessageType == "message" {
			msgs = append(msgs, r.message())
		}
	}
	return msgs, nil
}

// ListChannelMessages returns the newest limit top-level posts of a channel with their reply count, newest first.
// System events count towards limit but are left out, as in ListMessages.
// ponytail: Replies counts the replies Graph expands with a post (up to 1000); fetch /replies if threads get longer.
func (c *Client) ListChannelMessages(teamID, channelID string, limit int) ([]ChannelMessage, error) {
	raw, err := list[struct {
		rawMessage
		Replies []struct{} `json:"replies"`
	}](c, fmt.Sprintf("/teams/%s/channels/%s/messages?$expand=replies&$top=%d", url.PathEscape(teamID), url.PathEscape(channelID), pageSize(limit)), limit)
	if err != nil {
		return nil, err
	}
	msgs := []ChannelMessage{}
	for _, r := range raw {
		if r.MessageType == "message" {
			msgs = append(msgs, ChannelMessage{Message: r.message(), Replies: len(r.Replies)})
		}
	}
	return msgs, nil
}

// ListTeams returns the teams the user has joined.
func (c *Client) ListTeams() ([]Team, error) {
	raw, err := list[struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}](c, "/me/joinedTeams", 0)
	if err != nil {
		return nil, err
	}
	teams := make([]Team, 0, len(raw))
	for _, r := range raw {
		teams = append(teams, Team{ID: r.ID, Name: r.DisplayName, Description: r.Description})
	}
	return teams, nil
}

// ListChannels returns the channels of a team.
func (c *Client) ListChannels(teamID string) ([]Channel, error) {
	raw, err := list[struct {
		ID             string `json:"id"`
		DisplayName    string `json:"displayName"`
		Description    string `json:"description"`
		MembershipType string `json:"membershipType"`
	}](c, fmt.Sprintf("/teams/%s/channels", url.PathEscape(teamID)), 0)
	if err != nil {
		return nil, err
	}
	channels := make([]Channel, 0, len(raw))
	for _, r := range raw {
		channels = append(channels, Channel{ID: r.ID, Name: r.DisplayName, Description: r.Description, MembershipType: r.MembershipType})
	}
	return channels, nil
}

var (
	blockTag = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|li|tr|h[1-6])>`)
	anyTag   = regexp.MustCompile(`<[^>]*>`)
	blanks   = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText turns a Teams HTML message body into plain text.
// ponytail: regexp, not an HTML parser; good enough for Teams' simple markup, swap for x/net/html if bodies get richer.
func HTMLToText(s string) string {
	s = blockTag.ReplaceAllString(s, "\n")
	s = html.UnescapeString(anyTag.ReplaceAllString(s, ""))
	s = strings.ReplaceAll(s, " ", " ")
	return strings.TrimSpace(blanks.ReplaceAllString(s, "\n\n"))
}
