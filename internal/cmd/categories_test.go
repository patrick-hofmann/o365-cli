package cmd

import (
	"testing"

	"github.com/yourname/o365-cli/internal/profile"
)

func TestCategoryCommandsEnforceExistingProfilePermissions(t *testing.T) {
	readOnly := &profile.Profile{Name: "read-only", Allow: []string{"mail.read"}}
	root := newMailCategoriesCommand()
	for _, name := range []string{"get", "add", "remove", "set", "clear"} {
		command, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		err = profile.CheckCommand(readOnly, command)
		if (name == "get") != (err == nil) {
			t.Fatalf("unexpected permission for %s: %v", name, err)
		}
	}
}

func TestInvalidCategoryArgumentsFailBeforeAuthentication(t *testing.T) {
	for _, args := range [][]string{{"get"}, {"add", "m1"}, {"remove", "m1", " "}, {"set", "m1"}, {"clear", "m1", "extra"}} {
		command := newMailCategoriesCommand()
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
