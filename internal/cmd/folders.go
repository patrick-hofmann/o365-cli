package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yourname/o365-cli/internal/profile"
)

var foldersCmd = &cobra.Command{
	Use:   "folders",
	Short: "Manage folders",
	Long:  "Commands for listing, creating, and deleting mail folders via Microsoft Graph API.",
}

var foldersListCmd = &cobra.Command{
	Use:         "list",
	Short:       "List all folders",
	Annotations: map[string]string{profile.AnnotationKey: "folders.read"},
	RunE:        runFoldersList,
}

var foldersCreateCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create new folder",
	Long: `Creates a new mail folder.

Examples:
  o365-cli folders create "Archive"
  o365-cli folders create "Projects"`,
	Annotations: map[string]string{profile.AnnotationKey: "folders.manage"},
	Args:        cobra.ExactArgs(1),
	RunE:        runFoldersCreate,
}

var foldersDeleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete folder",
	Long: `Deletes a mail folder.

Warning: All emails in the folder will be deleted!

Examples:
  o365-cli folders delete "Old Folder"`,
	Annotations: map[string]string{profile.AnnotationKey: "folders.manage"},
	Args:        cobra.ExactArgs(1),
	RunE:        runFoldersDelete,
}

func init() {
	foldersCmd.AddCommand(foldersListCmd)
	foldersCmd.AddCommand(foldersCreateCmd)
	foldersCmd.AddCommand(foldersDeleteCmd)
}

func runFoldersList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Fetching folders via Graph API")

	folders, err := client.ListFolders()
	if err != nil {
		return err
	}

	fmt.Println("\nAvailable Folders:")
	fmt.Println("──────────────────")

	for _, folder := range folders {
		// Indentation based on hierarchy (count '/' in name)
		depth := strings.Count(folder.Name, "/")
		indent := strings.Repeat("  ", depth)

		// Show display name (last part)
		displayName := folder.Name
		if idx := strings.LastIndex(folder.Name, "/"); idx != -1 {
			displayName = folder.Name[idx+1:]
		}

		// Icon based on folder name
		icon := "📁"
		lowerName := strings.ToLower(displayName)
		switch {
		case strings.Contains(lowerName, "sent"):
			icon = "📤"
		case strings.Contains(lowerName, "draft") || strings.Contains(lowerName, "entwürf"):
			icon = "📝"
		case strings.Contains(lowerName, "deleted") || strings.Contains(lowerName, "trash") || strings.Contains(lowerName, "gelöscht"):
			icon = "🗑️"
		case strings.Contains(lowerName, "junk") || strings.Contains(lowerName, "spam"):
			icon = "⚠️"
		case strings.Contains(lowerName, "archive") || strings.Contains(lowerName, "archiv"):
			icon = "📦"
		case strings.Contains(lowerName, "inbox") || strings.Contains(lowerName, "posteingang"):
			icon = "📥"
		}

		// Show unread count if any
		unreadInfo := ""
		if folder.UnreadCount > 0 {
			unreadInfo = fmt.Sprintf(" (%d unread)", folder.UnreadCount)
		}

		fmt.Printf("%s%s %s%s\n", indent, icon, displayName, unreadInfo)
	}

	fmt.Printf("\n%d folders found\n", len(folders))

	return nil
}

func runFoldersCreate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	folderName := args[0]

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	debugLog("Creating folder via Graph API")

	if err := client.CreateFolder(folderName, ""); err != nil {
		return err
	}

	printSuccess("Folder '%s' created", folderName)

	return nil
}

func runFoldersDelete(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	folderName := args[0]

	// Confirmation
	fmt.Printf("Delete folder '%s'? All emails will be lost! [y/N]: ", folderName)
	var response string
	fmt.Scanln(&response)

	if response != "y" && response != "Y" {
		printInfo("Cancelled.")
		return nil
	}

	client, err := getGraphClient(ctx)
	if err != nil {
		return err
	}

	// Get folder ID
	folderID, err := client.GetFolderByName(folderName)
	if err != nil {
		return err
	}

	debugLog("Deleting folder via Graph API")

	if err := client.DeleteFolder(folderID); err != nil {
		return err
	}

	printSuccess("Folder '%s' deleted", folderName)

	return nil
}
