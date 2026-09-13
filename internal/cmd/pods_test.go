package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestPodsMachineContractIgnoresAmbientConfiguration(t *testing.T) {
	t.Setenv("O365_ACCOUNT", "ambient@example.invalid")
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{{"read", "--operation", "messages", "--folder", "inbox"}, {"read", "--account", "pod@example.invalid", "--cache-dir", "relative", "--operation", "messages"}, {"read", "--account", "pod@example.invalid", "--cache-dir", t.TempDir(), "--operation", "send"}} {
		command := newPodsCommand()
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	command := newPodsCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"capabilities"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"scope":"Mail.Read"`) || !strings.Contains(output.String(), `"ambientConfig":false`) {
		t.Fatal(output.String())
	}
}
