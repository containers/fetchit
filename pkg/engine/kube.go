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
	"k8s.io/klog/v2"
	k8syaml "sigs.k8s.io/yaml"
)

const kubeMethod = "kube"

// Kube to launch pods using podman kube-play
type Kube struct {
	// Name must be unique within target Kube methods
	Name string `mapstructure:"name"`
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// initialRun is set by fetchit
	initialRun bool
}

func (k *Kube) Type() string {
	return kubeMethod
}

func (k *Kube) GetName() string {
	return k.Name
}

func (k *Kube) SchedInfo() SchedInfo {
	return SchedInfo{
		Schedule: k.Schedule,
		Skew:     k.Skew,
	}
}

func (k *Kube) Process(ctx, conn context.Context, target *Target, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	initial := k.initialRun
	tag := []string{"yaml", "yml"}
	if initial {
		err := getClone(target, PAT)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.Url, target.Name, err)
			return
		}
	}

	latest, err := getLatest(target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := getCurrent(target, kubeMethod, k.Name)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = k.Apply(ctx, conn, target, current, latest, k.TargetPath, &tag)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		updateCurrent(ctx, target, latest, kubeMethod, k.Name)
		klog.Infof("Moved kube from %s to %s for target %s", current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, kube currently at %s", target.Name, current)
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

func (k *Kube) Apply(ctx, conn context.Context, target *Target, currentState, desiredState plumbing.Hash, targetPath string, tags *[]string) error {
	changeMap, err := applyChanges(ctx, target, currentState, desiredState, targetPath, tags)
	if err != nil {
		return err
	}
	if err := k.runChangesConcurrent(ctx, conn, changeMap); err != nil {
		return err
	}
	return nil
}

func (k *Kube) runChangesConcurrent(ctx context.Context, conn context.Context, changeMap map[*object.Change]string) error {
	ch := make(chan error)
	for change, changePath := range changeMap {
		go func(ch chan<- error, changePath string, change *object.Change) {
			if err := k.MethodEngine(ctx, conn, change, changePath); err != nil {
				ch <- utils.WrapErr(err, "error running engine method for change from: %s to %s", change.From.Name, change.To.Name)
			}
			ch <- nil
		}(ch, changePath, change)
	}
	for range changeMap {
		err := <-ch
		if err != nil {
			return err
		}
	}
	return nil
}

func (k *Kube) kubePodman(ctx, conn context.Context, path string, prev *string) error {
	if path != deleteFile {
		klog.Infof("Creating podman container from %s using kube method", path)
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
