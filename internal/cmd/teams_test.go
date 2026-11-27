package cmd

import (
	"testing"

	"github.com/yourname/o365-cli/internal/profile"
)

func TestTeamsCommandsNeedTeamsRead(t *testing.T) {
	p := &profile.Profile{Name: "mail-only", Allow: []string{"mail.read"}}
	for _, c := range teamsCmd.Commands() {
		if err := profile.CheckCommand(p, c); err == nil {
			t.Errorf("%s ran without teams.read", c.Name())
		}
	}
	if err := profile.CheckCommand(&profile.Profile{Allow: []string{"teams.read"}}, teamsChatsCmd); err != nil {
		t.Errorf("teams.read refused: %v", err)
	}
}
