package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/auth"
	podmail "github.com/yourname/o365-cli/internal/mail"
)

func newPodsCommand() *cobra.Command {
	var account, cacheDir string
	command := &cobra.Command{Use: "pods", Short: "Isolated read-only JSON protocol for OpenApe Pods", SilenceUsage: true, SilenceErrors: false}
	command.PersistentFlags().StringVar(&account, "account", "", "Exact account; no environment fallback")
	command.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "Absolute isolated connection cache directory")
	command.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "capabilities" {
			return nil
		}
		address, err := mail.ParseAddress(account)
		if err != nil || address.Address != account || len(account) > 320 {
			return errors.New("an explicit account email is required")
		}
		if !filepath.IsAbs(cacheDir) || filepath.Clean(cacheDir) != cacheDir {
			return errors.New("an explicit absolute cache directory is required")
		}
		for _, key := range []string{"profile", "config", "debug"} {
			if cmd.Flags().Changed(key) {
				return errors.New("ambient configuration flags are not permitted in pods mode")
			}
		}
		return nil
	}
	encode := func(cmd *cobra.Command, value any) error { return json.NewEncoder(cmd.OutOrStdout()).Encode(value) }
	command.AddCommand(&cobra.Command{Use: "capabilities", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return encode(cmd, map[string]any{"version": 1, "scope": "Mail.Read", "operations": []string{"folders", "messages", "attachments", "attachment"}, "pagination": "bounded-pages", "immutableIds": true, "ambientConfig": false})
	}})
	var request podmail.ReadRequest
	read := &cobra.Command{Use: "read", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := request.Endpoint(); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		client, err := auth.NewReadOnlyOAuthClient("", cacheDir)
		if err != nil {
			return err
		}
		token, err := client.GetAccessToken(ctx, account)
		if err != nil {
			return errors.New("connection needs authentication or token refresh failed")
		}
		page, err := podmail.Read(ctx, token, account, request)
		if err != nil {
			return err
		}
		return encode(cmd, page)
	}}
	read.Flags().StringVar(&request.Operation, "operation", "", "folders, messages, attachments, or attachment")
	read.Flags().StringVar(&request.Folder, "folder", "", "Assigned folder id")
	read.Flags().StringVar(&request.Message, "message", "", "Immutable message id")
	read.Flags().StringVar(&request.Attachment, "attachment", "", "Attachment id")
	read.Flags().StringVar(&request.Cursor, "cursor", "", "Provider continuation from the preceding page")
	command.AddCommand(read)
	command.AddCommand(&cobra.Command{Use: "login", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
		defer cancel()
		client, err := auth.NewReadOnlyOAuthClient("", cacheDir)
		if err != nil {
			return err
		}
		accounts, err := client.ListAccounts(ctx)
		if err != nil {
			return errors.New("cannot inspect connection cache")
		}
		for _, existing := range accounts {
			if existing != account {
				return errors.New("connection cache belongs to another account")
			}
		}
		code, results, err := client.StartDeviceCodeFlow(ctx)
		if err != nil {
			return errors.New("cannot start Microsoft sign-in")
		}
		target, err := url.Parse(code.VerificationURL)
		if err != nil || target.Scheme != "https" || (target.Host != "microsoft.com" && target.Host != "www.microsoft.com" && target.Host != "login.microsoftonline.com") || target.User != nil {
			return errors.New("unexpected Microsoft verification URL")
		}
		if err := encode(cmd, map[string]any{"event": "deviceCode", "code": code.UserCode, "url": code.VerificationURL, "expiresIn": code.ExpiresIn}); err != nil {
			return err
		}
		result := <-results
		if result.Error != nil {
			return errors.New("Microsoft sign-in failed or was cancelled")
		}
		if result.Email != account {
			if err := client.Logout(ctx, result.Email); err != nil {
				return errors.New("wrong account signed in; discard this isolated connection cache")
			}
			return errors.New("wrong account signed in; expected " + account)
		}
		return encode(cmd, map[string]any{"event": "connected", "account": result.Email, "scope": "Mail.Read", "expiresAt": result.ExpiresAt})
	}})
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return errors.New("invalid pods arguments")
	})
	return command
}
