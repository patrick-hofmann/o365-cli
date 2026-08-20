package mail

import "testing"

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
