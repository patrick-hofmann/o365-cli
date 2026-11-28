package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/config"
	"github.com/yourname/o365-cli/internal/profile"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  "Commands for viewing and changing configuration.",
}

var configShowCmd = &cobra.Command{
	Use:         "show",
	Short:       "Show current configuration",
	Annotations: map[string]string{profile.AnnotationKey: "config.read"},
	RunE:        runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set configuration value",
	Long: `Sets a configuration value.

Available keys:
  client_id       - Azure App Client ID
  current_account - Active account (email address)
  lock_dir        - Directory for cross-process locks (absolute path)
  debug           - Debug output (true or false)

Examples:
  o365-cli config set client_id "your-client-id"
  o365-cli config set current_account "user@example.com"
  o365-cli config set debug true`,
	Annotations: map[string]string{profile.AnnotationKey: "config.write"},
	Args:        cobra.ExactArgs(2),
	RunE:        runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:         "get [key]",
	Short:       "Get configuration value",
	Annotations: map[string]string{profile.AnnotationKey: "config.read"},
	Args:        cobra.ExactArgs(1),
	RunE:        runConfigGet,
}

var configPathCmd = &cobra.Command{
	Use:         "path",
	Short:       "Show config file path",
	Annotations: map[string]string{profile.AnnotationKey: "config.read"},
	Run:         runConfigPath,
}

func init() {
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configPathCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	fmt.Println("\nCurrent Configuration")
	fmt.Println("─────────────────────")
	fmt.Printf("Client ID:       %s\n", maskIfLong(cfg.ClientID, 20))
	fmt.Printf("Cache Dir:       %s\n", cfg.CacheDir)
	fmt.Printf("Debug:           %v\n", cfg.Debug)

	fmt.Printf("\nConfig file: %s\n", config.FilePath())

	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	if err := config.SetValue(key, value); err != nil {
		return err
	}

	printSuccess("%s = %s", key, value)
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	value, err := config.GetValue(key)
	if err != nil {
		return err
	}

	fmt.Println(value)
	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) {
	// Without --config, keep printing the directory: accounts.yaml lives there too.
	if config.File != "" {
		fmt.Println(config.File)
		return
	}
	fmt.Println(config.GetConfigDir())
}

// Helper

func valueOrNone(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func maskIfLong(s string, maxShow int) string {
	if len(s) <= maxShow {
		return s
	}
	return s[:maxShow] + "..."
}
