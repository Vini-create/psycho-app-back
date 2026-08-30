package httpapi

import "testing"

func TestPreferredLocale(t *testing.T) {
	tests := map[string]string{
		"pt-BR,pt;q=0.9,en-US;q=0.8": "pt-BR",
		"en-US,en;q=0.9":             "en-US",
		"es; q=0.8":                  "es",
		"*":                          "",
		"":                           "",
	}
	for header, expected := range tests {
		if actual := preferredLocale(header); actual != expected {
			t.Errorf("preferredLocale(%q) = %q, want %q", header, actual, expected)
		}
	}
}
