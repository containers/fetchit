//go:build samples_integration

package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"go.uber.org/zap"
)

func sampleHTTP(t *testing.T, port int, expected string) {
	t.Helper()
	client := http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == 200 && strings.Contains(string(body), expected) {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("sample on port %d did not serve %q", port, expected)
}

func sampleCommand(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("podman", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("podman %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func sampleArchitecture(t *testing.T, image string) {
	t.Helper()
	if got := sampleCommand(t, "image", "inspect", "--format", "{{.Architecture}}", image); got != runtime.GOARCH {
		t.Fatalf("sample architecture %s, runner %s", got, runtime.GOARCH)
	}
}

func TestSampleApplications(t *testing.T) {
	oldLogger := logger
	logger = zap.NewNop().Sugar()
	t.Cleanup(func() { logger = oldLogger })
	conn, err := bindings.NewConnection(context.Background(), os.Getenv("SAMPLE_TEST_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "examples")
	for _, file := range []string{"single-raw/welcome.yaml", "raw/color1.json", "raw/color2.yaml", "raw/cap.json", "raw/cap.yaml", "rollback/working.yaml", "rollback/1-working.yaml"} {
		t.Run(file, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join(root, file))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := rawPodFromBytes(input)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { exec.Command("podman", "rm", "-f", raw.Name).Run() })
			method := &Raw{CommonMethod: CommonMethod{Name: "samples", target: &Target{url: "https://example.invalid/samples", branch: "main"}}}
			if err := method.applyRawInput(context.Background(), conn, file, nil, input); err != nil {
				t.Fatal(err)
			}
			sampleArchitecture(t, raw.Image)
			sampleHTTP(t, int(raw.Ports[0].HostPort), "It works!")
		})
	}
	t.Run("image-load", func(t *testing.T) {
		image := "quay.io/notreal/httpd:latest" // Local archive tag; no registry request.
		sampleCommand(t, "tag", "docker.io/library/httpd:2.4-alpine", image)
		archive := filepath.Join(t.TempDir(), "httpd.tar")
		sampleCommand(t, "save", "-o", archive, image)
		sampleCommand(t, "image", "rm", image)
		t.Cleanup(func() {
			exec.Command("podman", "rm", "-f", "local").Run()
			exec.Command("podman", "image", "rm", image).Run()
		})
		if err := (&Image{}).podmanImageLoad(context.Background(), conn, archive); err != nil {
			t.Fatal(err)
		}
		input, err := os.ReadFile(filepath.Join(root, "imageLoad", "byo-image.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		method := &Raw{CommonMethod: CommonMethod{Name: "samples", target: &Target{url: "https://example.invalid/samples", branch: "main"}}}
		if err := method.applyRawInput(context.Background(), conn, "examples/imageLoad", nil, input); err != nil {
			t.Fatal(err)
		}
		sampleArchitecture(t, image)
		sampleHTTP(t, 9090, "It works!")
	})
	t.Run("kube", func(t *testing.T) {
		var input []byte
		for _, file := range []string{"1-pvc.yaml", "2-example.yaml", "3-example.yaml"} {
			data, err := os.ReadFile(filepath.Join(root, "kube", file))
			if err != nil {
				t.Fatal(err)
			}
			input = append(input, []byte("\n---\n")...)
			input = append(input, data...)
		}
		t.Cleanup(func() {
			exec.Command("podman", "pod", "rm", "-f", "nginx-pod", "colors_pod").Run()
			exec.Command("podman", "volume", "rm", "-f", "task-pv-claim").Run()
		})
		method := &Kube{CommonMethod: CommonMethod{Name: "samples", target: &Target{url: "https://example.invalid/samples", branch: "main"}}}
		if err := method.applyKubeInput(context.Background(), conn, "examples/kube", nil, input); err != nil {
			t.Fatal(err)
		}
		// The PVC example intentionally supplies its own website rather than the image's page.
		sampleCommand(t, "exec", "nginx-pod-nginx-server", "sh", "-c", "printf 'Sample PVC site' > /usr/share/nginx/html/index.html")
		sampleHTTP(t, 8080, "Sample PVC site")
		sampleHTTP(t, 7080, "It works!")
		sampleArchitecture(t, "docker.io/library/httpd:2.4-alpine")
		sampleArchitecture(t, "docker.io/library/nginx:stable-alpine")
	})
	t.Run("systemd-image", func(t *testing.T) {
		// The legacy service's image also has to be runnable on the native architecture.
		unit, err := os.ReadFile(filepath.Join(root, "systemd", "httpd.service"))
		if err != nil {
			t.Fatal(err)
		}
		var image string
		for _, line := range strings.Split(string(unit), "\n") {
			if strings.HasPrefix(line, "ExecStart=") {
				parts := strings.Fields(line)
				image = parts[len(parts)-1]
			}
		}
		if image == "" {
			t.Fatal("systemd sample has no image")
		}
		site := t.TempDir()
		if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("Sample systemd site"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(site, 0755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { exec.Command("podman", "rm", "-f", "sample-systemd").Run() })
		sampleCommand(t, "run", "-d", "--name", "sample-systemd", "-p", "18080:8080", "-v", site+":/var/www/html:ro", image)
		sampleArchitecture(t, image)
		sampleHTTP(t, 18080, "Sample systemd site")
	})
	t.Run("quadlet-image", func(t *testing.T) {
		unit, err := os.ReadFile(filepath.Join(root, "quadlet", "units", "web.container"))
		if err != nil {
			t.Fatal(err)
		}
		var image string
		for _, line := range strings.Split(string(unit), "\n") {
			if strings.HasPrefix(line, "Image=") {
				image = strings.TrimPrefix(line, "Image=")
			}
		}
		if image == "" {
			t.Fatal("quadlet sample has no image")
		}
		sampleCommand(t, "run", "--rm", image, "/bin/sh", "-c", "echo sample-quadlet")
		sampleArchitecture(t, image)
	})
}
