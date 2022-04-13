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

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/play"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/redhat-et/harpoon/pkg/engine/utils"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	k8syaml "sigs.k8s.io/yaml"
)

func kubePodman(ctx context.Context, mo *SingleMethodObj, path string, prev *string) error {

	if path != deleteFile {
		klog.Infof("Creating podman container from %s using kube method", path)
	}

	if prev != nil {
		err := stopPods(mo.Conn, []byte(*prev))
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
		err = stopPods(mo.Conn, kubeYaml)
		if err != nil {
			if !strings.Contains(err.Error(), "no such pod") {
				return utils.WrapErr(err, "Error stopping pods")
			}
		}

		err = createPods(mo.Conn, path, kubeYaml)
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

	klog.Infof("Created pods from spec in %s\n", path)
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
