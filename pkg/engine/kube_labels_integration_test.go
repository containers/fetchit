//go:build sops_integration

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
	"go.uber.org/zap"
)

func assertKubeRuntimeLabels(t *testing.T, k *Kube) {
	t.Helper()
	filter := kubeOwnerLabel + "=" + k.kubeLabels()[kubeOwnerLabel]
	for _, resource := range []string{"pod", "container"} {
		args := []string{"ps", "-a", "-q", "--filter", "label=" + filter}
		if resource == "pod" {
			args = []string{"pod", "ps", "-q", "--filter", "label=" + filter}
		}
		out, err := exec.Command("podman", args...).Output()
		if err != nil || len(strings.Fields(string(out))) == 0 {
			t.Fatalf("no %s with expected owner label", resource)
		}
		for _, id := range strings.Fields(string(out)) {
			format := `{{ index .Labels "fetchit.containers.io/managed-by" }}`
			if resource == "container" {
				format = `{{ index .Config.Labels "fetchit.containers.io/managed-by" }}`
			}
			out, err := exec.Command("podman", resource, "inspect", "--format", format, id).Output()
			if err != nil || strings.TrimSpace(string(out)) != "fetchit" {
				t.Fatalf("%s missing managed-by label", resource)
			}
		}
	}
}

func TestKubeLabelsPodmanLifecycle(t *testing.T) {
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	conn, err := bindings.NewConnection(context.Background(), os.Getenv("SOPS_TEST_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	image := os.Getenv("SOPS_TEST_IMAGE")
	if image == "" {
		t.Fatal("SOPS_TEST_IMAGE required")
	}
	for _, kind := range []string{"Pod", "Deployment", "DaemonSet", "Job"} {
		t.Run(kind, func(t *testing.T) {
			name := fmt.Sprintf("fetchit-labels-%d-%s", os.Getpid(), strings.ToLower(kind))
			k := &Kube{CommonMethod: CommonMethod{Name: name}}
			podSpec := fmt.Sprintf("spec:\n  hostNetwork: %t\n  restartPolicy: Never\n  containers:\n  - name: app\n    image: %s\n    command: [sleep, infinity]\n", os.Getenv("SOPS_TEST_HOST_NETWORK") == "1", image)
			metadata := "metadata:\n  name: " + name + "\n  labels: {app: labels-test}\n"
			version := "v1"
			if kind != "Pod" {
				version = "apps/v1"
				if kind == "Job" {
					version = "batch/v1"
				}
				podSpec = "spec:\n  selector:\n    matchLabels: {app: labels-test}\n  template:\n" + indentKubeFixture(metadata+podSpec)
				metadata = "metadata: {name: " + name + "}\n"
			}
			path := filepath.Join(t.TempDir(), "manifest.yaml")
			input := []byte("apiVersion: " + version + "\nkind: " + kind + "\n" + metadata + podSpec)
			if err := os.WriteFile(path, input, 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				out, _ := exec.Command("podman", "pod", "ps", "-q", "--filter", "label="+kubeOwnerLabel+"="+k.kubeLabels()[kubeOwnerLabel]).Output()
				for _, id := range strings.Fields(string(out)) {
					exec.Command("podman", "pod", "rm", "-f", id).Run()
				}
			})
			if err := k.MethodEngine(context.Background(), conn, nil, path); err != nil {
				t.Fatal(err)
			}
			assertKubeRuntimeLabels(t, k)
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(input) {
				t.Fatal("modified repository manifest")
			}
			// Reapply and then delete through the normal lifecycle.
			if err := k.MethodEngine(context.Background(), conn, nil, path); err != nil {
				t.Fatal(err)
			}
			assertKubeRuntimeLabels(t, k)
			previous := string(input)
			if err := k.kubePodman(context.Background(), conn, deleteFile, &previous); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("podman", "pod", "ps", "-q", "--filter", "label="+kubeOwnerLabel+"="+k.kubeLabels()[kubeOwnerLabel]).Output()
			if err != nil || strings.TrimSpace(string(out)) != "" {
				t.Fatal("deleted labeled pod remains")
			}
		})
	}
}
