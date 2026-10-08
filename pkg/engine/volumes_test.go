package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-git/go-git/v5/plumbing/object"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRawVolumeCreation(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		mounts               []namedVolume
		creates, inspections int
		fail                 bool
	}{
		{"default", 404, []namedVolume{{Name: "data", Dest: "/data"}}, 0, 0, false},
		{"create only selected", 404, []namedVolume{{Name: "data", Dest: "/data", Create: true}, {Name: "other", Dest: "/other"}}, 1, 1, false},
		{"reuse", 200, []namedVolume{{Name: "data", Dest: "/data", Create: true}}, 0, 1, false},
		{"inspect failure", 500, []namedVolume{{Name: "data", Dest: "/data", Create: true}}, 0, 1, true},
		{"duplicate mounts", 404, []namedVolume{{Name: "data", Dest: "/data", Create: true}, {Name: "data", Dest: "/second", Create: true}}, 1, 1, false},
		{"invalid name", 404, []namedVolume{{Name: "../bad", Dest: "/data", Create: true}}, 0, 0, true},
		{"invalid path", 404, []namedVolume{{Name: "data", Dest: "relative", Create: true}}, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creates, inspections := 0, 0
			conn := testPodmanConnection(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/json"):
					inspections++
					w.WriteHeader(tc.status)
					fmt.Fprintf(w, `{"Name":"data","message":"inspect","response":%d}`, tc.status)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/volumes/create"):
					creates++
					var request map[string]interface{}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Fatal(err)
					}
					if request["Name"] != "data" || request["IgnoreIfExists"] != true {
						t.Errorf("unexpected create request: %#v", request)
					}
					fmt.Fprint(w, `{"Name":"data"}`)
				default:
					t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			})
			err := ensureRawVolumes(conn, tc.mounts)
			if (err != nil) != tc.fail || creates != tc.creates || inspections != tc.inspections {
				t.Fatalf("err=%v creates=%d inspections=%d", err, creates, inspections)
			}
			specs := convertVolumes(tc.mounts)
			for i, mount := range tc.mounts {
				if specs[i].Name != mount.Name || specs[i].Dest != mount.Dest {
					t.Fatal("mount changed")
				}
			}
		})
	}
}

func TestKubeVolumeDeclarations(t *testing.T) {
	workload := func(name, flag, claim string) string {
		return fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata:\n  name: %s\n  annotations: {%s: %s}\nspec:\n  volumes:\n  - name: storage\n    persistentVolumeClaim: {claimName: %s}\n  containers:\n  - name: app\n    image: example.invalid/app\n    volumeMounts: [{name: storage, mountPath: /data, readOnly: true}]\n", name, kubeCreateVolumes, flag, claim)
	}
	for _, tc := range []struct {
		name, input string
		count       int
		fail        bool
	}{
		{"default", strings.ReplaceAll(workload("one", `"false"`, "data"), "  annotations: {"+kubeCreateVolumes+`: "false"}`+"\n", ""), 0, false},
		{"enabled", workload("one", `"true"`, "data"), 1, false},
		{"mixed", workload("one", `"true"`, "data") + "---\n" + workload("two", `"false"`, "other"), 1, false},
		{"deduplicated", workload("one", `"true"`, "data") + "---\n" + workload("two", `"true"`, "data"), 1, false},
		{"explicit PVC preserved", workload("one", `"true"`, "data") + "---\napiVersion: v1\nkind: PersistentVolumeClaim\nmetadata: {name: data, annotations: {volume.podman.io/uid: '1001'}}\n", 1, false},
		{"boolean rejected", workload("one", "true", "data"), 0, true},
		{"invalid claim", workload("one", `"true"`, "../bad"), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := addKubeVolumeDeclarations([]byte(tc.input))
			if (err != nil) != tc.fail {
				t.Fatalf("%v", err)
			}
			if err != nil {
				return
			}
			defer clear(output)
			decoder := yaml.NewDecoder(bytes.NewReader(output))
			count := 0
			for {
				var doc map[string]interface{}
				err := decoder.Decode(&doc)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if doc["kind"] == "PersistentVolumeClaim" {
					count++
					metadata := doc["metadata"].(map[string]interface{})
					if metadata["name"] != "data" {
						t.Fatal("created unrelated claim")
					}
					if tc.name == "explicit PVC preserved" && metadata["annotations"].(map[string]interface{})["volume.podman.io/uid"] != "1001" {
						t.Fatal("lost PVC options")
					}
				}
				if doc["kind"] == "Pod" && !strings.Contains(string(output), "readOnly: true") {
					t.Fatal("mount options lost")
				}
			}
			if count != tc.count {
				t.Fatalf("PVC count %d want %d", count, tc.count)
			}
		})
	}
}

func TestKubeControllerVolumeDeclarations(t *testing.T) {
	for _, kind := range []string{"Deployment", "DaemonSet", "Job"} {
		t.Run(kind, func(t *testing.T) {
			input := []byte("apiVersion: apps/v1\nkind: " + kind + "\nmetadata: {name: controller}\nspec:\n  template:\n    metadata:\n      annotations: {" + kubeCreateVolumes + ": 'true'}\n    spec:\n      volumes: [{name: storage, persistentVolumeClaim: {claimName: controller-data}}]\n      containers: [{name: app, image: alpine, volumeMounts: [{name: storage, mountPath: /data}]}]\n")
			output, err := labelKubeManifest(input, map[string]string{"test-owner": "controller"})
			if err != nil {
				t.Fatal(err)
			}
			defer clear(output)
			if !strings.Contains(string(output), "kind: PersistentVolumeClaim") || !strings.Contains(string(output), "name: controller-data") || !strings.Contains(string(output), "test-owner: controller") {
				t.Fatal("missing generated PVC or workload labels")
			}
		})
	}
}

func TestSOPSVolumeDeclarations(t *testing.T) {
	k := &Kube{SOPS: sopsTestSettings(t)}
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}
	input := strings.Replace(testPlainKube, "metadata:\n", "metadata:\n  annotations: {"+kubeCreateVolumes+": 'true'}\n", 1)
	input = strings.Replace(input, "spec:\n", "spec:\n  volumes: [{name: storage, persistentVolumeClaim: {claimName: encrypted-data}}]\n", 1)
	prepared, err := k.prepareSOPSChanges(context.Background(), map[*object.Change]string{nil: path}, func(context.Context, []byte) ([]byte, error) { return []byte(input), nil })
	if err != nil {
		t.Fatal(err)
	}
	defer clearPreparedKube(prepared)
	if len(prepared) != 1 || !strings.Contains(string(prepared[0].next), "name: encrypted-data") || !strings.Contains(string(prepared[0].next), "kind: PersistentVolumeClaim") {
		t.Fatal("decrypted manifest not expanded")
	}
}
