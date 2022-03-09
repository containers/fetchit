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
	"k8s.io/klog/v2"
	k8syaml "sigs.k8s.io/yaml"
)

type YamlMeta struct {
	ApiVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

func kubePodman(ctx context.Context, path string) error {
	klog.Infof("Creating podman container from %s using kube method", path)

	kubeYaml, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	var i interface{}
	var pod = v1.Pod{}
	d := yaml.NewDecoder(bytes.NewReader(kubeYaml))

	pod_map := make(map[string]bool)

	for {
		err = d.Decode(&i)
		if err == io.EOF {
			break
		}
		if _, ok := err.(*yaml.TypeError); ok {
			continue
		}
		if err != nil {
			return err
		}

		o, err := yaml.Marshal(i)
		if err != nil {
			return err
		}

		b, err := k8syaml.YAMLToJSON(o)
		if err != nil {
			return err
		}

		err = json.Unmarshal(b, &pod)
		if err != nil {
			return err
		}

		for _, container := range pod.Spec.Containers {
			if container.Name == pod.ObjectMeta.Name {
				return errors.New("pod and container within pod cannot share same name for Podman v3")
			}
		}
		pod_map[pod.ObjectMeta.Name] = true
	}
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		return err
	}

	report, err := pods.List(conn, nil)
	if err != nil {
		return err
	}

	for _, r := range report {
		if _, ok := pod_map[r.Name]; ok {
			_, err = pods.Stop(conn, r.Name, nil)
			if err != nil {
				return err
			}
			_, err = pods.Remove(conn, r.Name, nil)
			if err != nil {
				return err
			}
		}
	}

	_, err = play.Kube(conn, path, nil)
	if err != nil {
		return err
	}
	klog.Infof("Played kube successfully!")
	return nil
}
