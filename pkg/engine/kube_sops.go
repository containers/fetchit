package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings/play"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"
)

type preparedKubeChange struct {
	path           string
	previous, next []byte
}

func clearPreparedKube(changes []preparedKubeChange) {
	for _, change := range changes {
		clear(change.previous)
		clear(change.next)
	}
}

func (k *Kube) prepareSOPSChanges(ctx context.Context, changeMap map[*object.Change]string, decrypt func(context.Context, []byte) ([]byte, error)) ([]preparedKubeChange, error) {
	if err := k.SOPS.validate(); err != nil {
		return nil, err
	}
	if target := k.GetTarget(); target != nil {
		repository, _ := filepath.Abs(getDirectory(target))
		key, err := filepath.EvalSymlinks(k.SOPS.AgeKeyFile)
		if err != nil {
			return nil, errors.New("cannot resolve age key file")
		}
		repository, err = filepath.EvalSymlinks(repository)
		if err == nil {
			relative, err := filepath.Rel(repository, key)
			if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				return nil, errors.New("age key file must be outside the repository")
			}
		}
	}
	keys := make([]*object.Change, 0, len(changeMap))
	for change := range changeMap {
		keys = append(keys, change)
	}
	sort.Slice(keys, func(i, j int) bool {
		name := func(c *object.Change) string {
			if c == nil {
				return ""
			}
			if c.To.Name != "" {
				return c.To.Name
			}
			return c.From.Name
		}
		return name(keys[i]) < name(keys[j])
	})
	prepared := make([]preparedKubeChange, 0, len(keys))
	success := false
	defer func() {
		if !success {
			clearPreparedKube(prepared)
		}
	}()
	total := 0
	decode := func(input []byte) ([]byte, error) {
		plain, err := decrypt(ctx, input)
		if err != nil {
			return nil, errors.New("cannot decrypt manifest; check age keys and SOPS integrity")
		}
		if len(plain) > sopsFileLimit || total+len(plain) > sopsBatchLimit {
			clear(plain)
			return nil, errors.New("decrypted manifests exceed size limits")
		}
		if err := validateDecryptedKube(plain); err != nil {
			clear(plain)
			return nil, err
		}
		total += len(plain)
		return plain, nil
	}
	for _, change := range keys {
		item := preparedKubeChange{path: changeMap[change]}
		prepared = append(prepared, item)
		itemPtr := &prepared[len(prepared)-1]
		var nextFile *object.File
		if change != nil {
			old, next, err := change.Files()
			if err != nil {
				return nil, errors.New("cannot read previous encrypted manifest")
			}
			nextFile = next
			if old != nil {
				data, err := readSOPSGitFile(old)
				if err != nil {
					return nil, err
				}
				itemPtr.previous, err = decode(data)
				if err != nil {
					return nil, err
				}
			}
		}
		if item.path != deleteFile {
			var data []byte
			var err error
			if nextFile != nil {
				data, err = readSOPSGitFile(nextFile)
			} else {
				data, err = readSOPSInput(item.path)
			}
			if err != nil {
				return nil, err
			}
			itemPtr.next, err = decode(data)
			if err != nil {
				return nil, err
			}
		}
	}
	success = true
	return prepared, nil
}

func validateDecryptedKube(input []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	seen := map[string]bool{}
	for {
		var doc struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			SOPS interface{} `yaml:"sops"`
		}
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil || doc.APIVersion == "" || doc.Metadata.Name == "" || doc.SOPS != nil {
			return errors.New("invalid decrypted Kubernetes YAML")
		}
		switch doc.Kind {
		case "Pod", "Deployment", "DaemonSet", "Job", "Secret", "ConfigMap", "PersistentVolumeClaim":
		default:
			return errors.New("unsupported decrypted Kubernetes kind")
		}
		identity := doc.Kind + "/" + doc.Metadata.Name
		if seen[identity] {
			return errors.New("duplicate decrypted resource identity")
		}
		seen[identity] = true
	}
	if len(seen) == 0 {
		return errors.New("empty decrypted manifest")
	}
	pods, err := podFromBytes(input)
	if err != nil {
		return errors.New("invalid decrypted pod specification")
	}
	for _, pod := range pods {
		if err := validatePod(pod); err != nil {
			return errors.New("invalid decrypted pod specification")
		}
	}
	return nil
}

func (k *Kube) runPreparedSOPS(ctx, conn context.Context, changes []preparedKubeChange) error {
	if err := validateNetworks(conn, k.Networks); err != nil {
		return errors.New("encrypted workload network preflight failed")
	}
	for _, change := range changes {
		if change.previous != nil {
			if err := stopPods(conn, change.previous); err != nil && !strings.Contains(err.Error(), "no such pod") {
				return errors.New("cannot stop previous encrypted workload")
			}
		}
		if change.next != nil {
			if err := stopPods(conn, change.next); err != nil && !strings.Contains(err.Error(), "no such pod") {
				return errors.New("cannot stop existing encrypted workload")
			}
			if _, err := play.KubeWithBody(conn, bytes.NewReader(change.next), kubeNetworkOptions(k.Networks)); err != nil {
				return errors.New("cannot play encrypted workload")
			}
		}
	}
	return nil
}

// Read ciphertext from the commit rather than a mutable shared worktree.
func readSOPSGitFile(file *object.File) ([]byte, error) {
	reader, err := file.Reader()
	if err != nil {
		return nil, errors.New("cannot read encrypted Git blob")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, sopsFileLimit+1))
	if err != nil || len(data) > sopsFileLimit {
		return nil, errors.New("encrypted Git blob exceeds read limits")
	}
	return data, nil
}
