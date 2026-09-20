package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestWorkflowCapabilitiesDoNotClaimConditionalMoveSafety(t *testing.T) {
	command := newWorkflowCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"capabilities"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"conditionalMoveVerified":false`) || !strings.Contains(output.String(), `"mutationAttempts":1`) {
		t.Fatal(output.String())
	}
}
func TestWorkflowRejectsAmbientAccountBeforeAuthentication(t *testing.T) {
	saved := accountFlag
	defer func() { accountFlag = saved }()
	accountFlag = ""
	t.Setenv("O365_ACCOUNT", "ambient@example.invalid")
	command := newWorkflowCommand()
	command.SetArgs([]string{"delta"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "explicit --account") {
		t.Fatalf("ambient account accepted: %v", err)
	}
}
