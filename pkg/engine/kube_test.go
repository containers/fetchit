package engine

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/pods"
	"github.com/joho/godotenv"
)

func createSingleMethodObj(conn context.Context, url, branch string) SingleMethodObj {
	return SingleMethodObj{
		Conn:   conn,
		Method: kubeMethod,
		Target: &Target{},
	}
}

type testCaseSpec struct {
	Path             string
	ExpectedPodNames []string
	// ExpectedContainerNames []string
	// ExpectedVolumeNames []string
}

func removeParallel(conn context.Context, podList []string) error {
	ch := make(chan error)
	for _, pod := range podList {
		go func(ch chan<- error, conn context.Context, pod string) {
			_, err := pods.Remove(conn, pod, nil)
			ch <- err
		}(ch, conn, pod)
	}

	for range podList {
		err := <-ch
		if err != nil {
			return err
		}
	}

	return nil
}

func stopParallel(conn context.Context, podList []string) error {
	ch := make(chan error)
	for _, pod := range podList {
		go func(ch chan<- error, conn context.Context, pod string) {
			_, err := pods.Stop(conn, pod, nil)
			ch <- err
		}(ch, conn, pod)
	}

	for range podList {
		err := <-ch
		if err != nil {
			return err
		}
	}

	return nil
}

func cleanPodman(conn context.Context) error {
	report, err := pods.List(conn, nil)
	if err != nil {
		return err
	}

	podList := []string{}
	for _, rep := range report {
		podList = append(podList, rep.Name)
	}

	err = stopParallel(conn, podList)
	if err != nil {
		return err
	}

	err = removeParallel(conn, podList)
	if err != nil {
		return err
	}

	return nil
}

func TestKubePodman(t *testing.T) {
	testCases := []testCaseSpec{
		{
			Path: "../../examples/kube/2-example.yaml",
			ExpectedPodNames: []string{
				"colors_pod",
			},
		},
	}

	_ = godotenv.Load("../../test.env")

	ctx := context.Background()

	conn, err := bindings.NewConnection(ctx, "unix://run/user/1000/podman/podman.sock")
	if err != nil {
		t.Error(err)
	}

	url := os.Getenv("URL")
	branch := os.Getenv("BRANCH")

	smo := createSingleMethodObj(conn, url, branch)
	for _, testCase := range testCases {
		err := cleanPodman(conn)
		if err != nil {
			t.Error(err)
		}

		err = kubePodman(ctx, &smo, testCase.Path, nil)
		if err != nil {
			t.Error(err)
		}

		report, err := pods.List(conn, nil)
		if err != nil {
			t.Error(err)
		}

		pods := []string{}

		for _, rep := range report {
			pods = append(pods, rep.Name)
		}

		if len(pods) != len(testCase.ExpectedPodNames) {
			t.Errorf("Expected pods %v got %v", testCase.ExpectedPodNames, pods)
		}

		sort.Strings(pods)
		sort.Strings(testCase.ExpectedPodNames)

		for index, element := range testCase.ExpectedPodNames {
			if pods[index] != element {
				t.Errorf("Expected pods %v got %v", testCase.ExpectedPodNames, pods)
			}
		}
	}
}
