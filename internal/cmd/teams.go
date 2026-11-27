package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/auth"
	"github.com/yourname/o365-cli/internal/profile"
	"github.com/yourname/o365-cli/internal/teams"
)

var teamsCmd = &cobra.Command{
	Use:   "teams",
	Short: "Read and post Microsoft Teams chats and channels (JSON)",
	Long: `Reads Microsoft Teams through the separate Teams login (o365-cli auth login --teams).
All commands print JSON. With several Teams logins, pass --account.`,
}

var teamsChatsLimit, teamsMessagesLimit, teamsChannelMessagesLimit int
var teamsSendChat, teamsSendTeam, teamsSendChannel, teamsSendText string

var teamsChatsCmd = &cobra.Command{
	Use:   "chats",
	Short: "List chats, most recent first",
	Long: `Lists chats with id, topic, members and last message time.

Examples:
  o365-cli teams chats
  o365-cli teams chats --limit 10 --account user@example.com`,
	Annotations: map[string]string{profile.AnnotationKey: "teams.read"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTeams(teamsLogin, func(c *teams.Client) (any, error) { return c.ListChats(teamsChatsLimit) })
	},
}

var teamsMessagesCmd = &cobra.Command{
	Use:   "messages CHAT_ID",
	Short: "Show the newest messages of a chat",
	Long: `Shows sender, time and plain text of the newest messages of a chat, newest first.
System events (members added, calls) count towards --limit but are left out.

Examples:
  o365-cli teams messages 19:abc...@thread.v2 --limit 20`,
	Annotations: map[string]string{profile.AnnotationKey: "teams.read"},
	Args:        cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTeams(teamsLogin, func(c *teams.Client) (any, error) { return c.ListMessages(args[0], teamsMessagesLimit) })
	},
}

var teamsListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List the teams you have joined",
	Annotations: map[string]string{profile.AnnotationKey: "teams.read"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTeams(teamsLogin, func(c *teams.Client) (any, error) { return c.ListTeams() })
	},
}

var teamsChannelsCmd = &cobra.Command{
	Use:         "channels TEAM_ID",
	Short:       "List the channels of a team",
	Annotations: map[string]string{profile.AnnotationKey: "teams.read"},
	Args:        cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTeams(teamsLogin, func(c *teams.Client) (any, error) { return c.ListChannels(args[0]) })
	},
}

var teamsChannelMessagesCmd = &cobra.Command{
	Use:   "channel-messages TEAM_ID CHANNEL_ID",
	Short: "Show the most recently active posts of a team channel",
	Long: `Shows sender, time, plain text and reply count of the most recently active top-level posts of a
channel. Graph orders posts by the last activity in their thread, so a new reply moves an old post to the top.
Needs the separate channel message login (o365-cli auth login --channel-messages), which a tenant admin
must accept for the organisation. Get TEAM_ID from 'teams list' and CHANNEL_ID from 'teams channels'.

Examples:
  o365-cli teams channel-messages TEAM_ID 19:abc...@thread.tacv2 --limit 20`,
	Annotations: map[string]string{profile.AnnotationKey: "teams.read"},
	Args:        cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTeams(channelMessageLogin, func(c *teams.Client) (any, error) {
			msgs, err := c.ListChannelMessages(args[0], args[1], teamsChannelMessagesLimit)
			if err != nil && strings.Contains(err.Error(), "status 403") {
				return nil, fmt.Errorf("%w: either you are not a member of this channel or the tenant lacks admin consent. %s", err, channelConsentHint)
			}
			return msgs, err
		})
	},
}

var teamsSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Post a plain-text message to a chat or a channel",
	Long: `Posts --text as plain text, as the signed-in user, to a chat (--chat) or a
channel (--team and --channel). Prints the id and web link of the new message.
A sent message cannot be recalled unseen: send only text and recipients the owner approved.

Examples:
  o365-cli teams send --chat 19:abc...@thread.v2 --text "On my way"
  o365-cli teams send --team TEAM_ID --channel 19:def...@thread.tacv2 --text "Build is green"`,
	Annotations: map[string]string{profile.AnnotationKey: "teams.send"},
	Args:        cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		toChat := teamsSendChat != "" && teamsSendTeam == "" && teamsSendChannel == ""
		toChannel := teamsSendChat == "" && teamsSendTeam != "" && teamsSendChannel != ""
		if !toChat && !toChannel {
			return fmt.Errorf("pass either --chat or both --team and --channel")
		}
		if strings.TrimSpace(teamsSendText) == "" {
			return fmt.Errorf("--text must not be empty")
		}
		return runTeams(teamsLogin, func(c *teams.Client) (any, error) {
			return c.SendMessage(teamsSendChat, teamsSendTeam, teamsSendChannel, teamsSendText)
		})
	},
}

func init() {
	teamsSendCmd.Flags().StringVar(&teamsSendChat, "chat", "", "Chat id (from 'teams chats')")
	teamsSendCmd.Flags().StringVar(&teamsSendTeam, "team", "", "Team id (from 'teams list'), with --channel")
	teamsSendCmd.Flags().StringVar(&teamsSendChannel, "channel", "", "Channel id (from 'teams channels'), with --team")
	teamsSendCmd.Flags().StringVar(&teamsSendText, "text", "", "Message text, sent as plain text")
	teamsChatsCmd.Flags().IntVar(&teamsChatsLimit, "limit", 50, "Maximum number of chats (1-1000)")
	teamsMessagesCmd.Flags().IntVar(&teamsMessagesLimit, "limit", 20, "Maximum number of messages (1-1000)")
	teamsChannelMessagesCmd.Flags().IntVar(&teamsChannelMessagesLimit, "limit", 20, "Maximum number of posts (1-1000)")
	teamsCmd.AddCommand(teamsChatsCmd, teamsMessagesCmd, teamsListCmd, teamsChannelsCmd, teamsChannelMessagesCmd, teamsSendCmd)
	rootCmd.AddCommand(teamsCmd)
}

// teamsLogin and channelMessageLogin name a login's token client and the flag that creates it.
type teamsLoginKind struct {
	newClient func(clientID, cacheDir string) (*auth.OAuthClient, error)
	name      string
	flag      string
}

var (
	teamsLogin          = teamsLoginKind{auth.NewTeamsOAuthClient, "Teams", "--teams"}
	channelMessageLogin = teamsLoginKind{auth.NewChannelMessageOAuthClient, "channel message", "--channel-messages"}
)

func runTeams(login teamsLoginKind, fetch func(*teams.Client) (any, error)) error {
	for _, limit := range []int{teamsChatsLimit, teamsMessagesLimit, teamsChannelMessagesLimit} {
		if limit < 1 || limit > 1000 {
			return fmt.Errorf("--limit must be between 1 and 1000, got %d", limit)
		}
	}
	ctx := context.Background()
	token, err := getTeamsToken(ctx, login)
	if err != nil {
		return err
	}
	data, err := fetch(teams.NewClient(token))
	if err != nil {
		return err
	}
	return calOutputJSON(data)
}

// getTeamsToken resolves the account among the logins of one kind like requireAccount does among mail logins.
func getTeamsToken(ctx context.Context, login teamsLoginKind) (string, error) {
	client, err := login.newClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return "", err
	}
	email := getExplicitAccount()
	if email == "" {
		accounts, err := client.ListAccounts(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to read %s logins: %w", login.name, err)
		}
		switch len(accounts) {
		case 0:
			return "", fmt.Errorf("no %s login. Please run 'auth login %s'", login.name, login.flag)
		case 1:
			email = accounts[0]
		default:
			return "", fmt.Errorf("multiple %s logins, --account required. Use 'auth status' to see them", login.name)
		}
	}
	token, err := client.GetAccessToken(ctx, email)
	if err != nil {
		return "", fmt.Errorf("no %s login for %s; run 'o365-cli auth login %s --account %s': %w", login.name, email, login.flag, email, err)
	}
	return token, nil
}
