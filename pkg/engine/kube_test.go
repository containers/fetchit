package engine

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidatePod(t *testing.T) {
	var pod v1.Pod = v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "test1",
				},
				{
					Name: "test2",
				},
			},
		},
	}

	err := validatePod(pod)
	if err != nil {
		t.Errorf("expected no error, got %s", err)
	}

	pod.Spec.Containers[0].Name = pod.ObjectMeta.Name

	err = validatePod(pod)
	if err == nil {
		t.Errorf("expected error, got nil")
	}

}

var invalidYaml = `
apiVersion: v1
		kind: Pod
	metadata`

var notPodYaml = `
apiVersion: v1
kind: ConfigMap`

var podYaml = `
apiVersion: v1
kind: Pod
metadata:
  name: test_pod1
spec:
  containers:
  - name: test1
    image: test1
---
apiVersion: v1
kind: Pod
metadata:
  name: test_pod2
spec:
  containers:
  - name: test2
    image: test2`

var expectedPodList = []v1.Pod{
	{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test_pod1",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "test1",
					Image: "test1",
				},
			},
		},
	}, {
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test_pod2",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "test2",
					Image: "test2",
				},
			},
		},
	},
}

func comparePodList(t *testing.T, expected []v1.Pod, actual []v1.Pod) {
	m := make(map[string]int)

	if len(expected) != len(actual) {
		t.Errorf("expected %d pods, got %d", len(expected), len(actual))
	}

	for _, pod := range expected {
		pod_bytes, err := yaml.Marshal(pod)
		if err != nil {
			t.Errorf("error running test: %s", err)
		}
		if _, ok := m[string(pod_bytes)]; ok {
			m[string(pod_bytes)] += 1
		} else {
			m[string(pod_bytes)] = 1
		}
	}

	for _, pod := range actual {
		pod_bytes, err := yaml.Marshal(pod)
		if err != nil {
			t.Errorf("error running test: %s", err)
		}
		if _, ok := m[string(pod_bytes)]; !ok {
			t.Errorf("actual pod not in expected pods: %s", string(pod_bytes))
		}
		m[string(pod_bytes)] -= 1
		if m[string(pod_bytes)] == 0 {
			delete(m, string(pod_bytes))
		}
	}

	if len(m) != 0 {
		t.Errorf("missing actual pods from expected pods")
	}
}

func TestPodFromBytes(t *testing.T) {
	_, err := podFromBytes([]byte(invalidYaml))
	if err == nil {
		t.Error("expected error, got nothing")
	} else if !strings.Contains(err.Error(), "Error decoding yaml") {
		t.Errorf("expected error, got %s", err)
	}

	_, err = podFromBytes([]byte(notPodYaml))
	if err != nil {
		t.Errorf("expected no error, got %s", err)
	}

	actualPodList, err := podFromBytes([]byte(podYaml))
	if err != nil {
		t.Errorf("expected no error, got %s", err)
	}

	comparePodList(t, expectedPodList, actualPodList)
}
