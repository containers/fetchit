package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/play"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "sigs.k8s.io/yaml"
)

const kubeMethod = "kube"

// Kube to launch pods using podman kube-play
type Kube struct {
	CommonMethod `mapstructure:",squash"`
}

func (k *Kube) GetKind() string {
	return kubeMethod
}

func (k *Kube) Process(ctx, conn context.Context, PAT string, skew int) {
	target := k.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	initial := k.initialRun
	tag := []string{"yaml", "yml"}
	if initial {
		err := getRepo(target, PAT)
		if err != nil {
			logger.Errorf("Failed to clone repository %s: %v", target.url, err)
			return
		}

		err = zeroToCurrent(ctx, conn, k, target, &tag)
		if err != nil {
			logger.Errorf("Error moving to current: %v", err)
			return
		}
	}

	err := currentToLatest(ctx, conn, k, target, &tag)
	if err != nil {
		logger.Errorf("Error moving current to latest: %v", err)
		return
	}

	k.initialRun = false
}

func (k *Kube) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	prev, err := getChangeString(change)
	if err != nil {
		return err
	}
	return k.kubePodman(ctx, conn, path, prev)
}

func (k *Kube) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, k.GetTarget(), k.GetTargetPath(), k.Glob, currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChanges(ctx, conn, k, changeMap); err != nil {
		return err
	}
	return nil
}

func (k *Kube) kubePodman(ctx, conn context.Context, path string, prev *string) error {
	if path != deleteFile {
		logger.Infof("Creating podman container from %s using kube method", path)
	}

	if prev != nil {
		err := stopPods(conn, []byte(*prev))
		if err != nil {
			return utils.WrapErr(err, "Error stopping pods")
		}
	}

	if path != deleteFile {
		kubeYaml, err := ioutil.ReadFile(path)
		if err != nil {
			return utils.WrapErr(err, "Error reading file")
		}

		// Try stopping the pods, don't care if they don't exist
		err = stopPods(conn, kubeYaml)
		if err != nil {
			if !strings.Contains(err.Error(), "no such pod") {
				return utils.WrapErr(err, "Error stopping pods")
			}
		}

		err = createPods(conn, path, kubeYaml)
		if err != nil {
			return utils.WrapErr(err, "Error creating pod")
		}
	}

	return nil
}

func stopPods(ctx context.Context, podSpec []byte) error {
	conn, err := bindings.GetClient(ctx)
	if err != nil {
		return utils.WrapErr(err, "Error getting podman connection")
	}

	response, err := conn.DoRequest(ctx, bytes.NewReader(podSpec), http.MethodDelete, "/play/kube", nil, nil)
	if err != nil {
		return utils.WrapErr(err, "Error making podman API call to delete pod")
	}

	var report entities.PlayKubeReport
	if err := response.Process(&report); err != nil {
		return utils.WrapErr(err, "Error processing podman response when deleting pod")
	}

	return nil
}

func createPods(ctx context.Context, path string, specs []byte) error {
	pod_list, err := podFromBytes(specs)
	if err != nil {
		return utils.WrapErr(err, "Error getting list of pods in spec")
	}

	for _, pod := range pod_list {
		err = validatePod(pod)
		if err != nil {
			return utils.WrapErr(err, "Error validating pod spec")
		}
	}

	_, err = play.Kube(ctx, path, nil)
	if err != nil {
		return utils.WrapErr(err, "Error playing kube spec")
	}

	logger.Infof("Created pods from spec in %s", path)
	return nil
}

func podFromBytes(input []byte) ([]v1.Pod, error) {
	var t metav1.TypeMeta
	d := yaml.NewDecoder(bytes.NewReader(input))
	ret := make([]v1.Pod, 0)

	for {
		var i interface{}
		err := d.Decode(&i)
		if err == io.EOF {
			break
		}
		if err != nil {
			return ret, utils.WrapErr(err, "Error decoding yaml")
		}

		o, err := yaml.Marshal(i)
		if err != nil {
			return ret, utils.WrapErr(err, "Error marshalling yaml into object for conversion to json")
		}

		b, err := k8syaml.YAMLToJSON(o)
		if err != nil {
			return ret, utils.WrapErr(err, "Error converting yaml to json")
		}

		err = json.Unmarshal(b, &t)
		if err != nil {
			return ret, utils.WrapErr(err, "Error unmarshalling json object")
		}

		if t.Kind != "Pod" {
			continue
		}

		pod := v1.Pod{}
		err = json.Unmarshal(b, &pod)
		if err != nil {
			return ret, utils.WrapErr(err, "Error unmarshalling json into pod object")
		}

		ret = append(ret, pod)
	}

	return ret, nil
}

func validatePod(p v1.Pod) error {
	for _, container := range p.Spec.Containers {
		if container.Name == p.ObjectMeta.Name {
			return errors.New("pod and container within pod cannot share same name for Podman v3")
		}
	}
	return nil
}
