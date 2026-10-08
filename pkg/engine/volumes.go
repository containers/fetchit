package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/volumes"
	entities "github.com/containers/podman/v5/pkg/domain/entities/types"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
)

const kubeCreateVolumes = "fetchit.containers.io/create-volumes"

var namedVolumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// Creation is per named mount. Never recreate, relabel, or remove existing storage.
func ensureRawVolumes(conn context.Context, mounts []namedVolume) error {
	for _, mount := range mounts {
		if mount.Create && (!namedVolumeName.MatchString(mount.Name) || !strings.HasPrefix(mount.Dest, "/")) {
			return errors.New("volume creation requires a named volume and an absolute container destination")
		}
	}
	seen := map[string]bool{}
	for _, mount := range mounts {
		if !mount.Create || seen[mount.Name] {
			continue
		}
		seen[mount.Name] = true
		_, err := volumes.Inspect(conn, mount.Name, nil)
		if err == nil {
			continue
		}
		code, _ := bindings.CheckResponseCode(err)
		if code != http.StatusNotFound {
			return err
		}
		if _, err = volumes.Create(conn, entities.VolumeCreateOptions{Name: mount.Name, Driver: "local", IgnoreIfExists: true}, nil); err != nil {
			return err
		}
	}
	return nil
}

// A workload may opt into generating missing PVC declarations for its claim references.
// Mounts remain exclusively those specified in the workload, including init containers.
func addKubeVolumeDeclarations(input []byte) ([]byte, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var docs []map[string]interface{}
	declared, requested := map[string]bool{}, map[string]bool{}
	for {
		var doc map[string]interface{}
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, &KubeManifestError{Operation: "decode", cause: err}
		}
		if len(doc) == 0 {
			continue
		}
		docs = append(docs, doc)
		if doc["kind"] == "PersistentVolumeClaim" {
			metadata, err := kubeMapping(doc, "metadata")
			if err != nil {
				return nil, err
			}
			name, _ := metadata["name"].(string)
			declared[name] = true
		}
		workload := doc
		switch doc["kind"] {
		case "Deployment", "DaemonSet", "Job":
			spec, err := kubeMapping(doc, "spec")
			if err != nil {
				return nil, err
			}
			workload, err = kubeMapping(spec, "template")
			if err != nil {
				return nil, err
			}
		case "Pod":
		default:
			continue
		}
		metadata, err := kubeMapping(workload, "metadata")
		if err != nil {
			return nil, err
		}
		annotations, err := kubeMapping(metadata, "annotations")
		if err != nil {
			return nil, err
		}
		flag, present := annotations[kubeCreateVolumes]
		if !present || flag == "false" {
			continue
		}
		if flag != "true" {
			return nil, &KubeManifestError{Operation: "validate volume creation flag", cause: errors.New("create-volumes must be the quoted string true or false")}
		}
		spec, err := kubeMapping(workload, "spec")
		if err != nil {
			return nil, err
		}
		list, ok := spec["volumes"].([]interface{})
		if spec["volumes"] != nil && !ok {
			return nil, &KubeManifestError{Operation: "validate volumes", cause: errors.New("volumes must be a list")}
		}
		for _, item := range list {
			volume, ok := item.(map[string]interface{})
			if !ok {
				return nil, &KubeManifestError{Operation: "validate volumes", cause: errors.New("invalid volume declaration")}
			}
			if volume["persistentVolumeClaim"] == nil {
				continue
			}
			claim, err := kubeMapping(volume, "persistentVolumeClaim")
			if err != nil {
				return nil, err
			}
			name, _ := claim["claimName"].(string)
			if name == "" || len(validation.IsDNS1123Subdomain(name)) != 0 {
				return nil, &KubeManifestError{Operation: "validate claim name", cause: errors.New("invalid claim name")}
			}
			requested[name] = true
		}
	}
	names := make([]string, 0, len(requested))
	for name := range requested {
		if !declared[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	defer encoder.Close()
	defer func() { clear(output.Bytes()) }()
	for _, name := range names {
		if err := encoder.Encode(map[string]interface{}{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]interface{}{"name": name}}); err != nil {
			return nil, err
		}
	}
	for _, doc := range docs {
		if err := encoder.Encode(doc); err != nil {
			return nil, err
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return append([]byte(nil), output.Bytes()...), nil
}
