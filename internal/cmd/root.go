package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/auth"
	"github.com/yourname/o365-cli/internal/config"
	"github.com/yourname/o365-cli/internal/profile"
)

var (
	cfg           *config.Config
	cfgFile       string
	debug         bool
	accountFlag   string
	profileFlag   string
	activeProfile *profile.Profile
)

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "o365-cli",
	Short: "Microsoft 365 CLI for mail and calendar",
	Long: `A cross-platform CLI tool for Microsoft 365 mail and calendar access.

Uses OAuth2 Device Code Flow for authentication -
no admin approval or API keys required.

Examples:
  # Login
  o365-cli auth login

  # List emails
  o365-cli mail list

  # Send email
  o365-cli mail send --to recipient@example.com --subject "Test" --body "Hello!"

  # List calendar events
  o365-cli calendar list

  # Today's events
  o365-cli calendar today`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Migrate config directory from ~/.o365-mail-cli/ to ~/.o365-cli/ if needed
		if err := config.MigrateIfNeeded(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: config migration failed: %v\n", err)
		}

		// Load configuration
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if debug {
			cfg.Debug = true
		}

		// Resolve and check permission profile
		activeProfile, err = profile.ResolveProfile(profileFlag)
		if err != nil {
			return fmt.Errorf("failed to load profile: %w", err)
		}

		if err := profile.CheckCommand(activeProfile, cmd); err != nil {
			return err
		}

		return nil
	},
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "Config file (default: ~/.o365-cli/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output")
	rootCmd.PersistentFlags().StringVar(&accountFlag, "account", "", "Account to use (email address)")
	rootCmd.PersistentFlags().StringVar(&profileFlag, "profile", "", "Permission profile to use")

	// Add subcommands
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(mailCmd)
	rootCmd.AddCommand(foldersCmd)
	rootCmd.AddCommand(rulesCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(calendarCmd)
	rootCmd.AddCommand(versionCmd)
}

// getExplicitAccount returns the explicitly specified account, if any.
// Returns empty string if no account was specified via --account flag or O365_ACCOUNT env.
func getExplicitAccount() string {
	if accountFlag != "" {
		return accountFlag
	}
	if envAccount := os.Getenv("O365_ACCOUNT"); envAccount != "" {
		return envAccount
	}
	return ""
}

// requireAccount resolves which account to use for write operations.
// If --account is set, uses that. If only 1 account is logged in, uses that.
// If multiple accounts and no --account, returns an error.
func requireAccount(ctx context.Context) (string, error) {
	// Explicit --account flag or O365_ACCOUNT env
	if accountFlag != "" {
		return accountFlag, nil
	}
	if envAccount := os.Getenv("O365_ACCOUNT"); envAccount != "" {
		return envAccount, nil
	}

	// Check how many accounts are logged in
	oauthClient, err := auth.NewOAuthClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return "", err
	}

	accounts, err := oauthClient.ListAccounts(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list accounts: %w", err)
	}

	switch len(accounts) {
	case 0:
		return "", fmt.Errorf("no accounts configured. Please run 'auth login'")
	case 1:
		return accounts[0], nil
	default:
		return "", fmt.Errorf("multiple accounts logged in, --account required. Use 'auth list' to see accounts")
	}
}

// getAccessTokenForAccount obtains an access token for a specific account.
func getAccessTokenForAccount(ctx context.Context, email string) (string, error) {
	oauthClient, err := auth.NewOAuthClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return "", err
	}

	accessToken, err := oauthClient.GetAccessToken(ctx, email)
	if err != nil {
		return "", fmt.Errorf("not logged in as %s: %w", email, err)
	}

	return accessToken, nil
}

// getAccessToken obtains an access token using requireAccount (for write operations).
func getAccessToken(ctx context.Context) (string, error) {
	account, err := requireAccount(ctx)
	if err != nil {
		return "", err
	}
	return getAccessTokenForAccount(ctx, account)
}

// isMultiAccount returns true if more than one account is logged in.
func isMultiAccount(ctx context.Context) bool {
	oauthClient, err := auth.NewOAuthClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return false
	}
	accounts, err := oauthClient.ListAccounts(ctx)
	if err != nil {
		return false
	}
	return len(accounts) > 1
}

// getFilteredAccessTokens returns tokens for accounts matching the --account filter.
// If --account is set, returns only that account. Otherwise returns all.
func getFilteredAccessTokens(ctx context.Context) ([]AccountToken, error) {
	if accountFlag != "" {
		token, err := getAccessTokenForAccount(ctx, accountFlag)
		if err != nil {
			return nil, err
		}
		return []AccountToken{{Email: accountFlag, AccessToken: token}}, nil
	}
	return getAllAccessTokens(ctx)
}

// AccountToken holds an access token for a specific account.
type AccountToken struct {
	Email       string
	AccessToken string
	Error       error
}

// getAllAccessTokens retrieves access tokens for all logged-in accounts in parallel.
// Accounts that fail token retrieval are included with their error (non-fatal).
func getAllAccessTokens(ctx context.Context) ([]AccountToken, error) {
	oauthClient, err := auth.NewOAuthClient(cfg.ClientID, cfg.CacheDir)
	if err != nil {
		return nil, err
	}

	accounts, err := oauthClient.ListAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}

	if len(accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured. Please run 'auth login'")
	}

	results := make([]AccountToken, len(accounts))
	var wg sync.WaitGroup

	for i, email := range accounts {
		wg.Add(1)
		go func(idx int, email string) {
			defer wg.Done()
			token, err := oauthClient.GetAccessToken(ctx, email)
			results[idx] = AccountToken{
				Email:       email,
				AccessToken: token,
				Error:       err,
			}
		}(i, email)
	}

	wg.Wait()
	return results, nil
}

// versionCmd shows the version
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("o365-cli v2.0.0")
	},
}

// debugLog prints debug messages when enabled
func debugLog(format string, args ...interface{}) {
	if cfg != nil && cfg.Debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}

// printError prints a formatted error
func printError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}

// printSuccess prints a success message
func printSuccess(format string, args ...interface{}) {
	fmt.Printf("✓ "+format+"\n", args...)
}

// printInfo prints an info message
func printInfo(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
}
