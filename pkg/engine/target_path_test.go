package engine

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestTrailingSlashTargetGitReconciliation(t *testing.T) {
	r, q := quadletTestRepo(t)
	first := quadletCommit(t, r, map[string]string{"app.container": "[Container]\nImage=quay.io/podman/hello\n", "nested/file.txt": "first"})
	compare := func(from, to plumbing.Hash) {
		t.Helper()
		paths := func(target string) []string {
			changes, err := applyChanges(context.Background(), q.target, target, nil, from, to, nil)
			if err != nil {
				t.Fatal(err)
			}
			var result []string
			for change, path := range changes {
				result = append(result, change.From.Name+" -> "+change.To.Name+" = "+path)
			}
			sort.Strings(result)
			return result
		}
		plain := paths("bundle")
		if len(plain) == 0 {
			t.Fatal("fixture has no changes")
		}
		for _, target := range []string{"bundle/", "bundle///"} {
			if got := paths(target); !reflect.DeepEqual(got, plain) {
				t.Fatalf("%s: %v != %v", target, got, plain)
			}
		}
	}
	compare(plumbing.ZeroHash, first)
	second := quadletCommit(t, r, map[string]string{"app.container": "[Container]\nImage=quay.io/podman/hello\n", "renamed.txt": "second"})
	compare(first, second)
	compare(second, plumbing.ZeroHash)
	q.TargetPath = "bundle/"
	if _, _, err := q.paths(); err != nil {
		t.Fatal("Quadlet rejected trailing separator", err)
	}
	if _, err := q.bundle(second); err != nil {
		t.Fatal("Quadlet failed to read normalized bundle", err)
	}
}
func TestTrailingSlashConfigNormalizationAndIdentity(t *testing.T) {
	common := func(name string) CommonMethod { return CommonMethod{Name: name, TargetPath: "examples/"} }
	raw := &Raw{CommonMethod: common("raw")}
	kube := &Kube{CommonMethod: common("kube")}
	quadlet := &Quadlet{CommonMethod: common("quadlet"), Root: true}
	files := &FileTransfer{CommonMethod: common("files")}
	systemd := &Systemd{CommonMethod: common("services")}
	ansible := &Ansible{CommonMethod: common("ansible")}
	engine := getMethodTargetScheds([]*TargetConfig{{Url: "https://example.invalid/repo", Branch: "main", Raw: []*Raw{raw}, Kube: []*Kube{kube}, Quadlet: []*Quadlet{quadlet}, FileTransfer: []*FileTransfer{files}, Systemd: []*Systemd{systemd}, Ansible: []*Ansible{ansible}}}, newFetchit())
	if len(engine.methodTargetScheds) != 6 {
		t.Fatal("missing configured methods")
	}
	for method := range engine.methodTargetScheds {
		if got := method.(interface{ GetTargetPath() string }).GetTargetPath(); got != "examples" {
			t.Fatalf("%s path=%q", method.GetKind(), got)
		}
	}
	raw.TargetPath = "examples/"
	withSlash := raw.workloadLabels()
	raw.TargetPath = "examples"
	if !reflect.DeepEqual(raw.workloadLabels(), withSlash) {
		t.Fatal("separator changes workload ownership")
	}
	quadlet.TargetPath = "examples/"
	configID := quadlet.GetName()
	namespace, home := quadlet.hostIdentity()
	quadlet.TargetPath = "examples"
	ns2, home2 := quadlet.hostIdentity()
	if configID != quadlet.GetName() || namespace != ns2 || home != home2 {
		t.Fatal("separator changes Quadlet identity")
	}
	for _, path := range []string{"/", "///", "", "../", "/absolute/", "folder/../"} {
		result := normalizedTargetPath(path)
		if path == "/" || path == "///" || path == "" {
			if result != path {
				t.Fatal("root/empty path changed meaning")
			}
		}
		if filepath.IsAbs(path) && !filepath.IsAbs(result) {
			t.Fatal("absolute path became relative")
		}
	}
}
