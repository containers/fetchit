package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestStatusInitializationAndReload(t *testing.T) {
	r := &statusRegistry{}
	w := httptest.NewRecorder()
	r.writeJSON(w)
	var response struct {
		Status    string         `json:"status"`
		StartedAt *time.Time     `json:"startedAt"`
		Uptime    int64          `json:"uptimeSeconds"`
		Methods   []methodStatus `json:"methods"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != "initializing" || response.StartedAt != nil || response.Uptime != 0 || len(response.Methods) != 0 {
		t.Fatalf("unexpected initial response: %s", w.Body)
	}
	m := &Quadlet{CommonMethod: CommonMethod{Name: "status-test", target: &Target{url: "https://example.com/repo"}}}
	methods := map[Method]SchedInfo{m: {schedule: "* * * * *"}}
	r.replace(methods)
	r.recordRun(m)
	r.replace(map[Method]SchedInfo{m: {schedule: "*/2 * * * *"}})
	w = httptest.NewRecorder()
	r.writeJSON(w)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.StartedAt == nil || response.Status != "running" || len(response.Methods) != 1 || response.Methods[0].Runs != 1 || response.Methods[0].LastRun == nil || response.Methods[0].Schedule != "*/2 * * * *" {
		t.Fatalf("unexpected reload response: %s", w.Body)
	}
	r.replace(nil)
	r.recordRun(m)
	w = httptest.NewRecorder()
	r.writeJSON(w)
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Methods) != 0 {
		t.Fatal("removed methods remain in status")
	}
}

func TestStatusConcurrentSnapshots(t *testing.T) {
	r := &statusRegistry{}
	m := &Quadlet{CommonMethod: CommonMethod{Name: "concurrent", target: &Target{}}}
	methods := map[Method]SchedInfo{m: {schedule: "* * * * *"}}
	r.replace(methods)
	var wg sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		wg.Go(func() {
			for i := 0; i < 100; i++ {
				r.recordRun(m)
				r.replace(methods)
				w := httptest.NewRecorder()
				r.writeJSON(w)
				if !json.Valid(w.Body.Bytes()) {
					t.Error("invalid snapshot")
				}
			}
		})
	}
	wg.Wait()
	if r.methods[statusKey(m)].Runs != 300 {
		t.Fatal("lost run statistics")
	}
}

func TestStatusHTTPRoutes(t *testing.T) {
	r := &statusRegistry{}
	r.replace(nil)
	server := newStatusServer("127.0.0.1:0", r)
	if server.ReadHeaderTimeout == 0 || server.WriteTimeout == 0 || server.IdleTimeout == 0 {
		t.Fatal("missing HTTP timeouts")
	}
	for _, path := range []string{"/healthz", "/status"} {
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	server.Handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/status", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("status accepts writes")
	}
}
