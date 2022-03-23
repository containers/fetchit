package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"

	"github.com/containers/podman/v4/pkg/bindings/play"
	"github.com/containers/podman/v4/pkg/bindings/pods"
	"github.com/redhat-et/harpoon/pkg/engine/utils"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	k8syaml "sigs.k8s.io/yaml"
)

func kubePodman(ctx context.Context, mo *FileMountOptions) error {
	if mo.Path != deleteFile {
		klog.Infof("Creating podman container from %s using kube method", mo.Path)
	}

	if mo.Previous != nil {
		err := stopPods(mo.Conn, []byte(*mo.Previous))
		if err != nil {
			return utils.WrapErr(err, "Error stopping pod")
		}
	}

	if mo.Path != deleteFile {
		kubeYaml, err := ioutil.ReadFile(mo.Path)
		if err != nil {
			return utils.WrapErr(err, "Error reading file")
		}
		err = stopPods(mo.Conn, kubeYaml)
		if err != nil {
			return utils.WrapErr(err, "Error stopping pod")
		}

		err = createPods(mo.Conn, mo.Path, kubeYaml)
		if err != nil {
			return utils.WrapErr(err, "Error creating pod")
		}
	}

	return nil
}

// This should be deprecated in favour of play.KubeDown() at some point
func stopPods(ctx context.Context, specs []byte) error {
	pod_list, err := podsToStop([]byte(specs))
	if err != nil {
		return utils.WrapErr(err, "Error getting list of pods to stop")
	}

	var pod_map = make(map[string]bool)

	for _, pod := range pod_list {
		pod_map[pod] = true
	}

	report, err := pods.List(ctx, nil)
	if err != nil {
		return utils.WrapErr(err, "Error getting list of pods from podman")
	}
	for _, p := range report {
		if _, ok := pod_map[p.Name]; ok {
			klog.Infof("Tearing down pod: %s\n", p.Name)
			err = tearDownPods(ctx, p.Name)
			if err != nil {
				return utils.WrapErr(err, "error tearing down pod %s", p.Name)
			}
		}
	}

	return nil
}

func tearDownPods(ctx context.Context, podName string) error {
	_, err := pods.Stop(ctx, podName, nil)
	if err != nil {
		return utils.WrapErr(err, "error stopping pod %s", podName)
	}
	_, err = pods.Remove(ctx, podName, nil)
	if err != nil {
		return utils.WrapErr(err, "error removing pod %s", podName)
	}

	return nil
}

func createPods(ctx context.Context, path string, specs []byte) error {
	pod_list, err := podFromBytes(specs)
	if err != nil {
		return utils.WrapErr(err, "error getting list of pods in spec")
	}

	for _, pod := range pod_list {
		err = validatePod(pod)
		if err != nil {
			return utils.WrapErr(err, "error validating pod spec")
		}
	}

	_, err = play.Kube(ctx, path, nil)
	if err != nil {
		return utils.WrapErr(err, "error playing kube spec")
	}

	klog.Infof("Created pods from spec in %s\n", path)
	return nil
}

func podsToStop(curr []byte) ([]string, error) {
	pod_list, err := podFromBytes(curr)
	if err != nil {
		return nil, utils.WrapErr(err, "error getting list of pods in spec")
	}

	ret := make([]string, 0)
	for _, pod := range pod_list {
		ret = append(ret, pod.ObjectMeta.Name)
	}

	return ret, nil
}

func podFromBytes(input []byte) ([]v1.Pod, error) {
	var i interface{}
	var t metav1.TypeMeta
	pod := v1.Pod{}
	d := yaml.NewDecoder(bytes.NewReader(input))
	ret := make([]v1.Pod, 0)
	for {
		err := d.Decode(&i)
		if err == io.EOF {
			break
		}
		if err != nil {
			return ret, utils.WrapErr(err, "error decoding yaml")
		}

		o, err := yaml.Marshal(i)
		if err != nil {
			return ret, utils.WrapErr(err, "error marshalling yaml into object for conversion to json")
		}

		b, err := k8syaml.YAMLToJSON(o)
		if err != nil {
			return ret, utils.WrapErr(err, "error converting yaml to json")
		}

		err = json.Unmarshal(b, &t)
		if err != nil {
			return ret, utils.WrapErr(err, "error unmarshalling json object")
		}

		if t.Kind != "Pod" {
			continue
		}

		err = json.Unmarshal(b, &pod)
		if err != nil {
			return ret, utils.WrapErr(err, "error unmarshalling json into pod object")
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
