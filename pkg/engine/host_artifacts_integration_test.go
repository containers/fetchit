//go:build host_artifacts_integration

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/podman/v5/pkg/bindings"
	"go.uber.org/zap"
)

// Run on a disposable host: this fixture owns the FetchIt volume and installs a
// uniquely named service in the real root/user systemd manager.
func TestHostArtifactsPodmanLifecycle(t *testing.T) {
	socket := os.Getenv("HOST_ARTIFACT_SOCKET")
	if socket == "" {
		t.Fatal("HOST_ARTIFACT_SOCKET must select the disposable test engine")
	}
	conn, err := bindings.NewConnection(context.Background(), socket)
	if err != nil {
		t.Fatal(err)
	}
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	command := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("podman", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("podman %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := exec.Command("podman", "volume", "exists", fetchitVolume).Run(); err == nil {
		t.Fatal("refusing to replace an existing FetchIt volume")
	}
	command("volume", "create", fetchitVolume)
	t.Cleanup(func() { exec.Command("podman", "volume", "rm", "-f", fetchitVolume).Run() })
	source := t.TempDir()
	name := fmt.Sprintf("fetchit-artifacts-%d", os.Getpid())
	unit := name + ".service"
	root := os.Getenv("HOST_ARTIFACT_MODE") == "rootful"
	wantedBy := "default.target"
	if root {
		wantedBy = "multi-user.target"
	}
	unitText := "[Unit]\nDescription=FetchIt artifact lifecycle fixture\n[Service]\nExecStart=/usr/bin/sleep infinity\n[Install]\nWantedBy=" + wantedBy + "\n"
	files := map[string]string{"owned file.txt": "original", unit: unitText, "legacy.txt": "legacy"}
	for file, contents := range files {
		if err := os.WriteFile(filepath.Join(source, file), []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	uploader := command("create", "--network=none", "--entrypoint", "/bin/sleep", "-v", fetchitVolume+":/opt", fetchitImage, "infinity")
	t.Cleanup(func() { exec.Command("podman", "rm", "-f", uploader).Run() })
	command("cp", source+"/.", uploader+":/opt/")
	command("rm", uploader)
	dest, legacyDest := t.TempDir(), t.TempDir()
	ft := &FileTransfer{CommonMethod: CommonMethod{Name: "files", TargetPath: "files/", CleanupOnRemoval: true}, DestinationDirectory: dest}
	legacy := &FileTransfer{CommonMethod: CommonMethod{Name: "legacy", TargetPath: "files/"}, DestinationDirectory: legacyDest}
	sd := &Systemd{CommonMethod: CommonMethod{Name: name, TargetPath: "services/", CleanupOnRemoval: true}, Root: root, Enable: true}
	getMethodTargetScheds([]*TargetConfig{{Url: "https://example.invalid/artifact-fixture", Branch: "main", FileTransfer: []*FileTransfer{ft, legacy}, Systemd: []*Systemd{sd}}}, newFetchit())
	methods := map[Method]SchedInfo{ft: {}, legacy: {}, sd: {}}
	statePath := filepath.Join(t.TempDir(), "state", "removals.json")
	store, err := loadRemovalStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.configure(methods); err != nil {
		t.Fatal(err)
	}
	if err := ft.MethodEngine(context.Background(), conn, nil, "owned file.txt"); err != nil {
		t.Fatal(err)
	}
	if err := legacy.MethodEngine(context.Background(), conn, nil, "legacy.txt"); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(dest, "unrelated.txt")
	os.WriteFile(unrelated, []byte("keep"), 0644)
	systemctl := func(args ...string) (string, error) {
		if !root {
			args = append([]string{"--user"}, args...)
		}
		out, err := exec.Command("systemctl", args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	receipt, _, err := methodRemovalReceipt(sd)
	if err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(receipt.Host.Destination, unit)
	installed := false
	t.Cleanup(func() {
		if installed {
			systemctl("disable", "--now", unit)
			os.Remove(unitPath)
			systemctl("daemon-reload")
		}
	})
	if err := sd.MethodEngine(context.Background(), conn, nil, unit); err != nil {
		t.Fatal(err)
	}
	installed = true
	if state, err := systemctl("is-active", unit); err != nil || state != "active" {
		t.Fatal("owned unit did not start", state, err)
	}
	// Stop relying on Git/source files before reconstructing removal receipts.
	command("volume", "rm", fetchitVolume)
	ownedPath := filepath.Join(dest, "owned file.txt")
	if err := os.WriteFile(ownedPath, []byte("external edit"), 0644); err != nil {
		t.Fatal(err)
	}
	restored, err := loadRemovalStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.configure(nil); err != nil {
		t.Fatal(err)
	}
	if err := restored.reconcile(conn); err == nil {
		t.Fatal("external edit did not retain a retry receipt")
	}
	if data, err := os.ReadFile(ownedPath); err != nil || string(data) != "external edit" {
		t.Fatal("changed file removed", err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned unit file remains", err)
	}
	if state, _ := systemctl("is-active", unit); state == "active" {
		t.Fatal("removed service still active")
	}
	if state, _ := systemctl("is-enabled", unit); state == "enabled" {
		t.Fatal("removed service still enabled")
	}
	if err := os.WriteFile(ownedPath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	retry, err := loadRemovalStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.configure(nil); err != nil {
		t.Fatal(err)
	}
	if err := retry.reconcile(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned file remains after retry", err)
	}
	for _, path := range []string{unrelated, filepath.Join(legacyDest, "legacy.txt")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("retained file removed", path, err)
		}
	}
	if len(retry.state.Receipts) != 0 {
		t.Fatal("completed host cleanup left receipts")
	}
	if err := retry.reconcile(conn); err != nil {
		t.Fatal("cleanup is not idempotent", err)
	}
}
