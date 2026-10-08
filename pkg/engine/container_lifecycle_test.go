package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/containers/podman/v5/pkg/bindings"
)

func TestHelperContainerCompletion(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code       int
		malformed  bool
		exists     bool
		checkFails bool
		wantError  bool
	}{
		{name: "successful"},
		{name: "command failure", code: 7, wantError: true},
		{name: "verified malformed removal", malformed: true},
		{name: "unverified malformed removal", malformed: true, exists: true, wantError: true},
		{name: "verification failure", malformed: true, checkFails: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			removed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/_ping"):
					w.Header().Set("Libpod-API-Version", "5.0.0")
				case strings.HasSuffix(r.URL.Path, "/wait"):
					fmt.Fprint(w, tc.code)
				case r.Method == http.MethodDelete:
					removed = true
					if !tc.malformed {
						fmt.Fprint(w, "[]")
					}
				case strings.HasSuffix(r.URL.Path, "/json"):
					if tc.checkFails {
						w.WriteHeader(http.StatusInternalServerError)
						fmt.Fprint(w, `{"message":"failed","response":500}`)
					} else if !tc.exists {
						w.WriteHeader(http.StatusNotFound)
						fmt.Fprint(w, `{"message":"missing","response":404}`)
					} else {
						fmt.Fprint(w, `{}`)
					}
				default:
					t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			conn, err := bindings.NewConnection(context.Background(), "tcp://"+strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			err = waitAndRemoveContainer(conn, "helper")
			if (err != nil) != tc.wantError || !removed {
				t.Fatalf("completion err=%v removed=%v", err, removed)
			}
			if tc.code != 0 && !strings.Contains(err.Error(), "status 7") {
				t.Fatalf("lost exit status: %v", err)
			}
		})
	}
}
