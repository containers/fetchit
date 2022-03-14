package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/play"
	"github.com/containers/podman/v4/pkg/bindings/pods"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	k8syaml "sigs.k8s.io/yaml"
)

func kubePodman(ctx context.Context, path string, prev *string) error {
	if path != "" {
		klog.Infof("Creating podman container from %s using kube method", path)
	}
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		return err
	}

	if prev != nil {
		err = stopPods(conn, []byte(*prev))
		if err != nil {
			return err
		}
	}

	if path != "" {
		kubeYaml, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		err = stopPods(conn, kubeYaml)
		if err != nil {
			return err
		}

		err = createPods(conn, path, kubeYaml)
		if err != nil {
			return err
		}
	}

	return nil
}

func stopPods(ctx context.Context, specs []byte) error {
	pod_list, err := podsToStop([]byte(specs))
	if err != nil {
		return err
	}

	var pod_map = make(map[string]bool)

	for _, pod := range pod_list {
		pod_map[pod] = true
	}

	report, err := pods.List(ctx, nil)
	if err != nil {
		return err
	}
	for _, p := range report {
		if _, ok := pod_map[p.Name]; ok {
			klog.Infof("Tearing down pod: %s\n", p.Name)
			err = tearDownPods(ctx, p.Name)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func tearDownPods(ctx context.Context, podName string) error {
	_, err := pods.Stop(ctx, podName, nil)
	if err != nil {
		return err
	}
	_, err = pods.Remove(ctx, podName, nil)
	if err != nil {
		return err
	}

	return nil
}

func createPods(ctx context.Context, path string, specs []byte) error {
	pod_list, err := podFromBytes(specs)
	if err != nil {
		return err
	}

	for _, pod := range pod_list {
		err = validatePod(pod)
		if err != nil {
			return err
		}
	}

	_, err = play.Kube(ctx, path, nil)
	if err != nil {
		return err
	}

	klog.Infof("Created pods from spec in %s\n", path)
	return nil
}

func podsToStop(curr []byte) ([]string, error) {
	pod_list, err := podFromBytes(curr)
	ret := make([]string, 0)
	if err != nil {
		return nil, err
	}
	for _, pod := range pod_list {
		ret = append(ret, pod.ObjectMeta.Name)
	}
	return ret, nil
}

func podFromBytes(input []byte) ([]v1.Pod, error) {
	ret := make([]v1.Pod, 0)
	var i interface{}
	var t metav1.TypeMeta
	pod := v1.Pod{}
	d := yaml.NewDecoder(bytes.NewReader(input))
	for {
		err := d.Decode(&i)
		if err == io.EOF {
			break
		}
		if err != nil {
			return ret, err
		}

		o, err := yaml.Marshal(i)
		if err != nil {
			return ret, err
		}

		b, err := k8syaml.YAMLToJSON(o)
		if err != nil {
			return ret, err
		}

		err = json.Unmarshal(b, &t)
		if err != nil {
			return ret, err
		}

		if t.Kind != "Pod" {
			continue
		}

		err = json.Unmarshal(b, &pod)
		if err != nil {
			return ret, err
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
