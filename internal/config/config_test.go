package config

import "testing"

func TestRequireHTTPSOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		wantErr bool
	}{
		{name: "valid", origins: []string{"https://app.example.com"}},
		{name: "http", origins: []string{"http://app.example.com"}, wantErr: true},
		{name: "path", origins: []string{"https://app.example.com/login"}, wantErr: true},
		{name: "query", origins: []string{"https://app.example.com?mode=test"}, wantErr: true},
		{name: "missing host", origins: []string{"https://"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireHTTPSOrigins("TEST_ORIGINS", test.origins)
			if (err != nil) != test.wantErr {
				t.Fatalf("requireHTTPSOrigins() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestApplicationURLFromEnv(t *testing.T) {
	t.Setenv("TEST_APP_URL", "https://app.example.com/")
	got, err := applicationURLFromEnv("TEST_APP_URL", "production")
	if err != nil || got != "https://app.example.com" {
		t.Fatalf("applicationURLFromEnv() = %q, %v", got, err)
	}
	t.Setenv("TEST_APP_URL", "http://app.example.com")
	if _, err := applicationURLFromEnv("TEST_APP_URL", "production"); err == nil {
		t.Fatal("applicationURLFromEnv() expected HTTPS error")
	}
}
