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

const kubeManagedByLabel = "fetchit.containers.io/managed-by"
const kubeOwnerLabel = "fetchit.containers.io/owner"

// The owner is stable across restarts without exposing repository URLs or credentials.
func (k *Kube) kubeLabels() map[string]string {
	url, branch := "", ""
	if k.target != nil {
		url, branch = k.target.url, k.target.branch
	}
	identity, _ := json.Marshal([]string{url, branch, k.Name, k.TargetPath})
	digest := sha256.Sum256(identity)
	return map[string]string{kubeManagedByLabel: "fetchit", kubeOwnerLabel: hex.EncodeToString(digest[:16])}
}

// Label the pod metadata Podman uses for both pods and their containers. Controller
// selectors and unrelated resources are deliberately left unchanged.
func labelKubeManifest(input []byte, labels map[string]string) ([]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	defer encoder.Close()
	for {
		var doc map[string]interface{}
		if err := decoder.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return nil, errors.New("cannot decode kube manifest for FetchIt labels")
		}
		if len(doc) == 0 {
			continue
		}
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
			return nil, err
		}
		if metadata != nil {
			existing, err := kubeMapping(metadata, "labels")
			if err != nil {
				return nil, err
			}
			for key, value := range labels {
				existing[key] = value
			}
		}
		if err := encoder.Encode(doc); err != nil {
			return nil, errors.New("cannot encode labeled kube manifest")
		}
	}
	return output.Bytes(), nil
}

func kubeMapping(parent map[string]interface{}, key string) (map[string]interface{}, error) {
	if value, ok := parent[key]; ok && value != nil {
		mapping, ok := value.(map[string]interface{})
		if !ok {
			return nil, errors.New("invalid kube metadata or workload structure")
		}
		return mapping, nil
	}
	mapping := make(map[string]interface{})
	parent[key] = mapping
	return mapping, nil
}
