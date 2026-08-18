package insight

import "testing"

func TestScopeAllowsItem(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		kind   string
		want   bool
	}{
		{name: "summary allows theme", scopes: []string{"summaries"}, kind: "theme", want: true},
		{name: "summary does not allow event", scopes: []string{"summaries"}, kind: "event", want: false},
		{name: "events allows event", scopes: []string{"summaries", "events"}, kind: "event", want: true},
		{name: "marked topics allows marked topic", scopes: []string{"summaries", "marked_topics"}, kind: "marked_topic", want: true},
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
