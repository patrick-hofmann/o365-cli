package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/auth"
	"github.com/yourname/o365-cli/internal/config"
	"github.com/yourname/o365-cli/internal/profile"
)

var logoutAll bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
	Long:  "Commands for login, logout, and OAuth2 authentication status.",
}

var loginCategories, loginTeams, loginChannelMessages bool

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to Office 365",
	Long: `Starts the OAuth2 Device Code Flow.

You will receive a code to enter in your browser at microsoft.com/devicelogin.
After successful authentication, a token is stored locally.`,
	Annotations: map[string]string{profile.AnnotationKey: "auth"},
	RunE:        runLogin,
}

var logoutCmd = &cobra.Command{
	Use:   "logout [email]",
	Short: "Logout and delete token",
	Long: `Logs out an account and deletes its token.

Without argument, logs out the active account.
With --all, logs out all accounts.

Examples:
  o365-cli auth logout
  o365-cli auth logout user@example.com
  o365-cli auth logout --all`,
	Annotations: map[string]string{profile.AnnotationKey: "auth"},
	Args:        cobra.MaximumNArgs(1),
	RunE:        runLogout,
}

var statusCmd = &cobra.Command{
	Use:         "status",
	Short:       "Show current auth status",
	Annotations: map[string]string{profile.AnnotationKey: "auth"},
	RunE:        runStatus,
}

var listCmd = &cobra.Command{
	Use:         "list",
	Short:       "List all logged-in accounts",
	Annotations: map[string]string{profile.AnnotationKey: "auth"},
	RunE:        runList,
}

var debugCmd = &cobra.Command{
	Use:   "debug <email>",
	Short: "Debug token issues",
	Long: `Shows detailed diagnostic information about token state.

This command helps diagnose why authentication might be failing
by showing the actual error from token refresh attempts.

Examples:
  o365-cli auth debug user@example.com`,
	Annotations: map[string]string{profile.AnnotationKey: "auth"},
	Args:        cobra.ExactArgs(1),
	RunE:        runDebug,
}

func init() {
	logoutCmd.Flags().BoolVar(&logoutAll, "all", false, "Logout all accounts")
	loginCmd.Flags().BoolVar(&loginCategories, "categories", false, "Also allow workflow ensure-categories to add names to the Outlook category list")
	loginCmd.Flags().BoolVar(&loginTeams, "teams", false, "Grant only Microsoft Teams access (chats, teams, channels), separately from mail")
	loginCmd.Flags().BoolVar(&loginChannelMessages, "channel-messages", false, "Grant only reading team channel messages (ChannelMessage.Read.All, needs tenant admin consent)")
	loginCmd.MarkFlagsMutuallyExclusive("categories", "teams", "channel-messages")

	authCmd.AddCommand(loginCmd)
	authCmd.AddCommand(logoutCmd)
	authCmd.AddCommand(statusCmd)
	authCmd.AddCommand(listCmd)
	authCmd.AddCommand(debugCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Create OAuth client; --teams and --channel-messages ask for their scopes alone so other consents stay untouched
	newClient, cacheDirOf := auth.NewOAuthClient, func(dir string) (string, error) { return dir, nil }
	switch {
	case loginTeams:
		newClient, cacheDirOf = auth.NewTeamsOAuthClient, auth.TeamsCacheDir
	case loginChannelMessages:
		newClient, cacheDirOf = auth.NewChannelMessageOAuthClient, auth.ChannelMessageCacheDir
	}
	oauthClient, err := newClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to create OAuth client: %w", err)
	}
	if loginCategories {
		oauthClient.ConsentToCategoryList()
	}

	// Start device code flow
	printInfo("Starting login...")

	deviceCode, resultChan, err := oauthClient.StartDeviceCodeFlow(ctx)
	if err != nil {
		return fmt.Errorf("failed to start device code flow: %w", err)
	}

	// Show instructions (agent-friendly plain text + visual box for humans)
	fmt.Println()
	fmt.Printf("Login URL:  %s\n", deviceCode.VerificationURL)
	fmt.Printf("Login Code: %s\n", deviceCode.UserCode)
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  To sign in, open this URL in your browser:                ║")
	fmt.Printf("║  %s%s║\n", deviceCode.VerificationURL, spaces(36-len(deviceCode.VerificationURL)))
	fmt.Println("║                                                            ║")
	fmt.Println("║  And enter this code:                                      ║")
	fmt.Printf("║                        %s                            ║\n", deviceCode.UserCode)
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Copy code to clipboard and open browser
	if copyToClipboard(deviceCode.UserCode) {
		printInfo("Code copied to clipboard!")
	}
	if openBrowser(deviceCode.VerificationURL) {
		printInfo("Browser opened. Paste the code and sign in.")
	} else {
		printInfo("Open the URL above and enter the code.")
	}
	printInfo("Waiting for browser login...")

	// Wait for result
	result := <-resultChan

	if result.Error != nil {
		if hint := consentHint(result.Error); hint != "" {
			printInfo("%s", hint)
			if loginChannelMessages {
				printInfo("%s", channelConsentHint)
			}
		}
		return fmt.Errorf("authentication failed: %w", result.Error)
	}

	// Add account to accounts.yaml; a Teams or channel message login must not make an account look usable for mail
	if !loginTeams && !loginChannelMessages {
		if err := config.AddAccount(result.Email); err != nil {
			printError(fmt.Errorf("failed to save account: %w", err))
		}
	}

	fmt.Println()
	printSuccess("Successfully logged in as %s", result.Email)
	printInfo("Token valid until: %s", result.ExpiresAt.Format(time.RFC1123))
	cacheDir, _ := cacheDirOf(cfg.CacheDir)
	printInfo("Token saved in: %s/token.json", cacheDir)

	return nil
}

func runLogout(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	clients, err := loginClients()
	if err != nil {
		return err
	}

	// --all flag: Logout all accounts
	if logoutAll {
		for _, l := range clients {
			if err := l.client.LogoutAll(ctx); err != nil {
				return fmt.Errorf("%s logout failed: %w", l.name, err)
			}
		}
		if err := config.RemoveAllAccounts(); err != nil {
			printError(fmt.Errorf("failed to remove accounts from config: %w", err))
		}
		printSuccess("All accounts logged out")
		printInfo("Local tokens deleted.")
		return nil
	}

	// Argument required (no implicit "active account" anymore)
	if len(args) == 0 {
		return fmt.Errorf("email argument required. Use 'auth logout <email>' or 'auth logout --all'")
	}
	email := args[0]

	// Logout from mail, Teams and channel messages; each cache may hold the account or not
	found := false
	for _, l := range clients {
		has, err := hasAccount(ctx, l.client, email)
		if err != nil {
			return fmt.Errorf("%s logout failed: %w", l.name, err)
		}
		if !has {
			continue
		}
		found = true
		if err := l.client.Logout(ctx, email); err != nil {
			return fmt.Errorf("%s logout failed: %w", l.name, err)
		}
	}
	if !found {
		printInfo("Account %s is not logged in.", email)
		return nil
	}

	// Remove from accounts.yaml
	if err := config.RemoveAccount(email); err != nil {
		printError(fmt.Errorf("failed to remove account from config: %w", err))
	}

	printSuccess("Successfully logged out from %s", email)
	printInfo("Token deleted.")

	return nil
}

type namedLogin struct {
	name   string
	client *auth.OAuthClient
}

// loginClients returns the mail, Teams and channel message logins, each with its own token cache.
func loginClients() ([]namedLogin, error) {
	var logins []namedLogin
	for _, l := range []struct {
		name string
		new  func(string, string) (*auth.OAuthClient, error)
	}{{"mail", auth.NewOAuthClient}, {"Teams", auth.NewTeamsOAuthClient}, {"channel messages", auth.NewChannelMessageOAuthClient}} {
		client, err := l.new(cfg.ClientID, cfg.CacheDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create OAuth client: %w", err)
		}
		logins = append(logins, namedLogin{l.name, client})
	}
	return logins, nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	oauthClient, err := auth.NewOAuthClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to create OAuth client: %w", err)
	}

	statuses, err := oauthClient.GetAllStatuses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	// Teams and channel message state come from their own caches without a token request:
	// an account is there only after `auth login --teams` or `--channel-messages`
	clients, err := loginClients()
	if err != nil {
		return err
	}
	extra := clients[1:]
	loggedIn := make([]map[string]bool, len(extra))
	var others []string // accounts with a Teams or channel message login but no mail login
	for i, l := range extra {
		accounts, err := l.client.ListAccounts(ctx)
		if err != nil {
			return fmt.Errorf("failed to read %s logins: %w", l.name, err)
		}
		loggedIn[i] = map[string]bool{}
		for _, email := range accounts {
			loggedIn[i][email] = true
			if !slices.Contains(others, email) {
				others = append(others, email)
			}
		}
	}
	grants := func(email string) string {
		parts := make([]string, len(extra))
		for i, l := range extra {
			state := "not logged in"
			if loggedIn[i][email] {
				state = "logged in"
			}
			parts[i] = l.name + ": " + state
		}
		return strings.Join(parts, ", ")
	}

	fmt.Println()
	fmt.Println("Auth Status")
	fmt.Println("───────────")

	if len(statuses) == 0 && len(others) == 0 {
		printInfo("Status: Not logged in")
		printInfo("\nUse 'auth login' to sign in.")
		return nil
	}

	hasExpiredToken := false

	for _, status := range statuses {
		teams := grants(status.Email)
		others = slices.DeleteFunc(others, func(e string) bool { return e == status.Email })
		if status.TokenExpired {
			fmt.Printf("  %s (token expired, %s)\n", status.Email, teams)
			hasExpiredToken = true
		} else {
			remaining := time.Until(status.ExpiresAt)
			var remainingStr string
			if remaining > time.Hour {
				remainingStr = fmt.Sprintf("%.0fh", remaining.Hours())
			} else {
				remainingStr = fmt.Sprintf("%.0fm", remaining.Minutes())
			}
			fmt.Printf("  %s (valid, %s remaining, %s)\n", status.Email, remainingStr, teams)
		}
	}

	for _, email := range others {
		fmt.Printf("  %s (no mail login, %s)\n", email, grants(email))
	}

	if hasExpiredToken {
		fmt.Println()
		printInfo("Some tokens are expired. Run 'auth debug <email>' for details.")
	}

	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	accounts, err := config.LoadAccounts()
	if err != nil {
		return fmt.Errorf("failed to load accounts: %w", err)
	}

	if len(accounts) == 0 {
		printInfo("No accounts logged in.")
		printInfo("\nUse 'auth login' to sign in.")
		return nil
	}

	fmt.Println()
	fmt.Println("Logged-in Accounts")
	fmt.Println("──────────────────")

	for _, acc := range accounts {
		addedAt := acc.AddedAt.Format("2006-01-02 15:04")
		fmt.Printf("  %s (added: %s)\n", acc.Email, addedAt)
	}

	fmt.Println()
	printInfo("Use --account <email> to target a specific account for write operations.")

	return nil
}

func runDebug(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	email := args[0]

	oauthClient, err := auth.NewOAuthClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to create OAuth client: %w", err)
	}

	fmt.Println()
	fmt.Println("Token Debug Info")
	fmt.Println("────────────────")

	status, err := oauthClient.GetDetailedStatus(ctx, email)
	if err != nil {
		return fmt.Errorf("failed to get detailed status: %w", err)
	}

	fmt.Printf("Account:           %s\n", status.Email)
	fmt.Printf("Token cache:       %s\n", status.CacheFile)

	if status.CacheSize > 0 {
		fmt.Printf("Cache size:        %.1f KB\n", float64(status.CacheSize)/1024)
	} else {
		fmt.Printf("Cache size:        (file not found)\n")
	}

	fmt.Printf("Cached accounts:   %d\n", status.CachedAccounts)
	fmt.Printf("Has cached token:  %v\n", status.HasCachedToken)
	fmt.Printf("Refresh present:   %v\n", status.RefreshPresent)

	if !status.AccessExpiry.IsZero() {
		if time.Now().After(status.AccessExpiry) {
			fmt.Printf("Access expires:    %s (expired %s ago)\n",
				status.AccessExpiry.Format("2006-01-02 15:04:05"),
				time.Since(status.AccessExpiry).Round(time.Minute))
		} else {
			fmt.Printf("Access expires:    %s (%s remaining)\n",
				status.AccessExpiry.Format("2006-01-02 15:04:05"),
				time.Until(status.AccessExpiry).Round(time.Minute))
		}
	}

	fmt.Println()

	if status.SilentRefreshOK {
		printSuccess("Silent token refresh: SUCCESS")
		printInfo("Token is valid and can be refreshed silently.")
	} else {
		printError(fmt.Errorf("Silent token refresh: FAILED"))
		if status.LastError != "" {
			fmt.Println()
			fmt.Println("Error details:")
			fmt.Printf("  %s\n", status.LastError)
		}
		fmt.Println()
		printInfo("Possible causes:")
		printInfo("  - Refresh token expired (check Azure AD tenant policies)")
		printInfo("  - Token cache corruption (try 'auth logout' then 'auth login')")
		printInfo("  - Scope mismatch (re-authenticate to request correct scopes)")
		printInfo("  - Network/temporary error (retry later)")
	}

	return nil
}

// hasAccount reports whether the client's token cache holds email.
func hasAccount(ctx context.Context, client *auth.OAuthClient, email string) (bool, error) {
	accounts, err := client.ListAccounts(ctx)
	if err != nil {
		return false, err
	}
	return slices.Contains(accounts, email), nil
}

const channelConsentHint = "Reading channel messages needs ChannelMessage.Read.All: a tenant admin runs 'o365-cli auth login --channel-messages' and accepts it on behalf of the organisation."

var aadstsCode = regexp.MustCompile(`AADSTS\d+`)

// consentHint names the AADSTS code of a refused login and what the tenant does; empty without a code.
func consentHint(err error) string {
	code := aadstsCode.FindString(err.Error())
	switch code {
	case "":
		return ""
	case "AADSTS53003":
		return code + ": the tenant's Conditional Access blocks this app; only its admin can allow it."
	case "AADSTS65001", "AADSTS90094", "AADSTS90095":
		return code + ": the tenant needs an admin to approve these permissions for this app."
	default:
		return code + ": the tenant refused the login; it may need admin approval or block this app."
	}
}

// spaces returns n spaces
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	s := ""
	for i := 0; i < n; i++ {
		s += " "
	}
	return s
}

// openBrowser opens a URL in the default browser
func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return false
	}
	return cmd.Start() == nil
}

// copyToClipboard copies text to the system clipboard
func copyToClipboard(text string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case "windows":
		cmd = exec.Command("cmd", "/c", "clip")
	default:
		return false
	}
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	pipe.Write([]byte(text))
	pipe.Close()
	return cmd.Wait() == nil
}
