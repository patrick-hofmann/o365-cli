package cmd

import (
	"testing"

	"github.com/patrick-hofmann/o365-cli/v2/internal/profile"
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

func TestTeamsSendNeedsTeamsSend(t *testing.T) {
	if err := profile.CheckCommand(&profile.Profile{Name: "reader", Allow: []string{"teams.read"}}, teamsSendCmd); err == nil {
		t.Error("teams send ran without teams.send")
	}
	if err := profile.CheckCommand(&profile.Profile{Allow: []string{"teams.send"}}, teamsSendCmd); err != nil {
		t.Errorf("teams.send refused: %v", err)
	}
}
