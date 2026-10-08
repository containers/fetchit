//go:build sops_integration

package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestSOPSPodmanLifecycle(t *testing.T) {
	ctx := context.Background()
	socket := os.Getenv("SOPS_TEST_SOCKET")
	if socket == "" {
		t.Fatal("SOPS_TEST_SOCKET must point to a Podman 5 service")
	}
	conn, err := bindings.NewConnection(ctx, socket)
	if err != nil {
		t.Fatal("cannot connect to Podman")
	}
	version, err := exec.Command("podman", "--version").Output()
	if err != nil || !strings.Contains(string(version), "version 5.") {
		t.Fatal("integration requires Podman 5")
	}
	image := os.Getenv("SOPS_TEST_IMAGE")
	if image == "" {
		t.Fatal("SOPS_TEST_IMAGE must identify the pinned test image")
	}
	directory := t.TempDir()
	t.Chdir(directory)
	key := filepath.Join(directory, "age.txt")
	wrong := filepath.Join(directory, "wrong.txt")
	for _, path := range []string{key, wrong} {
		if err := exec.Command("age-keygen", "-o", path).Run(); err != nil {
			t.Fatal("age key generation failed")
		}
	}
	recipient, err := exec.Command("age-keygen", "-y", key).Output()
	if err != nil {
		t.Fatal("age public key failed")
	}
	core, logs := observer.New(zap.InfoLevel)
	oldLogger := logger
	logger = zap.New(core).Sugar()
	t.Cleanup(func() { logger = oldLogger })
	repo := mirrorSource(t, "repo")
	tree, _ := repo.Worktree()
	if err := os.MkdirAll("repo/kube", 0755); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("fetchit-sops-%d", os.Getpid())
	pod := name + "-pod"
	container := name + "-app"
	secret := name + "-secret"
	k := &Kube{CommonMethod: CommonMethod{Name: name, TargetPath: "kube", target: &Target{url: "https://example.invalid/repo.git", branch: "main"}}, SOPS: &SOPS{AgeKeyFile: key}}
	encrypt := func(value string) []byte {
		t.Helper()
		plain := fmt.Sprintf("apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\nstringData:\n  password: %s\n---\napiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\nspec:\n  hostNetwork: %t\n  containers:\n  - name: %s\n    image: %s\n    command: [sleep, infinity]\n    env:\n    - name: PASSWORD\n      valueFrom:\n        secretKeyRef:\n          name: %s\n          key: password\n", secret, value, pod, os.Getenv("SOPS_TEST_HOST_NETWORK") == "1", container, image, secret)
		command := exec.Command(sopsExecutable, "encrypt", "--filename-override", "manifest.yaml", "--age", strings.TrimSpace(string(recipient)), "--encrypted-regex", "^(data|stringData)$", "--input-type", "yaml", "--output-type", "yaml")
		command.Stdin = strings.NewReader(plain)
		cipher, err := command.Output()
		if err != nil {
			t.Fatal("SOPS encryption failed")
		}
		return cipher
	}
	commit := func(path string, cipher []byte) plumbing.Hash {
		t.Helper()
		if err := os.WriteFile(filepath.Join("repo/kube", path), cipher, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := tree.Add("kube"); err != nil {
			t.Fatal(err)
		}
		hash, err := tree.Commit("encrypted fixture", &git.CommitOptions{Author: &object.Signature{Name: "CI", Email: "ci@example.invalid", When: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	tags := []string{"yaml", "yml"}
	var applied plumbing.Hash
	apply := func(hash plumbing.Hash) {
		t.Helper()
		if err := k.Apply(ctx, conn, applied, hash, &tags); err != nil {
			var failure *SOPSPodmanError
			if errors.As(err, &failure) {
				diagnostic := regexp.MustCompile(`fetchit-secret-sentinel-[a-z-]+`).ReplaceAllString(failure.cause.Error(), "[redacted test secret]")
				t.Logf("Podman lifecycle fixture failure: %s", diagnostic)
			}
			t.Fatal(err)
		}
		if err := updateCurrent(ctx, k.target, hash, k.GetKind(), k.GetName()); err != nil {
			t.Fatal(err)
		}
		applied = hash
	}
	readSecret := func(want string) {
		t.Helper()
		out, err := exec.Command("podman", "exec", pod+"-"+container, "printenv", "PASSWORD").Output()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Fatal("deployed secret did not match expected value")
		}
	}
	t.Cleanup(func() {
		exec.Command("podman", "pod", "rm", "-f", pod).Run()
		exec.Command("podman", "secret", "rm", secret).Run()
	})
	first := commit("application.enc.yaml", encrypt("fetchit-secret-sentinel-first"))
	apply(first)
	assertKubeRuntimeLabels(t, k)
	readSecret("fetchit-secret-sentinel-first")
	secondCipher := encrypt("fetchit-secret-sentinel-second")
	second := commit("application.enc.yaml", secondCipher)
	// Wrong key and missing key must preserve the running pod and applied tag.
	for _, badKey := range []string{wrong, filepath.Join(directory, "missing.txt")} {
		k.SOPS.AgeKeyFile = badKey
		if err := k.Apply(ctx, conn, applied, second, &tags); err == nil {
			t.Fatal("invalid key applied")
		}
		readSecret("fetchit-secret-sentinel-first")
		current, err := getCurrent(k.target, k.GetKind(), k.GetName())
		if err != nil || current != applied {
			t.Fatal("failed apply advanced the tag")
		}
	}
	k.SOPS.AgeKeyFile = key
	// Authenticated metadata tampering must also preserve the running workload.
	tampered := bytes.Replace(secondCipher, []byte(secret), []byte(secret+"-tampered"), 1)
	bad := commit("application.enc.yaml", tampered)
	if err := k.Apply(ctx, conn, applied, bad, &tags); err == nil {
		t.Fatal("tampering applied")
	}
	readSecret("fetchit-secret-sentinel-first")
	recovered := commit("application.enc.yaml", secondCipher)
	apply(recovered)
	readSecret("fetchit-secret-sentinel-second")
	// Rotate the actual age identity with an overlap window for old Git content.
	oldIdentity, err := os.ReadFile(key)
	if err != nil {
		t.Fatal("cannot read old test identity")
	}
	newIdentity, err := os.ReadFile(wrong)
	if err != nil {
		t.Fatal("cannot read new test identity")
	}
	overlap := append(bytes.Clone(oldIdentity), newIdentity...)
	if err := os.WriteFile(key, overlap, 0600); err != nil {
		t.Fatal("cannot stage overlapping test identities")
	}
	clear(oldIdentity)
	clear(overlap)
	recipient, err = exec.Command("age-keygen", "-y", wrong).Output()
	if err != nil {
		t.Fatal("new age public key failed")
	}
	rotated := commit("application.enc.yaml", encrypt("fetchit-secret-sentinel-rotated"))
	apply(rotated)
	readSecret("fetchit-secret-sentinel-rotated")
	if err := os.WriteFile(key, newIdentity, 0600); err != nil {
		t.Fatal("cannot retire old test identity")
	}
	clear(newIdentity)
	if err := os.Rename("repo/kube/application.enc.yaml", "repo/kube/renamed.enc.yaml"); err != nil {
		t.Fatal(err)
	}
	tree.AddWithOptions(&git.AddOptions{All: true})
	renamed, err := tree.Commit("rename", &git.CommitOptions{Author: &object.Signature{Name: "CI", Email: "ci@example.invalid", When: time.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	apply(renamed)
	readSecret("fetchit-secret-sentinel-rotated")
	if _, err := tree.Remove("kube/renamed.enc.yaml"); err != nil {
		t.Fatal(err)
	}
	// Keep the subtree present for FetchIt's existing Git diff logic.
	empty := commit("README", []byte("empty encrypted workload directory"))
	apply(empty)
	if err := exec.Command("podman", "pod", "exists", pod).Run(); err == nil {
		t.Fatal("deleted pod remains")
	}
	if err := exec.Command("podman", "secret", "exists", secret).Run(); err == nil {
		t.Fatal("deleted secret remains")
	}
	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "fetchit-secret-sentinel") {
			t.Fatal("secret leaked to logs")
		}
	}
	filepath.Walk("repo", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			data, _ := os.ReadFile(path)
			if bytes.Contains(data, []byte("fetchit-secret-sentinel")) {
				t.Error("plaintext leaked to Git checkout")
			}
		}
		return err
	})
}
