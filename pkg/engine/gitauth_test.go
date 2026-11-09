package engine

import (
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestHTTPBasicAuth(t *testing.T) {
	tests := []struct {
		name      string
		username  string
		password  string
		anonymous bool
	}{
		{name: "anonymous", anonymous: true},
		{name: "username and password", username: "bob", password: "test-password"},
		{name: "username only", username: "test-token"},
		{name: "password only", password: "test-token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := httpBasicAuth(tt.username, tt.password)
			if tt.anonymous {
				if auth != nil {
					t.Fatalf("anonymous authentication = %T, want nil", auth)
				}
				return
			}
			basic, ok := auth.(*githttp.BasicAuth)
			if !ok {
				t.Fatalf("authentication = %T, want *http.BasicAuth", auth)
			}
			if basic.Username != tt.username || basic.Password != tt.password {
				t.Fatalf("credentials = %q/%q, want %q/%q", basic.Username, basic.Password, tt.username, tt.password)
			}
		})
	}
}

func TestMirrorCloneOptionsHTTPAuth(t *testing.T) {
	tests := []struct {
		name      string
		target    *Target
		username  string
		password  string
		anonymous bool
	}{
		{name: "anonymous", target: &Target{branch: "main"}, anonymous: true},
		{
			name:     "basic auth",
			target:   &Target{branch: "main", username: "bob", password: "test-password"},
			username: "bob",
			password: "test-password",
		},
		{
			name:     "PAT",
			target:   &Target{branch: "main", pat: "test-token"},
			username: "fetchit",
			password: "test-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options, err := mirrorCloneOptions(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if tt.anonymous {
				if options.Auth != nil {
					t.Fatalf("anonymous mirror authentication = %T, want nil", options.Auth)
				}
				return
			}
			basic, ok := options.Auth.(*githttp.BasicAuth)
			if !ok {
				t.Fatalf("mirror authentication = %T, want *http.BasicAuth", options.Auth)
			}
			if basic.Username != tt.username || basic.Password != tt.password {
				t.Fatalf("mirror credentials = %q/%q, want %q/%q", basic.Username, basic.Password, tt.username, tt.password)
			}
		})
	}
}
