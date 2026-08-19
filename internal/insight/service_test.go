package insight

import "testing"

func TestScopeAllowsItem(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		kind   string
		want   bool
	}{
		{name: "summary allows challenge", scopes: []string{"summaries"}, kind: "challenge", want: true},
		{name: "summary allows event", scopes: []string{"summaries"}, kind: "event", want: true},
		{name: "events alone does not allow report item", scopes: []string{"events"}, kind: "event", want: false},
		{name: "legacy kind is rejected", scopes: []string{"summaries"}, kind: "theme", want: false},
		{name: "unknown is never allowed", scopes: []string{"summaries", "events", "marked_topics"}, kind: "raw_message", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scopeAllowsItem(test.scopes, test.kind); got != test.want {
				t.Fatalf("scopeAllowsItem() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidItemKindRejectsRawMessage(t *testing.T) {
	if validItemKind("raw_message") {
		t.Fatal("raw_message must never be accepted as a generated context item")
	}
}
