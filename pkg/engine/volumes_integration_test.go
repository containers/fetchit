//go:build sops_integration

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/podman/v5/pkg/bindings"
	"go.uber.org/zap"
)

// Run on the same rootful/rootless and Fedora Podman 5 matrix as other lifecycles.
func TestVolumesPodmanLifecycle(t *testing.T) {
	old := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = old })
	conn, err := bindings.NewConnection(context.Background(), os.Getenv("SOPS_TEST_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	image := os.Getenv("SOPS_TEST_IMAGE")
	if image == "" {
		t.Fatal("SOPS_TEST_IMAGE required")
	}
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("podman", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("podman %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	for _, kind := range []string{"generated-pvc", "explicit-pvc", "raw"} {
		t.Run(kind, func(t *testing.T) {
			name := fmt.Sprintf("fetchit-volumes-%d-%s", os.Getpid(), kind)
			volume := name + "-data"
			common := CommonMethod{Name: name}
			k := &Kube{CommonMethod: common}
			raw := &Raw{CommonMethod: common}
			t.Cleanup(func() {
				out, _ := exec.Command("podman", "ps", "-aq", "--filter", "label="+kubeOwnerLabel+"="+common.workloadLabels()[kubeOwnerLabel]).Output()
				for _, id := range strings.Fields(string(out)) {
					exec.Command("podman", "rm", "-f", id).Run()
				}
				out, _ = exec.Command("podman", "pod", "ps", "-q", "--filter", "label="+kubeOwnerLabel+"="+common.workloadLabels()[kubeOwnerLabel]).Output()
				for _, id := range strings.Fields(string(out)) {
					exec.Command("podman", "pod", "rm", "-f", id).Run()
				}
				exec.Command("podman", "volume", "rm", volume).Run()
			})
			input := fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  annotations: {%s: 'true'}\nspec:\n  hostNetwork: %t\n  containers:\n  - name: app\n    image: %s\n    command: [sleep, infinity]\n    volumeMounts: [{name: storage, mountPath: /data}]\n  volumes:\n  - name: storage\n    persistentVolumeClaim: {claimName: %s}\n", name, kubeCreateVolumes, os.Getenv("SOPS_TEST_HOST_NETWORK") == "1", image, volume)
			if kind == "explicit-pvc" {
				input = strings.ReplaceAll(input, "  annotations: {"+kubeCreateVolumes+": 'true'}\n", "")
				input += "---\napiVersion: v1\nkind: PersistentVolumeClaim\nmetadata: {name: " + volume + "}\n"
			}
			path := filepath.Join(t.TempDir(), "workload.yaml")
			apply := func() error { return k.MethodEngine(context.Background(), conn, nil, path) }
			remove := func() error { return k.kubePodman(context.Background(), conn, deleteFile, &input) }
			if kind == "raw" {
				data, err := json.Marshal(RawPod{Image: image, Name: name, Volumes: []namedVolume{{Name: volume, Dest: "/data", Create: true}}})
				if err != nil {
					t.Fatal(err)
				}
				input = string(data)
				apply = func() error { return raw.applyRawInput(context.Background(), conn, path, nil, []byte(input)) }
				remove = func() error { return raw.applyRawInput(context.Background(), conn, deleteFile, &input, nil) }
			}
			if err := os.WriteFile(path, []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			if err := apply(); err != nil {
				t.Fatal(err)
			}
			ids := strings.Fields(run("ps", "-aq", "--filter", "label="+kubeOwnerLabel+"="+common.workloadLabels()[kubeOwnerLabel]))
			mounted := false
			for _, id := range ids {
				mounts := run("inspect", "--format", "{{range .Mounts}}{{.Name}}:{{.Destination}} {{end}}", id)
				mounted = mounted || strings.Contains(mounts, volume+":/data")
			}
			if !mounted {
				t.Fatal("named volume not mounted at /data")
			}
			run("run", "--rm", "-v", volume+":/data", image, "sh", "-c", "echo sentinel > /data/marker")
			// Reapply, remove, and re-add must reuse storage without the historical exists error.
			if err := apply(); err != nil {
				t.Fatal(err)
			}
			if got := run("run", "--rm", "-v", volume+":/data:ro", image, "cat", "/data/marker"); got != "sentinel" {
				t.Fatal("reapply lost data")
			}
			if err := remove(); err != nil {
				t.Fatal(err)
			}
			run("volume", "inspect", volume)
			if err := apply(); err != nil {
				t.Fatal(err)
			}
			if got := run("run", "--rm", "-v", volume+":/data:ro", image, "cat", "/data/marker"); got != "sentinel" {
				t.Fatal("re-add lost data")
			}
		})
	}
}
