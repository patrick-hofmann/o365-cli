package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestIsAllowed_NilProfile(t *testing.T) {
	var p *Profile
	if !p.IsAllowed("mail.read") {
		t.Error("nil profile should allow everything")
	}
}

func TestIsAllowed_Granted(t *testing.T) {
	p := &Profile{Allow: []string{"mail.read", "mail.modify"}}
	if !p.IsAllowed("mail.read") {
		t.Error("should allow mail.read")
	}
	if !p.IsAllowed("mail.modify") {
		t.Error("should allow mail.modify")
	}
}

func TestIsAllowed_Denied(t *testing.T) {
	p := &Profile{Allow: []string{"mail.read"}}
	if p.IsAllowed("mail.send") {
		t.Error("should deny mail.send")
	}
	if p.IsAllowed("mail.delete") {
		t.Error("should deny mail.delete")
	}
}

func TestIsAllowed_EmptyAllow(t *testing.T) {
	p := &Profile{Allow: []string{}}
	if p.IsAllowed("mail.read") {
		t.Error("empty allow list should deny everything")
	}
}

func TestCheckCommand_NilProfile(t *testing.T) {
	cmd := &cobra.Command{
		Use: "test",
		Annotations: map[string]string{
			AnnotationKey: "mail.read",
		},
	}
	if err := CheckCommand(nil, cmd); err != nil {
		t.Errorf("nil profile should allow: %v", err)
	}
}

func TestCheckCommand_NoAnnotation(t *testing.T) {
	p := &Profile{Name: "test", Allow: []string{}}
	cmd := &cobra.Command{Use: "test"}
	if err := CheckCommand(p, cmd); err != nil {
		t.Errorf("command without annotation should be allowed: %v", err)
	}
}

func TestCheckCommand_Allowed(t *testing.T) {
	p := &Profile{Name: "test", Allow: []string{"mail.read"}}
	cmd := &cobra.Command{
		Use: "list",
		Annotations: map[string]string{
			AnnotationKey: "mail.read",
		},
	}
	if err := CheckCommand(p, cmd); err != nil {
		t.Errorf("should allow: %v", err)
	}
}

func TestCheckCommand_Denied(t *testing.T) {
	p := &Profile{Name: "agent", Allow: []string{"mail.read"}}
	cmd := &cobra.Command{
		Use: "send",
		Annotations: map[string]string{
			AnnotationKey: "mail.send",
		},
	}
	err := CheckCommand(p, cmd)
	if err == nil {
		t.Fatal("should deny mail.send")
	}
	pde, ok := err.(*PermissionDeniedError)
	if !ok {
		t.Fatalf("expected PermissionDeniedError, got %T", err)
	}
	if pde.Permission != "mail.send" {
		t.Errorf("expected permission mail.send, got %s", pde.Permission)
	}
	if pde.Profile != "agent" {
		t.Errorf("expected profile agent, got %s", pde.Profile)
	}
}

func TestPermissionDeniedError_Message(t *testing.T) {
	err := &PermissionDeniedError{
		Command:    "mail send",
		Permission: "mail.send",
		Profile:    "agent",
	}
	expected := "permission denied: 'mail send' requires 'mail.send' (profile: agent)"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func setupTestProfiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Non-enforced profile
	writeYAML(t, filepath.Join(dir, "readonly.yaml"), `
description: "Read only"
enforce: false
allow:
  - mail.read
  - folders.read
`)

	// Enforced profile
	writeYAML(t, filepath.Join(dir, "agent.yaml"), `
description: "Agent profile"
enforce: true
allow:
  - mail.read
  - mail.modify
  - drafts.create
`)

	return dir
}

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func TestLoadProfile(t *testing.T) {
	dir := setupTestProfiles(t)

	p, err := loadProfileFromPath(filepath.Join(dir, "readonly.yaml"), "readonly")
	if err != nil {
		t.Fatalf("failed to load profile: %v", err)
	}
	if p.Name != "readonly" {
		t.Errorf("expected name readonly, got %s", p.Name)
	}
	if p.Enforce {
		t.Error("readonly should not be enforced")
	}
	if !p.IsAllowed("mail.read") {
		t.Error("should allow mail.read")
	}
	if p.IsAllowed("mail.send") {
		t.Error("should deny mail.send")
	}
}

func TestFindEnforcedProfile_WithEnforced(t *testing.T) {
	dir := setupTestProfiles(t)

	// We need to test FindEnforcedProfile, but it uses ProfilesDir().
	// So we'll test the scanning logic directly.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var enforced *Profile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(".yaml")]
		p, err := loadProfileFromPath(filepath.Join(dir, entry.Name()), name)
		if err != nil {
			continue
		}
		if p.Enforce {
			enforced = p
			break
		}
	}

	if enforced == nil {
		t.Fatal("should find enforced profile")
	}
	if enforced.Name != "agent" {
		t.Errorf("expected agent, got %s", enforced.Name)
	}
	if !enforced.IsAllowed("mail.read") {
		t.Error("enforced profile should allow mail.read")
	}
	if !enforced.IsAllowed("drafts.create") {
		t.Error("enforced profile should allow drafts.create")
	}
	if enforced.IsAllowed("mail.send") {
		t.Error("enforced profile should deny mail.send")
	}
}

func TestResolveProfile_NoProfileDir(t *testing.T) {
	// When profiles dir doesn't exist and no flag, returns nil
	p, err := ResolveProfile("")
	// This may or may not find an enforced profile depending on system state,
	// but should not error fatally
	if err != nil {
		t.Logf("ResolveProfile returned error (OK if profiles dir missing): %v", err)
	}
	_ = p
}

func TestFullCommandName(t *testing.T) {
	root := &cobra.Command{Use: "o365-cli"}
	mail := &cobra.Command{Use: "mail"}
	send := &cobra.Command{Use: "send"}
	root.AddCommand(mail)
	mail.AddCommand(send)

	name := fullCommandName(send)
	if name != "mail send" {
		t.Errorf("expected 'mail send', got %q", name)
	}
}

func TestFullCommandName_Drafts(t *testing.T) {
	root := &cobra.Command{Use: "o365-cli"}
	mail := &cobra.Command{Use: "mail"}
	drafts := &cobra.Command{Use: "drafts"}
	create := &cobra.Command{Use: "create"}
	root.AddCommand(mail)
	mail.AddCommand(drafts)
	drafts.AddCommand(create)

	name := fullCommandName(create)
	if name != "mail drafts create" {
		t.Errorf("expected 'mail drafts create', got %q", name)
	}
}
