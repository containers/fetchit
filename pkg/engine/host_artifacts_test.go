package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func artifactFixture(t *testing.T, systemd bool) (hostArtifactPlan, string, string, func(string, string)) {
	t.Helper()
	host, sourceRoot := t.TempDir(), t.TempDir()
	h := &hostRemoval{Destination: "/managed"}
	kind := filetransferMethod
	if systemd {
		h.Root = true
		h.Destination = systemdPathRoot
		kind = systemdMethod
	}
	if err := os.MkdirAll(filepath.Join(host, h.Destination), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sourceRoot, "opt", "repo"), 0755); err != nil {
		t.Fatal(err)
	}
	plan := hostArtifactPlan{Receipt: removalReceipt{Kind: kind, Owner: kubeOwner("fixture", kind, "", ""), Host: h}, Action: "apply"}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(sourceRoot, "opt", "repo", name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return plan, host, sourceRoot, write
}
func noArtifactControl(_ string, _ *hostRemoval, _ ...string) (string, error) { return "", nil }

func TestHostArtifactsOwnershipAndRestart(t *testing.T) {
	plan, host, sourceRoot, write := artifactFixture(t, false)
	write("file with spaces.txt", "first")
	plan.Source = "/opt/repo/file with spaces.txt"
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(host, plan.Receipt.Host.Destination)
	os.WriteFile(filepath.Join(dest, "unrelated.txt"), []byte("keep"), 0644)
	// A newly reconstructed receipt can clean up without the source repository.
	os.RemoveAll(sourceRoot)
	plan.Action, plan.Source = "cleanup", ""
	for i := 0; i < 2; i++ {
		if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "file with spaces.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned file remains", err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "unrelated.txt")); err != nil || string(data) != "keep" {
		t.Fatal("unrelated file changed", err)
	}
}

func TestHostArtifactsChangedFilesRetained(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "edited", true: "replaced with identical bytes"}[replacement], func(t *testing.T) {
			plan, host, sourceRoot, write := artifactFixture(t, false)
			write("site.txt", "original")
			plan.Source = "/opt/repo/site.txt"
			if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(host, plan.Receipt.Host.Destination, "site.txt")
			if replacement {
				temp := filepath.Join(filepath.Dir(path), "replacement")
				os.WriteFile(temp, []byte("original"), 0644)
				os.Rename(temp, path)
			} else {
				os.WriteFile(path, []byte("external edit"), 0644)
			}
			plan.Action = "cleanup"
			if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err == nil {
				t.Fatal("removed externally changed artifact")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("changed file missing", err)
			}
			j, err := loadArtifactJournal(filepath.Join(filepath.Dir(path), artifactJournalDirectory, "ownership.json"))
			if err != nil || len(j.Files) != 1 {
				t.Fatal("lost retry record", err)
			}
		})
	}
}
func TestHostArtifactsRefuseUnmanagedAndConflictingOwners(t *testing.T) {
	plan, host, sourceRoot, write := artifactFixture(t, false)
	write("file.txt", "managed")
	plan.Source = "/opt/repo/file.txt"
	dest := filepath.Join(host, plan.Receipt.Host.Destination)
	path := filepath.Join(dest, "file.txt")
	os.WriteFile(path, []byte("unmanaged"), 0644)
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err == nil {
		t.Fatal("adopted unmanaged destination")
	}
	os.Remove(path)
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
		t.Fatal(err)
	}
	plan.Receipt.Owner = kubeOwner("different", "owner", "", "")
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err == nil {
		t.Fatal("overwrote another owner")
	}
	plan.Action = "cleanup"
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "managed" {
		t.Fatal("another owner's file was removed", err)
	}
}
func TestHostArtifactsRenameAndDelete(t *testing.T) {
	plan, host, sourceRoot, write := artifactFixture(t, false)
	write("old.txt", "old")
	plan.Source = "/opt/repo/old.txt"
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
		t.Fatal(err)
	}
	write("new.txt", "new")
	plan.Source = "/opt/repo/new.txt"
	plan.Previous = "old.txt"
	if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(host, plan.Receipt.Host.Destination)
	if _, err := os.Stat(filepath.Join(dest, "old.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rename left old file")
	}
	plan.Action = "delete"
	plan.Previous = "new.txt"
	for i := 0; i < 2; i++ {
		if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
			t.Fatal(err)
		}
	}
}
func TestHostArtifactsPendingCopyRecovery(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before rename", true: "after rename"}[installed], func(t *testing.T) {
			plan, host, sourceRoot, _ := artifactFixture(t, false)
			dest := filepath.Join(host, plan.Receipt.Host.Destination)
			journalDir := filepath.Join(dest, artifactJournalDirectory)
			os.Mkdir(journalDir, 0700)
			stage := filepath.Join(journalDir, ".stage-pending")
			os.WriteFile(stage, []byte("pending"), 0644)
			pending, err := fingerprintArtifact(stage)
			if err != nil {
				t.Fatal(err)
			}
			j := artifactJournal{Version: 1, Files: map[string]artifactRecord{"file.txt": {Owner: plan.Receipt.Owner, Kind: filetransferMethod, Pending: pending, Stage: filepath.Base(stage)}}}
			if err := saveArtifactJournal(filepath.Join(journalDir, "ownership.json"), j); err != nil {
				t.Fatal(err)
			}
			if installed {
				os.Rename(stage, filepath.Join(dest, "file.txt"))
			}
			plan.Action = "cleanup"
			if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{stage, filepath.Join(dest, "file.txt")} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("interrupted operation left a file", path, err)
				}
			}
		})
	}
}
func TestHostArtifactsRejectSymlinksAndInvalidState(t *testing.T) {
	for _, kind := range []string{"destination", "file", "journal", "source"} {
		t.Run(kind, func(t *testing.T) {
			plan, host, sourceRoot, write := artifactFixture(t, false)
			write("file.txt", "source")
			plan.Source = "/opt/repo/file.txt"
			outside := t.TempDir()
			path := filepath.Join(host, plan.Receipt.Host.Destination)
			switch kind {
			case "destination":
				os.Remove(path)
				os.Symlink(outside, path)
			case "file":
				os.Symlink(filepath.Join(outside, "file"), filepath.Join(path, "file.txt"))
			case "journal":
				os.Symlink(outside, filepath.Join(path, artifactJournalDirectory))
			case "source":
				path = filepath.Join(sourceRoot, "opt", "repo", "file.txt")
				os.Remove(path)
				os.Symlink(filepath.Join(outside, "file"), path)
			}
			if err := executeHostArtifactPlan(plan, host, sourceRoot, noArtifactControl); err == nil {
				t.Fatal("accepted symlink", kind)
			}
		})
	}
	for _, data := range []string{`{"version":2,"files":{}}`, `{"version":1,"files":null}`, `{"version":1,"files":{"../escape":{}}}`, `{"version":1,"files":{}} {}`} {
		path := filepath.Join(t.TempDir(), "journal.json")
		os.WriteFile(path, []byte(data), 0600)
		if _, err := loadArtifactJournal(path); err == nil {
			t.Fatal("accepted corrupt journal")
		}
	}
}
func TestHostArtifactSystemdStopBeforeRemovalAndRetry(t *testing.T) {
	plan, host, sourceRoot, write := artifactFixture(t, true)
	write("app.service", "[Service]\nExecStart=/bin/true\n")
	plan.Source = "/opt/repo/app.service"
	plan.Enable = true
	var commands [][]string
	control := func(_ string, _ *hostRemoval, args ...string) (string, error) {
		commands = append(commands, append([]string(nil), args...))
		if args[0] == "show" {
			if strings.Contains(args[1], "FragmentPath") {
				return filepath.Join(systemdPathRoot, "app.service"), nil
			}
			return "loaded", nil
		}
		return "", nil
	}
	if err := executeHostArtifactPlan(plan, host, sourceRoot, control); err != nil {
		t.Fatal(err)
	}
	commands = nil
	plan.Action = "cleanup"
	failed := true
	retry := func(root string, h *hostRemoval, args ...string) (string, error) {
		if args[0] == "disable" && failed {
			return "", errors.New("stop failed")
		}
		if args[0] == "disable" {
			if _, err := os.Stat(filepath.Join(host, systemdPathRoot, "app.service")); err != nil {
				t.Fatal("file removed before stopping service")
			}
		}
		return control(root, h, args...)
	}
	if err := executeHostArtifactPlan(plan, host, sourceRoot, retry); err == nil {
		t.Fatal("lost stop error")
	}
	if _, err := os.Stat(filepath.Join(host, systemdPathRoot, "app.service")); err != nil {
		t.Fatal("deleted unit after failed stop")
	}
	failed = false
	commands = nil
	if err := executeHostArtifactPlan(plan, host, sourceRoot, retry); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"show", "--property=FragmentPath", "--value", "--", "app.service"}, {"show", "--property=LoadState", "--value", "--", "app.service"}, {"disable", "--now", "--", "app.service"}, {"daemon-reload"}}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands=%v", commands)
	}
}
func TestHostArtifactSystemdForeignUnit(t *testing.T) {
	plan, host, sourceRoot, write := artifactFixture(t, true)
	write("app.service", "content")
	plan.Source = "/opt/repo/app.service"
	control := func(_ string, _ *hostRemoval, _ ...string) (string, error) {
		return "/usr/lib/systemd/system/app.service", nil
	}
	if err := executeHostArtifactPlan(plan, host, sourceRoot, control); err == nil {
		t.Fatal("replaced foreign unit")
	}
	if _, err := os.Stat(filepath.Join(host, systemdPathRoot, "app.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installed over foreign service")
	}
}
func TestHostRemovalReceiptIdentityAndPersistence(t *testing.T) {
	first := &FileTransfer{CommonMethod: CommonMethod{Name: "files", TargetPath: "files/", CleanupOnRemoval: true}, DestinationDirectory: "/managed"}
	second := *first
	second.TargetPath = "files"
	a, _, err := methodRemovalReceipt(first)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := methodRemovalReceipt(&second)
	if err != nil || a.Owner != b.Owner {
		t.Fatal("trailing separator changed owner", err)
	}
	second.DestinationDirectory = "/other"
	b, _, _ = methodRemovalReceipt(&second)
	if a.Owner == b.Owner {
		t.Fatal("destination not part of ownership")
	}
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := loadRemovalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.configure(map[Method]SchedInfo{first: {}}); err != nil {
		t.Fatal(err)
	}
	recovered, err := loadRemovalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.configure(nil); err != nil {
		t.Fatal(err)
	}
	called := false
	recovered.remove = func(_ context.Context, r removalReceipt) error {
		called = true
		if !reflect.DeepEqual(r, a) {
			t.Fatal("lost host identity")
		}
		return nil
	}
	if err := recovered.reconcile(context.Background()); err != nil || !called {
		t.Fatal("did not clean persisted host receipt", err)
	}
	data, _ := os.ReadFile(path)
	var state removalState
	if err := json.Unmarshal(data, &state); err != nil || len(state.Receipts) != 0 {
		t.Fatal("successful removal not persisted", err)
	}
	first.CleanupOnRemoval = false
	first.DestinationDirectory = "relative"
	if _, _, err := methodRemovalReceipt(first); err != nil {
		t.Fatal("changed legacy validation", err)
	}
	first.CleanupOnRemoval = true
	if _, _, err := methodRemovalReceipt(first); err == nil {
		t.Fatal("accepted unsafe cleanup destination")
	}
}

func TestHostRemovalOptOutRetainsArtifacts(t *testing.T) {
	method := &FileTransfer{CommonMethod: CommonMethod{Name: "files", TargetPath: "files", CleanupOnRemoval: true}, DestinationDirectory: "/managed"}
	store, err := loadRemovalStore(filepath.Join(t.TempDir(), "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.configure(map[Method]SchedInfo{method: {}}); err != nil {
		t.Fatal(err)
	}
	method.CleanupOnRemoval = false
	if err := store.configure(map[Method]SchedInfo{method: {}}); err != nil {
		t.Fatal(err)
	}
	store.remove = func(context.Context, removalReceipt) error {
		t.Fatal("opt-out removed a retained artifact")
		return nil
	}
	if err := store.configure(nil); err != nil {
		t.Fatal(err)
	}
	if err := store.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.state.Receipts) != 0 {
		t.Fatal("opt-out retained cleanup intent")
	}
}
