package mail

import (
	"io"
	"net/http"
	"testing"
)

func TestNextSequence(t *testing.T) {
	tests := []struct {
		name  string
		rules []MessageRule
		want  int
	}{
		{"no rules", nil, 1},
		{"appends after highest", []MessageRule{{Sequence: 1}, {Sequence: 5}, {Sequence: 3}}, 6},
	}

	for _, tt := range tests {
		if got := nextSequence(tt.rules); got != tt.want {
			t.Errorf("%s: nextSequence() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// A partial patch must not blank the name or disable the rule, so unset fields stay off the wire.
func TestUpdateRuleSendsOnlyThePatchedFields(t *testing.T) {
	client := NewClient("synthetic-token")
	var sent string
	client.HttpClient = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		sent = string(body)
		return fixtureResponse(`{"id":"rule-1","displayName":"Kept","isEnabled":true}`), nil
	})}

	patch := &MessageRule{Actions: &MessageRuleActions{AssignCategories: []string{"X"}, StopProcessingRules: BoolPtr(true)}}
	updated, err := client.UpdateRule("rule-1", patch)
	if err != nil {
		t.Fatal(err)
	}
	if sent != `{"actions":{"assignCategories":["X"],"stopProcessingRules":true}}` {
		t.Fatalf("patch body: %s", sent)
	}
	if !updated.Enabled() || updated.DisplayName != "Kept" {
		t.Fatalf("reply: %+v", updated)
	}

	if _, err := client.DisableRule("rule-1"); err != nil {
		t.Fatal(err)
	}
	if sent != `{"isEnabled":false}` {
		t.Fatalf("disable body: %s", sent)
	}
}
