//go:build sops_integration

package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
)

func lifecycleConnection(t *testing.T) (context.Context, string) {
	t.Helper()
	conn, err := bindings.NewConnection(context.Background(), os.Getenv("SOPS_TEST_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	image := os.Getenv("SOPS_TEST_IMAGE")
	if image == "" {
		t.Fatal("SOPS_TEST_IMAGE required")
	}
	return conn, image
}
func lifecyclePod(name, image, value string) []byte {
	return []byte(fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata: {name: %s}\nspec:\n  hostNetwork: %t\n  containers:\n  - name: app\n    image: %s\n    command: [sleep, infinity]\n    env: [{name: VERSION, value: '%s'}]\n", name, os.Getenv("SOPS_TEST_HOST_NETWORK") == "1", image, value))
}
func lifecycleRaw(t *testing.T, conn context.Context, image, name string, method *Raw) {
	t.Helper()
	spec := createSpecGen(RawPod{Name: name, Image: image})
	spec.Command = []string{"sleep", "infinity"}
	spec.NetNS = specgen.Namespace{NSMode: specgen.Host}
	if method != nil {
		for k, v := range method.workloadLabels() {
			spec.Labels[k] = v
		}
		spec.Labels[kubeMethodLabel] = rawMethod
	}
	created, err := containers.CreateWithSpec(conn, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { containers.Remove(conn, created.ID, new(containers.RemoveOptions).WithForce(true)) })
	if err := containers.Start(conn, created.ID, nil); err != nil {
		t.Fatal(err)
	}
}

// Exercise the real ConfigReload download + Restart path, then a process-start
// reconstruction from the persistent receipts. No Git credentials or old config
// are required to remove an opted-in workload after a process restart.
func TestRemovalPodmanLifecycle(t *testing.T) {
	conn, image := lifecycleConnection(t)
	directory := t.TempDir()
	oldFetchit, oldConfig, oldPath, oldBackup, oldState, oldLogger := fetchit, fetchitConfig, defaultConfigPath, defaultConfigBackup, defaultRemovalsPath, logger
	defaultConfigPath = filepath.Join(directory, "config.yaml")
	defaultConfigBackup = filepath.Join(directory, "backup.yaml")
	defaultRemovalsPath = filepath.Join(directory, "state", "removals.json")
	logger = zap.NewNop().Sugar()
	t.Setenv("FETCHIT_CONFIG_URL", "")
	t.Setenv(statusAddrEnv, "")
	t.Cleanup(func() {
		if fetchit != nil {
			fetchit.retire()
			if fetchit.scheduler != nil {
				fetchit.scheduler.Stop()
			}
		}
		fetchit, fetchitConfig, defaultConfigPath, defaultConfigBackup, defaultRemovalsPath, logger = oldFetchit, oldConfig, oldPath, oldBackup, oldState, oldLogger
	})
	if out, err := exec.Command("podman", "tag", image, fetchitImage).CombinedOutput(); err != nil {
		t.Fatalf("prepare local FetchIt image alias: %s", out)
	}
	name := fmt.Sprintf("fetchit-removal-%d", os.Getpid())
	k := &Kube{CommonMethod: CommonMethod{Name: name + "-kube", TargetPath: "pods", CleanupOnRemoval: true, Schedule: "0 0 1 1 *"}}
	r := &Raw{CommonMethod: CommonMethod{Name: name + "-raw", TargetPath: "raw", CleanupOnRemoval: true, Schedule: "0 0 1 1 *"}}
	f := newFetchit()
	f.conn = conn
	f.scheduler = gocron.NewScheduler(time.UTC)
	getMethodTargetScheds([]*TargetConfig{{Branch: "main", Kube: []*Kube{k}, Raw: []*Raw{r}}}, f)
	// Do not run Git polling in this fixture; create the workloads explicitly.
	f.retired = true
	fetchit = f
	fetchitConfig = &FetchitConfig{conn: conn, scheduler: f.scheduler}
	if err := f.startTargets(); err != nil {
		t.Fatal(err)
	}
	pod := name + "-pod"
	path := filepath.Join(directory, "pod.yaml")
	manifest := lifecyclePod(pod, image, "initial")
	os.WriteFile(path, manifest, 0600)
	t.Cleanup(func() { exec.Command("podman", "pod", "rm", "-f", pod).Run() })
	if err := k.MethodEngine(context.Background(), conn, nil, path); err != nil {
		t.Fatal(err)
	}
	lifecycleRaw(t, conn, image, name+"-raw-container", r)
	unrelated := name + "-unrelated"
	lifecycleRaw(t, conn, image, unrelated, nil)
	initial := []byte("targetConfigs:\n- branch: main\n  kube:\n  - name: " + k.Name + "\n    targetPath: pods\n    cleanupOnRemoval: true\n    schedule: '0 0 1 1 *'\n")
	if err := os.WriteFile(defaultConfigPath, initial, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("targetConfigs: []\n")) }))
	defer server.Close()
	changed, err := downloadUpdateConfigFile(server.URL, true, false, "", "", "")
	if err != nil || !changed {
		t.Fatal("reload config download failed", err)
	}
	fetchitConfig.Restart()
	if ok, err := containers.Exists(conn, name+"-raw-container", nil); err != nil || ok {
		t.Fatal("removed method's Raw workload remains")
	}
	if err := exec.Command("podman", "pod", "exists", pod).Run(); err == nil {
		t.Fatal("removed method's Pod remains")
	}
	if ok, err := containers.Exists(conn, unrelated, nil); err != nil || !ok {
		t.Fatal("unrelated container removed")
	}
	// A newly registered method can leave its receipt across process termination.
	restarted, _ := loadRemovalStore(defaultRemovalsPath)
	if err := restarted.configure(map[Method]SchedInfo{r: {}}); err != nil {
		t.Fatal(err)
	}
	lifecycleRaw(t, conn, image, name+"-restart-container", r)
	fromDisk, err := loadRemovalStore(defaultRemovalsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := fromDisk.configure(nil); err != nil {
		t.Fatal(err)
	}
	if err := fromDisk.reconcile(conn); err != nil {
		t.Fatal(err)
	}
	if ok, err := containers.Exists(conn, name+"-restart-container", nil); err != nil || ok {
		t.Fatal("restart did not clean owned workload")
	}
	if err := fromDisk.reconcile(conn); err != nil {
		t.Fatal("repeated cleanup failed", err)
	}
}

func TestRawDeletionPodmanLifecycle(t *testing.T) {
	conn, image := lifecycleConnection(t)
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	t.Chdir(t.TempDir())
	repo := mirrorSource(t, "repo")
	tree, _ := repo.Worktree()
	os.MkdirAll("repo/raw", 0755)
	name := fmt.Sprintf("fetchit-git-delete-%d", os.Getpid())
	r := &Raw{CommonMethod: CommonMethod{Name: name, TargetPath: "raw", target: &Target{url: "https://example.invalid/repo.git"}}, Networks: []string{"missing-network"}}
	lifecycleRaw(t, conn, image, name, r)
	os.WriteFile("repo/raw/container.yaml", []byte("Name: "+name+"\nImage: unused\n"), 0600)
	tree.Add("raw")
	first, err := tree.Commit("create", &git.CommitOptions{Author: &object.Signature{Name: "CI", Email: "ci@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	tree.Remove("raw/container.yaml")
	second, err := tree.Commit("remove last file", &git.CommitOptions{Author: &object.Signature{Name: "CI", Email: "ci@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"yaml", "yml", "json"}
	if err := r.Apply(context.Background(), conn, first, second, &tags); err != nil {
		t.Fatal(err)
	}
	if ok, err := containers.Exists(conn, name, nil); err != nil || ok {
		t.Fatal("Git deletion left container")
	}
	if err := r.Apply(context.Background(), conn, first, second, &tags); err != nil {
		t.Fatal("deletion retry failed", err)
	}
}

func TestRollbackPodmanLifecycle(t *testing.T) {
	conn, image := lifecycleConnection(t)
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	t.Chdir(t.TempDir())
	repo := mirrorSource(t, "repo")
	tree, _ := repo.Worktree()
	os.MkdirAll("repo/pods", 0755)
	name := fmt.Sprintf("fetchit-rollback-%d", os.Getpid())
	k := &Kube{CommonMethod: CommonMethod{Name: name, TargetPath: "pods", target: &Target{url: "https://example.invalid/repo.git", rollback: true, trackBadCommits: true}}}
	t.Cleanup(func() {
		for _, n := range []string{name, name + "-broken", name + "-initial", name + "-initial-broken"} {
			exec.Command("podman", "pod", "rm", "-f", n).Run()
		}
	})
	commit := func() plumbing.Hash {
		t.Helper()
		tree.Add("pods")
		hash, err := tree.Commit("fixture", &git.CommitOptions{Author: &object.Signature{Name: "CI", Email: "ci@example.invalid", When: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	os.WriteFile("repo/pods/1-working.yaml", lifecyclePod(name, image, "old"), 0600)
	first := commit()
	tags := []string{"yaml", "yml"}
	if err := k.Apply(context.Background(), conn, plumbing.ZeroHash, first, &tags); err != nil {
		t.Fatal(err)
	}
	if err := updateCurrent(context.Background(), k.target, first, k.GetKind(), k.Name); err != nil {
		t.Fatal(err)
	}
	os.WriteFile("repo/pods/1-working.yaml", lifecyclePod(name, image, "new"), 0600)
	os.WriteFile("repo/pods/2-broken.yaml", lifecyclePod(name+"-broken", "invalid@@image", "broken"), 0600)
	second := commit()
	err := applyWithRecovery(context.Background(), conn, k, first, second, &tags)
	var recovery *ApplyRecoveryError
	if !errors.As(err, &recovery) || !recovery.RolledBack {
		t.Fatal("partial apply was not restored", err)
	}
	out, err := exec.Command("podman", "exec", name+"-app", "printenv", "VERSION").Output()
	if err != nil || strings.TrimSpace(string(out)) != "old" {
		t.Fatal("old version was not restored")
	}
	current, err := getCurrent(k.target, k.GetKind(), k.Name)
	if err != nil || current != first {
		t.Fatal("failed apply advanced tag")
	}
	if err := applyWithRecovery(context.Background(), conn, k, first, second, &tags); !errors.Is(err, ErrBadCommitSkipped) {
		t.Fatal("failed head not suppressed")
	}
	initial := &Kube{CommonMethod: CommonMethod{Name: name + "-initial", TargetPath: "pods", target: k.target}}
	// A first-deployment rollback to the empty tree must succeed as well.
	os.WriteFile("repo/pods/1-working.yaml", lifecyclePod(name+"-initial", image, "new"), 0600)
	os.WriteFile("repo/pods/2-broken.yaml", lifecyclePod(name+"-initial-broken", "invalid@@image", "broken"), 0600)
	initialCommit := commit()
	err = applyWithRecovery(context.Background(), conn, initial, plumbing.ZeroHash, initialCommit, &tags)
	if !errors.As(err, &recovery) || !recovery.RolledBack {
		t.Fatal("initial failure was not rolled back", err)
	}
}
