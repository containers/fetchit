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
	"github.com/containers/podman/v4/pkg/domain/entities"
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
		err := stopPods(ctx, []byte(*mo.Previous))
		if err != nil {
			return utils.WrapErr(err, "Error stopping pods")
		}
	}

	if mo.Path != deleteFile {
		kubeYaml, err := ioutil.ReadFile(mo.Path)
		if err != nil {
			return utils.WrapErr(err, "Error reading file")
		}

		err = stopPods(ctx, kubeYaml)
		if err != nil {
			return utils.WrapErr(err, "Error stopping pods")
		}

		err = createPods(mo.Conn, mo.Path, kubeYaml)
		if err != nil {
			return utils.WrapErr(err, "Error creating pod")
		}
	}

	return nil
}

func stopPods(conn context.Context, input []byte) error {
	podList, err := podFromBytes(input)
	if err != nil {
		return utils.WrapErr(err, "Error getting list of pods in spec")
	}

	podNameList, err := getPodNames(podList)
	if err != nil {
		return utils.WrapErr(err, "Error getting list of pod names")
	}

	podMap := podMapFromList(podNameList)

	report, err := pods.List(conn, nil)
	if err != nil {
		return utils.WrapErr(err, "Error getting list of pods from podman")
	}

	runningPodNameList := reportToPodNameList(report)

	podsToBeDeleted := filterPods(runningPodNameList, podMap)

	err = tearDownPods(conn, podsToBeDeleted)
	if err != nil {
		return utils.WrapErr(err, "Error tearing down pods")
	}

	return nil
}

func podMapFromList(podNameList []string) map[string]bool {
	podMap := make(map[string]bool)
	for _, pod := range podNameList {
		podMap[pod] = true
	}
	return podMap
}

func reportToPodNameList(report []*entities.ListPodsReport) []string {
	ret := make([]string, 0)
	for _, r := range report {
		ret = append(ret, r.Name)
	}
	return ret
}

func filterPods(runningPodNameList []string, podMap map[string]bool) []string {
	ret := make([]string, 0)
	for _, p := range runningPodNameList {
		if _, ok := podMap[p]; ok {
			ret = append(ret, p)
		}
	}

	return ret
}

func tearDownPods(ctx context.Context, podNameList []string) error {
	for _, podName := range podNameList {
		_, err := pods.Stop(ctx, podName, nil)
		if err != nil {
			return utils.WrapErr(err, "Error stopping pod %s", podName)
		}
		_, err = pods.Remove(ctx, podName, nil)
		if err != nil {
			return utils.WrapErr(err, "Error removing pod %s", podName)
		}
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

func getPodNames(podList []v1.Pod) ([]string, error) {
	ret := make([]string, 0)

	for _, pod := range podList {
		if pod.ObjectMeta.Name != "" {
			ret = append(ret, pod.ObjectMeta.Name)
		} else {
			return nil, errors.New("pod has no name")
		}
	}

	return ret, nil
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
