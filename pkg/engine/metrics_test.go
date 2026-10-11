package engine

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func metricOutput(r *metricsRegistry) string {
	response := httptest.NewRecorder()
	r.writeMetrics(response)
	return response.Body.String()
}
func requireMetric(t *testing.T, r *metricsRegistry, expected string) {
	t.Helper()
	if output := metricOutput(r); !strings.Contains(output, expected) {
		t.Fatalf("missing %q in:\n%s", expected, output)
	}
}

func TestMetricsReconciliationAndFreshness(t *testing.T) {
	target, source, sourcePath := mirrorTestTarget(t)
	mirrorCommit(t, source, "initial")
	m := &lifecycleFake{CommonMethod: CommonMethod{Name: "secret-name", target: target, initialRun: true}, kind: rawMethod}
	fail := true
	m.apply = func(_, _ plumbing.Hash) error {
		if fail {
			return errors.New("secret apply failure")
		}
		return nil
	}
	r := &metricsRegistry{}
	r.replace(map[Method]SchedInfo{m: {}}, "edge-01.example.invalid")
	label := `hostname="edge-01.example.invalid",kind="raw",state=`
	requireMetric(t, r, `fetchit_git_methods{`+label+`"unknown"} 1`)
	run := func() { ctx := r.begin(context.Background(), m); m.processGit(ctx, ctx, m, 0, nil) }
	run()
	requireMetric(t, r, `fetchit_git_methods{`+label+`"out_of_sync"} 1`)
	firstObservation := r.methods[m].observed
	if firstObservation.IsZero() {
		t.Fatal("successful fetch not observed")
	}
	fail = false
	run()
	requireMetric(t, r, `fetchit_git_methods{`+label+`"synced"} 1`)
	if !r.methods[m].observed.After(firstObservation) {
		t.Fatal("freshness did not advance")
	}
	// A previously synced cache cannot be mistaken for a successful new fetch.
	observed := r.methods[m].observed
	if err := os.Rename(sourcePath, sourcePath+"-offline"); err != nil {
		t.Fatal(err)
	}
	run()
	requireMetric(t, r, `fetchit_git_methods{`+label+`"unknown"} 1`)
	if !r.methods[m].observed.Equal(observed) {
		t.Fatal("failed fetch refreshed freshness")
	}
	if err := os.Rename(sourcePath+"-offline", sourcePath); err != nil {
		t.Fatal(err)
	}
	run()
	requireMetric(t, r, `fetchit_git_methods{`+label+`"synced"} 1`)
	// Startup replay is required before equality of saved and desired revisions
	// can establish synchronization after reload.
	m.initialRun = true
	fail = true
	run()
	requireMetric(t, r, `fetchit_git_methods{`+label+`"unknown"} 1`)
}

func TestMetricsRetirementCancellationAndMetadata(t *testing.T) {
	target := &Target{url: "https://user:secret-password@example.invalid/private.git", branch: "main"}
	m := &lifecycleFake{CommonMethod: CommonMethod{Name: "secret-name", target: target}, kind: rawMethod}
	nonGit := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{}}, kind: imageMethod}
	disconnected := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{url: "secret-archive", disconnected: true}}, kind: kubeMethod}
	r := &metricsRegistry{}
	methods := map[Method]SchedInfo{m: {}, nonGit: {}, disconnected: {}}
	r.replace(methods, "edge-01")
	old := r.begin(context.Background(), m)
	observeGit(old, "synced", true)
	r.replace(methods, "edge-01")
	observeGit(old, "synced", true)
	requireMetric(t, r, `fetchit_git_methods{hostname="edge-01",kind="raw",state="unknown"} 1`)
	older := r.begin(context.Background(), m)
	newer := r.begin(context.Background(), m)
	observeGit(older, "synced", true)
	if !r.methods[m].observed.IsZero() {
		t.Fatal("superseded run published success")
	}
	observeGit(newer, "out_of_sync", false)
	cancelled, cancel := context.WithCancel(r.begin(context.Background(), m))
	cancel()
	observeGit(cancelled, "synced", true)
	if !r.methods[m].observed.IsZero() {
		t.Fatal("retired or cancelled run changed replacement")
	}
	output := metricOutput(r)
	for _, secret := range []string{"secret", "private.git", "https:", "branch", "state=\"not_applicable\""} {
		if strings.Contains(output, secret) {
			t.Fatalf("metadata leaked: %s", secret)
		}
	}
	requireMetric(t, r, `fetchit_configured_methods{hostname="edge-01",kind="image"} 1`)
	if strings.Contains(output, `fetchit_git_methods{hostname="edge-01",kind="image"`) || strings.Contains(output, `fetchit_git_methods{hostname="edge-01",kind="kube"`) {
		t.Fatal("non-connected Git counted as synchronized")
	}
	r.replace(nil, "edge-01")
	if strings.Contains(metricOutput(r), `kind="raw"`) {
		t.Fatal("removed method remains")
	}
}

func TestMetricsRoutesAndHostname(t *testing.T) {
	r := &metricsRegistry{}
	r.replace(nil, "edge-01")
	server := newMetricsServer("127.0.0.1:8080", r)
	for _, test := range []struct {
		method, path string
		code         int
	}{{"GET", "/metrics", 200}, {"HEAD", "/metrics", 200}, {"POST", "/metrics", 405}, {"GET", "/status", 404}, {"GET", "/healthz", 404}, {"GET", "/debug/pprof/", 404}} {
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.code {
			t.Fatalf("%s %s: %d", test.method, test.path, response.Code)
		}
		if test.code == 200 && response.Header().Get("X-Fetchit-Hostname") != "edge-01" {
			t.Fatal("host identity missing")
		}
	}
	if server.ReadTimeout == 0 || server.WriteTimeout == 0 || server.ReadHeaderTimeout == 0 || server.MaxHeaderBytes > 16384 {
		t.Fatal("unbounded listener")
	}
	t.Setenv(hostnameEnv, "edge-01.example.invalid")
	if host, err := configuredHostname(); err != nil || host != "edge-01.example.invalid" {
		t.Fatal(host, err)
	}
	for _, host := range []string{"bad\nmetric 1", "bad\"label", strings.Repeat("a", 254), "-invalid", "é"} {
		t.Setenv(hostnameEnv, host)
		if _, err := configuredHostname(); err == nil {
			t.Fatalf("invalid hostname accepted: %q", host)
		}
	}
}

func TestMetricsConcurrentScrapesAndReloads(t *testing.T) {
	m := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{url: "secret"}}, kind: rawMethod}
	r := &metricsRegistry{}
	r.replace(map[Method]SchedInfo{m: {}}, "edge-01")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ctx := r.begin(context.Background(), m)
				observeGit(ctx, "out_of_sync", true)
				observeGit(ctx, "synced", false)
				_ = metricOutput(r)
				r.replace(map[Method]SchedInfo{m: {}}, "edge-01")
			}
		}()
	}
	wg.Wait()
	// Aggregate freshness is governed by the oldest method, including never fetched.
	second := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{url: "secret2"}}, kind: rawMethod}
	r.replace(map[Method]SchedInfo{m: {}, second: {}}, "edge-01")
	observeGit(r.begin(context.Background(), m), "synced", true)
	requireMetric(t, r, `fetchit_git_oldest_observation_timestamp_seconds{hostname="edge-01",kind="raw"} 0.000`)
	observeGit(r.begin(context.Background(), second), "synced", true)
	if strings.Contains(metricOutput(r), `kind="raw"} 0.000`) {
		t.Fatal("freshness not recorded")
	}
}

func TestMetricsRollbackAndSkippedBadCommitStayOutOfSync(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	initial := mirrorCommit(t, source, "initial")
	target.rollback = true
	target.trackBadCommits = true
	m := &lifecycleFake{CommonMethod: CommonMethod{Name: "rollback", target: target, initialRun: true}, kind: rawMethod}
	m.apply = func(_, _ plumbing.Hash) error { return nil }
	r := &metricsRegistry{}
	r.replace(map[Method]SchedInfo{m: {}}, "edge-01")
	run := func() { ctx := r.begin(context.Background(), m); m.processGit(ctx, ctx, m, 0, nil) }
	run()
	desired := mirrorCommit(t, source, "bad")
	calls := 0
	m.apply = func(from, to plumbing.Hash) error {
		calls++
		if to == desired {
			return errors.New("bad apply")
		}
		if from != desired || to != initial {
			t.Fatalf("unexpected rollback %s -> %s", from, to)
		}
		return nil
	}
	run()
	requireMetric(t, r, `fetchit_git_methods{hostname="edge-01",kind="raw",state="out_of_sync"} 1`)
	current, err := getCurrent(target, rawMethod, m.Name)
	if err != nil || current != initial || calls != 2 {
		t.Fatalf("rollback baseline: %s %v calls=%d", current, err, calls)
	}
	run()
	requireMetric(t, r, `fetchit_git_methods{hostname="edge-01",kind="raw",state="out_of_sync"} 1`)
	if calls != 2 {
		t.Fatal("known bad commit was retried")
	}
}

func TestMetricsQuadletForcedReplayMustSucceed(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	worktree, err := source.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	directory := worktree.Filesystem.Root()
	if err := os.MkdirAll(directory+"/bundle", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory+"/bundle/app.container", []byte("[Container]\nImage=quay.io/podman/hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("bundle/app.container"); err != nil {
		t.Fatal(err)
	}
	mirrorCommit(t, source, "bundle")
	q := &Quadlet{CommonMethod: CommonMethod{Name: "quadlet", TargetPath: "bundle", target: target, initialRun: true}, Root: true}
	fail := false
	q.runHost = func(context.Context, context.Context, quadletPlan) error {
		if fail {
			return errors.New("replay failed")
		}
		return nil
	}
	r := &metricsRegistry{}
	r.replace(map[Method]SchedInfo{q: {}}, "edge-01")
	run := func() { ctx := r.begin(context.Background(), q); q.Process(ctx, ctx, 0) }
	run()
	requireMetric(t, r, `fetchit_git_methods{hostname="edge-01",kind="quadlet",state="synced"} 1`)
	q.initialRun = true
	fail = true
	run()
	requireMetric(t, r, `fetchit_git_methods{hostname="edge-01",kind="quadlet",state="out_of_sync"} 1`)
}

func TestStatusHostnameIdentity(t *testing.T) {
	r := &statusRegistry{methods: map[string]*methodStatus{}}
	r.setHostname("edge-01")
	server := newStatusServer("", r)
	for _, path := range []string{"/status", "/healthz"} {
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Header().Get("X-Fetchit-Hostname") != "edge-01" {
			t.Fatal("hostname header missing")
		}
		if path == "/status" && !strings.Contains(response.Body.String(), `"hostname": "edge-01"`) {
			t.Fatal("JSON hostname missing")
		}
	}
}

func TestMetricsDefaultsAndDisabledListener(t *testing.T) {
	t.Setenv(hostnameEnv, "")
	expected, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := configuredHostname()
	if err != nil || actual != expected {
		t.Fatalf("native hostname: %q %v", actual, err)
	}
	t.Setenv(metricsAddrEnv, "")
	if err := startMetricsServer(); err != nil {
		t.Fatal(err)
	}
}
