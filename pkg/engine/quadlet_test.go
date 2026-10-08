package engine

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func quadletTestRepo(t *testing.T) (*git.Repository, *Quadlet) {
	t.Helper()
	logger = zap.NewExample().Sugar()
	t.Chdir(t.TempDir())
	r, err := git.PlainInit("repo", false)
	if err != nil {
		t.Fatal(err)
	}
	return r, &Quadlet{CommonMethod: CommonMethod{Name: "test", TargetPath: "bundle", target: &Target{url: "repo", branch: "main"}}, Root: true}
}

func quadletCommit(t *testing.T, r *git.Repository, files map[string]string) plumbing.Hash {
	t.Helper()
	os.RemoveAll("repo/bundle")
	// Retain the selected directory after deleting the final unit.
	if err := os.MkdirAll("repo/bundle", 0755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		dest := filepath.Join("repo/bundle", name)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	hash, err := w.Commit("bundle", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestQuadletNamesAndSupportingFiles(t *testing.T) {
	r, q := quadletTestRepo(t)
	files := map[string]string{
		"app.container": "[Container]\nImage=quay.io/podman/hello\n",
		"data.volume":   "[Volume]\n", "net.network": "[Network]\n", "group.pod": "[Pod]\n",
		"pull.image": "[Image]\nImage=quay.io/podman/hello\n", "compile.build": "[Build]\nImageTag=localhost/test\n",
		"manifest.kube": "[Kube]\nYaml=./files/pod.yaml\n", "asset.artifact": "[Artifact]\nArtifact=example.com/artifact\n",
		"files/pod.yaml": "supporting yaml", "context/Containerfile": "FROM scratch",
		"container.d/10-name.conf":     "[Container]\nServiceName=global\n",
		"app.container.d/10-name.conf": "[Container]\nServiceName=renamed\n",
	}
	hash := quadletCommit(t, r, files)
	b, err := q.bundle(hash)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"asset-artifact.service", "compile-build.service", "data-volume.service", "group-pod.service", "manifest.service", "net-network.service", "pull-image.service", "renamed.service"}
	if !reflect.DeepEqual(b.services, want) {
		t.Fatalf("names: got %v want %v", b.services, want)
	}
	archive, err := quadletArchive(b.files, b.modes)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	seen := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeDir {
			seen[strings.TrimPrefix(hdr.Name, "quadlet-bundle/")] = string(data)
		}
	}
	if !reflect.DeepEqual(seen, files) {
		t.Fatal("archive lost relative paths or supporting contents")
	}
}

func TestQuadletRejectsUnsafeBundles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"collision", map[string]string{"a.container": "[Container]\nServiceName=same\n", "b.container": "[Container]\nServiceName=same\n"}},
		{"template", map[string]string{"a@.container": "[Container]\nImage=example.com/a\n"}},
		{"nested unit", map[string]string{"nested/a.container": "[Container]\nImage=example.com/a\n"}},
		{"invalid filename", map[string]string{".container": "[Container]\nImage=example.com/a\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, q := quadletTestRepo(t)
			hash := quadletCommit(t, r, tc.files)
			if _, err := q.bundle(hash); err == nil {
				t.Fatal("unsafe bundle accepted")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		r, q := quadletTestRepo(t)
		quadletCommit(t, r, map[string]string{"a.container": "[Container]\nImage=example.com/a\n"})
		if err := os.Symlink("a.container", "repo/bundle/escape"); err != nil {
			t.Fatal(err)
		}
		w, _ := r.Worktree()
		w.Add("bundle/escape")
		hash, err := w.Commit("symlink", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.bundle(hash); err == nil {
			t.Fatal("symlink accepted")
		}
	})
}

func TestQuadletApplyKeepsHistoricalServiceNames(t *testing.T) {
	r, q := quadletTestRepo(t)
	old := quadletCommit(t, r, map[string]string{"app.container": "[Container]\nServiceName=old\nImage=example.com/a\n"})
	next := quadletCommit(t, r, map[string]string{"app.container": "[Container]\nServiceName=new\nImage=example.com/a\n"})
	sentinel := errors.New("host failure")
	q.runHost = func(_ context.Context, _ context.Context, p quadletPlan) error {
		if !reflect.DeepEqual(p.previous.services, []string{"old.service"}) || !reflect.DeepEqual(p.desired.services, []string{"new.service"}) {
			t.Fatalf("wrong plan: %+v", p)
		}
		return sentinel
	}
	if err := q.Apply(context.Background(), context.Background(), old, next, nil); !errors.Is(err, sentinel) {
		t.Fatalf("failure was swallowed: %v", err)
	}
	q.Root = false
	if _, _, err := q.paths(); err == nil {
		t.Fatal("rootless host identity guessed")
	}
	q.HostConfigHome = "/home/operator/.config"
	q.HostRuntimeDir = "/run/user/1234"
	if _, _, err := q.paths(); err != nil {
		t.Fatal(err)
	}
}

func TestQuadletConfigAndScheduler(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("targetConfigs:\n- url: example.com/repo\n  quadlet:\n  - name: app\n    schedule: '* * * * *'\n    targetPath: bundle\n    root: true\n    start: true\n")); err != nil {
		t.Fatal(err)
	}
	var config FetchitConfig
	if err := v.Unmarshal(&config); err != nil {
		t.Fatal(err)
	}
	f := getMethodTargetScheds(config.TargetConfigs, newFetchit())
	if len(f.methodTargetScheds) != 1 {
		t.Fatalf("method not registered: %+v", f)
	}
	q := config.TargetConfigs[0].Quadlet[0]
	if !q.Start || !q.Root || !q.initialRun || q.target.url != "example.com/repo" {
		t.Fatalf("wrong config: %+v", q)
	}
}

// Exercise the shipped shell orchestration against fake host commands. Real
// generator/systemd behavior is covered separately by the Linux integration job.
func TestQuadletHostLifecycleAndRetry(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("host orchestration tests require flock (run on Linux CI)")
	}
	for _, failure := range []string{"", "validate", "stop", "daemon-reload", "start", "restart"} {
		t.Run("failure_"+failure, func(t *testing.T) {
			dir := t.TempDir()
			host := filepath.Join(dir, "host")
			bundle := filepath.Join(dir, "bundle")
			bin := filepath.Join(dir, "bin")
			log := filepath.Join(dir, "operations")
			for _, p := range []string{filepath.Join(host, "etc/containers"), filepath.Join(host, "usr/lib/systemd/system-generators"), bundle, bin} {
				if err := os.MkdirAll(p, 0755); err != nil {
					t.Fatal(err)
				}
			}
			os.WriteFile(filepath.Join(host, "usr/lib/systemd/system-generators/podman-system-generator"), []byte(""), 0755)
			os.WriteFile(filepath.Join(bundle, "new.container"), []byte("new"), 0644)
			stub := `#!/bin/sh
set -eu
shift 3
while [ "${1#*=}" != "$1" ]; do export "$1"; shift; done
case "$1" in
 podman) echo 'podman version 5.8.7';;
 stat) echo cgroup2fs;;
 */podman-system-generator) echo validate >> "$LOG"; [ "$FAIL" != validate ]; echo '---new.service---';;
 systemctl)
 shift
 [ "${1:-}" != --user ] || shift
 case "$1" in
 show) case "$*" in *LoadState*) echo loaded;; *SourcePath*) echo /etc/containers/systemd/fetchit-test/unit.container;; esac;;
 *) echo "$*" >> "$LOG"; [ "$1" != "$FAIL" ];;
 esac;;
 *) exit 90;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "chroot"), []byte(stub), 0755); err != nil {
				t.Fatal(err)
			}
			run := func(fail string) (string, error) {
				cmd := exec.Command("sh", "-ceu", quadletHostScript, "quadlet", "/etc/containers", "", "fetchit-test", "old.service", "new.service", "true", map[bool]string{true: "new.service", false: ""}[failure == "restart"])
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FETCHIT_QUADLET_HOST_ROOT="+host, "FETCHIT_QUADLET_BUNDLE="+bundle, "LOG="+log, "FAIL="+fail)
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			out, err := run(failure)
			if failure == "" && err != nil {
				t.Fatalf("host failed: %v\n%s", err, out)
			}
			if failure != "" && err == nil {
				t.Fatal("host swallowed failure")
			}
			operations, _ := os.ReadFile(log)
			if failure == "validate" && strings.Contains(string(operations), "stop") {
				t.Fatal("validation failure stopped live service")
			}
			if failure != "" {
				if out, err := run(""); err != nil {
					t.Fatalf("retry failed: %v\n%s", err, out)
				}
			}
			operations, _ = os.ReadFile(log)
			lines := string(operations)
			if !strings.Contains(lines, "stop old.service\ndaemon-reload\n") {
				t.Fatalf("wrong lifecycle order: %s", lines)
			}
			if !strings.Contains(lines, "start new.service") && !strings.Contains(lines, "restart new.service") {
				t.Fatalf("new unit not activated: %s", lines)
			}
			if _, err := os.Stat(filepath.Join(host, "etc/containers/systemd/fetchit-test/new.container")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQuadletFailureDoesNotAdvanceAppliedCommit(t *testing.T) {
	r, q := quadletTestRepo(t)
	old := quadletCommit(t, r, map[string]string{"a.container": "[Container]\nImage=example.com/old\n"})
	next := quadletCommit(t, r, map[string]string{"a.container": "[Container]\nImage=example.com/new\n"})
	if err := r.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), next)); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs("repo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{abs}}); err != nil {
		t.Fatal(err)
	}
	if err := updateCurrent(context.Background(), q.target, old, q.GetKind(), q.Name); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("failed host operation")
	q.runHost = func(context.Context, context.Context, quadletPlan) error { return sentinel }
	if err := currentToLatest(context.Background(), context.Background(), q, q.target, nil); err == nil {
		t.Fatal("failed reconciliation accepted")
	}
	applied, err := getCurrent(q.target, q.GetKind(), q.Name)
	if err != nil {
		t.Fatal(err)
	}
	if applied != old {
		t.Fatalf("advanced failed commit: %s", applied)
	}
	calls := 0
	q.runHost = func(context.Context, context.Context, quadletPlan) error { calls++; return nil }
	if err := q.Apply(context.Background(), context.Background(), old, old, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("unchanged bundle touched host")
	}
	if err := currentToLatest(context.Background(), context.Background(), q, q.target, nil); err != nil {
		t.Fatal(err)
	}
	applied, err = getCurrent(q.target, q.GetKind(), q.Name)
	if err != nil || applied != next {
		t.Fatalf("successful retry failed to advance: %s %v", applied, err)
	}
}
