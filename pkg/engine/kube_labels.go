package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

const (
	kubeManagedByLabel = "fetchit.containers.io/managed-by"
	kubeOwnerLabel     = "fetchit.containers.io/owner"
)

// The owner is stable across restarts without exposing repository URLs or credentials.
func (k *Kube) kubeLabels() map[string]string {
	url, branch := "", ""
	if k.target != nil {
		url, branch = k.target.url, k.target.branch
	}
	return map[string]string{kubeManagedByLabel: "fetchit", kubeOwnerLabel: kubeOwner(url, branch, k.Name, k.TargetPath)}
}

func kubeOwner(url, branch, name, targetPath string) string {
	// Marshaling a fixed array of strings cannot fail (no unsupported values/cycles).
	identity, _ := json.Marshal([4]string{url, branch, name, targetPath})
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:16])
}

// KubeManifestError retains parser/encoder details for explicit inspection while
// keeping potentially decrypted values out of ordinary log messages.
type KubeManifestError struct {
	Operation string
	cause     error
}

func (e *KubeManifestError) Error() string {
	return "cannot " + e.Operation + " kube manifest for FetchIt labels"
}
func (e *KubeManifestError) Unwrap() error { return e.cause }

// Label the pod metadata Podman uses for both pods and their containers. Controller
// selectors and unrelated resources are deliberately left unchanged.
func labelKubeManifest(input []byte, labels map[string]string) (result []byte, err error) {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	defer func() {
		if err != nil {
			clear(output.Bytes())
		}
	}()
	for {
		var doc map[string]interface{}
		if err := decoder.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return nil, &KubeManifestError{Operation: "decode", cause: err}
		}
		if len(doc) == 0 {
			continue
		}
		if err := labelKubeDocument(doc, labels); err != nil {
			return nil, err
		}

		if err := encoder.Encode(doc); err != nil {
			return nil, &KubeManifestError{Operation: "encode", cause: err}
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, &KubeManifestError{Operation: "finalize", cause: err}
	}
	return output.Bytes(), nil
}

func labelKubeDocument(doc map[string]interface{}, labels map[string]string) error {
	kind, _ := doc["kind"].(string)
	var metadata map[string]interface{}
	var err error
	switch kind {
	case "Pod":
		metadata, err = kubeMapping(doc, "metadata")
	case "Deployment", "DaemonSet", "Job":
		var spec, template map[string]interface{}
		spec, err = kubeMapping(doc, "spec")
		if err == nil {
			template, err = kubeMapping(spec, "template")
		}
		if err == nil {
			metadata, err = kubeMapping(template, "metadata")
		}
	}
	if err != nil {
		return err
	}
	if metadata != nil {
		existing, err := kubeMapping(metadata, "labels")
		if err != nil {
			return err
		}
		for key, value := range labels {
			existing[key] = value
		}
	}
	return nil
}

func kubeMapping(parent map[string]interface{}, key string) (map[string]interface{}, error) {
	if value, ok := parent[key]; ok && value != nil {
		mapping, ok := value.(map[string]interface{})
		if !ok {
			return nil, &KubeManifestError{Operation: "validate", cause: errors.New("invalid metadata or workload structure")}
		}
		return mapping, nil
	}
	mapping := make(map[string]interface{})
	parent[key] = mapping
	return mapping, nil
}
