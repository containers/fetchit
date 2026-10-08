package engine

import (
	"context"
	"fmt"
	"github.com/containers/podman/v5/pkg/bindings"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPodmanConnection(t *testing.T, handler http.HandlerFunc) context.Context {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("Libpod-API-Version", "5.0.0")
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	conn, err := bindings.NewConnection(context.Background(), "tcp://"+strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestDisconnectedCopyCompletionAndInspection(t *testing.T) {
	old := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = old })
	for _, tc := range []struct {
		name                                 string
		inspectStatus, copyCode, wantCreates int
		wantError                            bool
	}{
		{"missing helpers copied", 404, 0, 2, false},
		{"copy failed", 404, 7, 2, true},
		{"inspection failed", 500, 0, 0, true},
		{"helper already exists", 200, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creates, removes := 0, 0
			conn := testPodmanConnection(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/json"):
					w.WriteHeader(tc.inspectStatus)
					fmt.Fprintf(w, `{"message":"inspect","response":%d}`, tc.inspectStatus)
				case strings.HasSuffix(r.URL.Path, "/create"):
					creates++
					fmt.Fprintf(w, `{"Id":"helper%d"}`, creates)
				case strings.HasSuffix(r.URL.Path, "/start"):
					w.WriteHeader(204)
				case strings.HasSuffix(r.URL.Path, "/wait"):
					code := 0
					if strings.Contains(r.URL.Path, "helper2/") {
						code = tc.copyCode
					}
					fmt.Fprint(w, code)
				case r.Method == http.MethodDelete:
					removes++
					fmt.Fprint(w, "[]")
				default:
					t.Errorf("unexpected API call %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			})
			// Non-image mode must return a failed copy before attempting a HEAD cache write.
			image := tc.copyCode == 0
			_, err := localDevicePullWithConnection(conn, "test-copy", "/dev/test", "", image)
			if (err != nil) != tc.wantError || creates != tc.wantCreates || removes != creates {
				t.Fatalf("err=%v creates=%d removes=%d", err, creates, removes)
			}
		})
	}
}
