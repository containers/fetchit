//go:build quadlet_integration

package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestQuadletIntegration(t *testing.T) {
	r, q := quadletTestRepo(t)
	q.Root = os.Getuid() == 0
	q.Start = true
	q.Restart = true
	q.HelperImage = os.Getenv("QUADLET_HELPER_IMAGE")
	if q.HelperImage == "" {
		t.Fatal("QUADLET_HELPER_IMAGE must name the image built by this checkout")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	q.HostHome = home
	q.HostConfigHome = filepath.Join(home, ".config")
	q.HostRuntimeDir = os.Getenv("XDG_RUNTIME_DIR")
	socket := "unix:///run/podman/podman.sock"
	if !q.Root {
		socket = "unix://" + q.HostRuntimeDir + "/podman/podman.sock"
	}
	conn, err := bindings.NewConnection(context.Background(), socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := fmt.Sprintf("fetchit-ci-%d", os.Getpid())
	unit := name + ".service"
	systemctl := func(args ...string) (string, error) {
		if !q.Root {
			args = append([]string{"--user"}, args...)
		}
		out, err := exec.Command("systemctl", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	source := func(service, marker string) string {
		return fmt.Sprintf("[Container]\nImage=docker.io/library/alpine:3.22\nServiceName=%s\nContainerName=%s\nNetwork=%s.network\nVolume=%s.volume:/data\nEnvironmentFile=./files/environment\nExec=/bin/sh -c 'echo %s > /data/marker; exec sleep infinity'\n[Service]\nTimeoutStartSec=180\n", service, name, name, name, marker)
	}
	files := map[string]string{
		name + ".container":                    source(name, "first"),
		name + ".network":                      "[Network]\n",
		name + ".volume":                       "[Volume]\n",
		"files/environment":                    "REVISION=one\n",
		name + ".container.d/10-override.conf": "[Container]\nEnvironment=DROPIN=yes\n",
	}
	first := quadletCommit(t, r, files)
	// Always try to remove managed sources/services, even on a failed assertion.
	var applied = first
	t.Cleanup(func() {
		// Keep an unrelated file so Git can represent an empty Quadlet directory.
		empty := quadletCommit(t, r, map[string]string{"README": "empty bundle"})
		if err := q.Apply(ctx, conn, applied, empty, nil); err != nil {
			t.Errorf("cleanup: %v", err)
		}
		exec.Command("podman", "rm", "-f", name).Run()
		exec.Command("podman", "network", "rm", "systemd-"+name).Run()
		exec.Command("podman", "volume", "rm", "-f", "systemd-"+name).Run()
	})
	apply := func(from, to plumbing.Hash) {
		t.Helper()
		if err := q.Apply(ctx, conn, from, to, nil); err != nil {
			t.Fatal(err)
		}
		applied = to
	}
	marker := func(want string) {
		t.Helper()
		out, err := exec.Command("podman", "exec", name, "cat", "/data/marker").CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Fatalf("marker: %q, %v (want %s)", out, err, want)
		}
	}
	apply(plumbing.ZeroHash, first)
	if out, err := systemctl("is-active", unit); err != nil || out != "active" {
		t.Fatalf("unit not active: %s %v", out, err)
	}
	marker("first")
	env, err := exec.Command("podman", "exec", name, "printenv", "DROPIN").CombinedOutput()
	if err != nil || strings.TrimSpace(string(env)) != "yes" {
		t.Fatalf("drop-in not applied: %q %v", env, err)
	}
	// Invalid generator syntax must fail before stopping the current workload.
	files[name+".container"] = "[Container]\nNotARealQuadletOption=true\n"
	bad := quadletCommit(t, r, files)
	if err := q.Apply(ctx, conn, first, bad, nil); err == nil {
		t.Fatal("invalid Quadlet accepted")
	}
	marker("first")
	// A failed systemd start must be reported, and the same commit must be retryable.
	files[name+".container"] = source(name, "second")
	files[name+".container.d/10-override.conf"] = "[Service]\nExecStartPre=/usr/bin/test -f /tmp/" + name + "-allow\n"
	broken := quadletCommit(t, r, files)
	if err := q.Apply(ctx, conn, first, broken, nil); err == nil {
		t.Fatal("failed service start accepted")
	}
	allow := "/tmp/" + name + "-allow"
	if err := os.WriteFile(allow, nil, 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(allow)
	apply(first, broken)
	marker("second")
	// Supporting-file-only changes restart the workload when requested.
	files["files/environment"] = "REVISION=two\n"
	support := quadletCommit(t, r, files)
	apply(broken, support)
	env, err = exec.Command("podman", "exec", name, "printenv", "REVISION").CombinedOutput()
	if err != nil || strings.TrimSpace(string(env)) != "two" {
		t.Fatalf("supporting file not applied: %q %v", env, err)
	}
	// Same source filename, changed service identity: the old service must retire.
	files[name+".container"] = source(name+"-renamed", "third")
	renamed := quadletCommit(t, r, files)
	apply(support, renamed)
	if out, err := systemctl("is-active", unit); err == nil && out == "active" {
		t.Fatal("orphan old service still active")
	}
	marker("third")
	// Then a filename rename with default service naming.
	delete(files, name+".container")
	delete(files, name+".container.d/10-override.conf")
	files[name+"-final.container"] = strings.ReplaceAll(source(name+"-final", "fourth"), "ServiceName="+name+"-final\n", "")
	final := quadletCommit(t, r, files)
	apply(renamed, final)
	marker("fourth")
	delete(files, name+"-final.container")
	deleted := quadletCommit(t, r, files)
	apply(final, deleted)
	if out, err := exec.Command("podman", "container", "exists", name).CombinedOutput(); err == nil {
		t.Fatalf("deleted container still exists: %s", out)
	}
}
