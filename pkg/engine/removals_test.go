package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func removalKube() *Kube {
	return &Kube{CommonMethod: CommonMethod{Name: "app", TargetPath: "pods", CleanupOnRemoval: true, target: &Target{url: "https://token@example.invalid/repo", branch: "main"}}}
}
func TestRemovalPersistenceAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "receipts.json")
	s, err := loadRemovalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	k := removalKube()
	if err := s.configure(map[Method]SchedInfo{k: {}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "token") || strings.Contains(string(data), "pods") {
		t.Fatal("receipt retained repository data")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("receipt must be private")
	}
	// Restart with a removed method: the new process has no old config or Git URL.
	restarted, err := loadRemovalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.configure(nil); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("temporary failure")
	calls := 0
	restarted.remove = func(_ context.Context, r removalReceipt) error {
		calls++
		if !matchesRemoval(k.kubeLabels(), r) {
			t.Fatal("wrong owner")
		}
		if calls == 1 {
			return failure
		}
		return nil
	}
	if err := restarted.reconcile(context.Background()); !errors.Is(err, failure) {
		t.Fatal("lost failure")
	}
	retry, _ := loadRemovalStore(path)
	if len(retry.state.Receipts) != 1 {
		t.Fatal("failed cleanup lost receipt")
	}
	if err := restarted.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	retry, _ = loadRemovalStore(path)
	if len(retry.state.Receipts) != 0 || calls != 2 {
		t.Fatal("successful cleanup not committed")
	}
	if err := restarted.reconcile(context.Background()); err != nil || calls != 2 {
		t.Fatal("cleanup not idempotent")
	}
}
func TestRemovalRetentionAndReintroduction(t *testing.T) {
	for _, optout := range []bool{false, true} {
		s, _ := loadRemovalStore(filepath.Join(t.TempDir(), "receipts.json"))
		k := removalKube()
		if err := s.configure(map[Method]SchedInfo{k: {}}); err != nil {
			t.Fatal(err)
		}
		k.CleanupOnRemoval = !optout
		if err := s.configure(map[Method]SchedInfo{k: {}}); err != nil {
			t.Fatal(err)
		}
		s.remove = func(context.Context, removalReceipt) error { t.Fatal("removed an active method"); return nil }
		if err := s.reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if optout && len(s.state.Receipts) != 0 {
			t.Fatal("opt-out retained cleanup responsibility")
		}
	}
}
func TestRemovalOwnerIsolation(t *testing.T) {
	r, _, _ := methodRemovalReceipt(removalKube())
	for _, labels := range []map[string]string{nil, {kubeManagedByLabel: "other", kubeOwnerLabel: r.Owner}, {kubeManagedByLabel: "fetchit", kubeOwnerLabel: "other"}} {
		if matchesRemoval(labels, r) {
			t.Fatal("accepted another owner")
		}
	}
	raw := r
	raw.Kind = rawMethod
	if matchesRemoval(removalKube().kubeLabels(), raw) {
		t.Fatal("Raw cleanup can select kube containers")
	}
	labels := map[string]string{kubeManagedByLabel: "fetchit", kubeOwnerLabel: r.Owner, kubeMethodLabel: rawMethod}
	if !matchesRemoval(labels, raw) {
		t.Fatal("Raw owner not selected")
	}
}
func TestRemovalInvalidStateAndUnsupportedMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.json")
	for _, data := range []string{`{"version":2,"receipts":{}}`, `{"version":1,"receipts":null}`, `{"version":1,"receipts":{}} {}`, `{"version":1,"receipts":{"invalid":{}}}`} {
		os.WriteFile(path, []byte(data), 0600)
		if _, err := loadRemovalStore(path); err == nil {
			t.Fatal("invalid state accepted")
		}
	}
	s, _ := loadRemovalStore(filepath.Join(t.TempDir(), "state.json"))
	unsupported := &Ansible{CommonMethod: CommonMethod{CleanupOnRemoval: true}}
	if err := s.configure(map[Method]SchedInfo{unsupported: {}}); err == nil {
		t.Fatal("unsupported cleanup silently accepted")
	}
	k := removalKube()
	duplicate := removalKube()
	if err := s.configure(map[Method]SchedInfo{k: {}, duplicate: {}}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
}

// A running job must finish before retirement, and no queued job may run afterward.
func TestRetirementWaitsForDeployment(t *testing.T) {
	f := newFetchit()
	entered, release := make(chan struct{}), make(chan struct{})
	method := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{}}, kind: rawMethod, process: func() { close(entered); <-release }}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); f.runMethod(method, context.Background(), context.Background(), 0) }()
	<-entered
	retired := make(chan struct{})
	go func() { f.retire(); close(retired) }()
	select {
	case <-retired:
		t.Fatal("retired while job was running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	<-retired
	method.process = func() { t.Fatal("retired job ran") }
	f.runMethod(method, context.Background(), context.Background(), 0)
}

type lifecycleFake struct {
	CommonMethod
	kind    string
	process func()
	apply   func(plumbing.Hash, plumbing.Hash) error
}

func (m *lifecycleFake) GetKind() string { return m.kind }
func (m *lifecycleFake) Process(context.Context, context.Context, int) {
	if m.process != nil {
		m.process()
	}
}
func (m *lifecycleFake) Apply(_ context.Context, _ context.Context, from, to plumbing.Hash, _ *[]string) error {
	return m.apply(from, to)
}
func (m *lifecycleFake) MethodEngine(context.Context, context.Context, *object.Change, string) error {
	return nil
}

func TestRawRemovalDoesNotReadDeleteFile(t *testing.T) {
	r := &Raw{}
	if err := r.rawPodman(context.Background(), context.Background(), deleteFile, nil); err != nil {
		t.Fatal(err)
	}
	previous := "Name: missing\nImage: unused\n"
	err := r.rawPodman(context.Background(), context.Background(), deleteFile, &previous)
	if err == nil || strings.Contains(err.Error(), "open delete") {
		t.Fatalf("delete path read file: %v", err)
	}
	if _, err := rawPodFromBytes(nil); err == nil {
		t.Fatal("empty Raw input accepted")
	}
}

func TestLifecycleConfigValidation(t *testing.T) {
	for _, input := range []string{
		"targetConfigs: [null]\n",
		"targetConfigs:\n- raw: [null]\n",
		"targetConfigs:\n- trackBadCommits: true\n",
		"targetConfigs:\n- ansible:\n  - name: task\n    cleanupOnRemoval: true\n",
		"targetConfigs:\n- rollback: true\n  filetransfer:\n  - name: copy\n",
	} {
		if err := decodeConfigExactly([]byte(input)); err == nil {
			t.Fatalf("accepted invalid lifecycle config: %s", input)
		}
	}
	if err := decodeConfigExactly([]byte("targetConfigs:\n- rollback: true\n  trackBadCommits: true\n  raw:\n  - name: app\n    targetPath: raw\n    cleanupOnRemoval: true\n")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigReloadCanRetireItsOwnDeploymentGate(t *testing.T) {
	f := newFetchit()
	method := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{}}, kind: configFileMethod, process: func() { f.retire() }}
	done := make(chan struct{})
	go func() { f.runMethod(method, context.Background(), context.Background(), 0); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reload deadlocked on its own gate")
	}
}
