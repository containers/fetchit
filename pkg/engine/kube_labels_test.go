package engine

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestKubeLabelsPreserveWorkloads(t *testing.T) {
	labels := (&Kube{CommonMethod: CommonMethod{Name: "app"}}).kubeLabels()
	for _, kind := range []string{"Pod", "Deployment", "DaemonSet", "Job"} {
		t.Run(kind, func(t *testing.T) {
			metadata := "metadata:\n  name: app\n  labels:\n    app: original\n    fetchit.containers.io/managed-by: spoof\n"
			workload := "spec:\n  containers:\n  - name: web\n    image: example.invalid/web\n    env:\n    - name: FLAG\n      value: 'true'\n"
			if kind != "Pod" {
				workload = "spec:\n  selector:\n    matchLabels: {app: original}\n  template:\n" + indentKubeFixture(metadata+workload)
				metadata = "metadata: {name: controller}\n"
			}
			input := []byte("apiVersion: v1\nkind: " + kind + "\n" + metadata + workload + "---\napiVersion: v1\nkind: Secret\nmetadata: {name: credentials}\nstringData: {password: 'yes'}\n")
			labeled, err := labelKubeManifest(input, labels)
			if err != nil {
				t.Fatal(err)
			}
			decode := func(data []byte) []map[string]interface{} {
				d := yaml.NewDecoder(bytes.NewReader(data))
				var result []map[string]interface{}
				for {
					var doc map[string]interface{}
					if err := d.Decode(&doc); err == io.EOF {
						break
					} else if err != nil {
						t.Fatal(err)
					}
					result = append(result, doc)
				}
				return result
			}
			before, after := decode(input), decode(labeled)
			if !reflect.DeepEqual(before[1], after[1]) {
				t.Fatal("changed Secret")
			}
			pod := after[0]
			original := before[0]
			if kind != "Pod" {
				spec := pod["spec"].(map[string]interface{})
				if !reflect.DeepEqual(spec["selector"], original["spec"].(map[string]interface{})["selector"]) {
					t.Fatal("changed selector")
				}
				pod = spec["template"].(map[string]interface{})
				original = original["spec"].(map[string]interface{})["template"].(map[string]interface{})
			}
			got := pod["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
			if got["app"] != "original" || got[kubeManagedByLabel] != "fetchit" || got[kubeOwnerLabel] != labels[kubeOwnerLabel] {
				t.Fatal("labels missing or overwritten incorrectly")
			}
			if !reflect.DeepEqual(pod["spec"], original["spec"]) {
				t.Fatal("changed pod spec or string values")
			}
			again, err := labelKubeManifest(labeled, labels)
			if err != nil || !bytes.Equal(labeled, again) {
				t.Fatal("labeling is not idempotent")
			}
		})
	}
}

func indentKubeFixture(s string) string {
	return "    " + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n    ") + "\n"
}

func TestKubeOwnerIdentity(t *testing.T) {
	k := &Kube{CommonMethod: CommonMethod{Name: "app", TargetPath: "kube", target: &Target{url: "https://example.invalid/repo", branch: "main"}}}
	first := k.kubeLabels()[kubeOwnerLabel]
	if len(first) != 32 || first != k.kubeLabels()[kubeOwnerLabel] {
		t.Fatal("unstable owner label")
	}
	for _, change := range []func(){func() { k.Name = "other" }, func() { k.TargetPath = "other" }, func() { k.target.branch = "other" }, func() { k.target.url = "other" }} {
		change()
		next := k.kubeLabels()[kubeOwnerLabel]
		if next == first {
			t.Fatal("distinct identity has same owner")
		}
		first = next
	}
}

func TestKubeLabelsRejectMalformedStructure(t *testing.T) {
	for _, input := range []string{"[invalid", "kind: Pod\nmetadata: invalid", "kind: Pod\nmetadata: {labels: []}", "kind: Deployment\nspec: {template: invalid}"} {
		if _, err := labelKubeManifest([]byte(input), map[string]string{kubeManagedByLabel: "fetchit"}); err == nil {
			t.Fatal("accepted malformed structure")
		}
	}
}

func TestKubeManifestErrorPreservesSafeCause(t *testing.T) {
	_, err := labelKubeManifest([]byte("kind: Pod\nmetadata: secret-sentinel\nmetadata: secret-sentinel\n"), nil)
	var failure *KubeManifestError
	if !errors.As(err, &failure) || failure.Operation != "decode" || errors.Unwrap(failure) == nil {
		t.Fatal("lost decoder error classification or cause")
	}
	if strings.Contains(err.Error(), "secret-sentinel") {
		t.Fatal("unsafe parser details in log message")
	}
}
