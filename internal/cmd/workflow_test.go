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
func TestWorkflowSubcommandsKeepTheirOwnOperation(t *testing.T) {
	saved := accountFlag
	defer func() { accountFlag = saved }()
	accountFlag = "owner@example.invalid"
	// An invalid folder is refused by request validation, before authentication; delta must not demand --categories.
	command := newWorkflowCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"delta", "--folder", "drafts"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "--categories") || !strings.Contains(output.String(), `"operation":"delta"`) || !strings.Contains(output.String(), "unexpected delta arguments") {
		t.Fatal(output.String())
	}
}

func TestWorkflowRefusesRawMutationBeforeAuthentication(t *testing.T) {
	saved := accountFlag
	defer func() { accountFlag = saved }()
	accountFlag = "owner@example.invalid"
	for _, operation := range []string{"categorize", "move"} {
		command := newWorkflowCommand()
		command.SetArgs([]string{operation})
		if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "raw workflow mutations are disabled") {
			t.Fatalf("unlocked mutation accepted: %s %v", operation, err)
		}
	}
}

func TestLabelRequestRejectsCrossAccountAndUnknownPolicyBeforeAuthentication(t *testing.T) {
	saved := accountFlag
	defer func() { accountFlag = saved }()
	accountFlag = "owner@example.invalid"
	for _, request := range []string{`{"account":"other@example.invalid"}`, `{"unexpected":true}`, `{} {}`, `{} broken`} {
		command := newWorkflowCommand()
		command.SetArgs([]string{"decide-label", "--request", request})
		if err := command.Execute(); err == nil {
			t.Fatalf("invalid request accepted: %s", request)
		}
	}
}
