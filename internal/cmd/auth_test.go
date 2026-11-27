package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestConsentHint(t *testing.T) {
	for msg, want := range map[string]string{
		"invalid_grant: AADSTS53003: Access has been blocked by Conditional Access policies": "AADSTS53003: the tenant's Conditional Access",
		"AADSTS65001: The user or administrator has not consented":                           "AADSTS65001: the tenant needs an admin",
		"AADSTS50105: user not assigned":                                                     "AADSTS50105: the tenant refused",
		"context deadline exceeded":                                                          "",
	} {
		got := consentHint(errors.New(msg))
		if want == "" && got != "" || !strings.HasPrefix(got, want) {
			t.Errorf("consentHint(%q) = %q, want prefix %q", msg, got, want)
		}
	}
}

func TestLoginTeamsExcludesCategories(t *testing.T) {
	if loginCmd.Flags().Lookup("teams") == nil {
		t.Fatal("auth login has no --teams flag")
	}
	loginCmd.Flags().Set("teams", "true")
	loginCmd.Flags().Set("categories", "true")
	defer func() { loginTeams, loginCategories = false, false }()
	if err := loginCmd.ValidateFlagGroups(); err == nil {
		t.Fatal("--teams with --categories was accepted")
	}
}
