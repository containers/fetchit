package engine

import (
	"context"
	"io/ioutil"

	"github.com/containers/podman/v4/pkg/bindings/containers"

	"k8s.io/klog/v2"
)

/* below is an example.json file:
{"Image":"docker.io/mmumshad/simple-webapp-color:latest",
"Name": "colors",
"Env": {"color": "blue", "tree": "trunk"},
"Ports": [{
    "HostIP":        "",
    "ContainerPort": 8080,
    "HostPort":      8080,
    "Range":         0,
    "Protocol":      ""}]
"CapAdd": []
"CapDrop": []
}
*/

type port struct {
	HostIP        string `json:"host_ip" yaml:"host_ip"`
	ContainerPort uint16 `json:"container_port" yaml:"container_port"`
	HostPort      uint16 `json:"host_port" yaml:"host_port"`
	Range         uint16 `json:"range" yaml:"range"`
	Protocol      string `json:"protocol" yaml:"protocol"`
}

type mount struct {
	Destination string   `json:"destination" yaml:"destination"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty" platform:"linux,solaris,zos"`
	Source      string   `json:"source,omitempty" yaml:"source,omitempty"`
	Options     []string `json:"options,omitempty" yaml:"options,omitempty"`
}

type namedVolume struct {
	Name    string   `json:"name" yaml:"name"`
	Dest    string   `json:"dest" yaml:"dest"`
	Options []string `json:"options" yaml:"options"`
}

type RawPod struct {
	Image   string            `json:"Image" yaml:"Image"`
	Name    string            `json:"Name" yaml:"Name"`
	Env     map[string]string `json:"Env" yaml:"Env"`
	Ports   []port            `json:"Ports" yaml:"Ports"`
	Mounts  []mount           `json:"Mounts" yaml:"Mounts"`
	Volumes []namedVolume     `json:"Volumes" yaml:"Volumes"`
	CapAdd  []string          `json:"CapAdd" yaml:"CapAdd"`
	CapDrop []string          `json:"CapDrop" yaml:"CapDrop"`
}

func rawPodman(ctx context.Context, mo *SingleMethodObj, path string, prev *string) error {

	klog.Infof("Creating podman container from %s", path)

	rawFile, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	raw, err := rawPodFromBytes(rawFile)
	if err != nil {
		return err
	}

	klog.Infof("Identifying if image exists locally")

	err = detectOrFetchImage(mo.Conn, raw.Image, mo.Target.Methods.Raw.PullImage)
	if err != nil {
		return err
	}

	// Delete previous file's podxz
	if prev != nil {
		raw, err := rawPodFromBytes([]byte(*prev))
		if err != nil {
			return err
		}

		err = deleteContainer(mo.Conn, raw.Name)
		if err != nil {
			return err
		}

		klog.Infof("Deleted podman container %s", raw.Name)
	}

	if path == deleteFile {
		return nil
	}

	err = removeExisting(mo.Conn, raw.Name)
	if err != nil {
		return err
	}

	s := createSpecGen(*raw)

	createResponse, err := containers.CreateWithSpec(mo.Conn, s, nil)
	if err != nil {
		return err
	}
	klog.Infof("Container %s created.", s.Name)

	if err := containers.Start(mo.Conn, createResponse.ID, nil); err != nil {
		return err
	}
	klog.Infof("Container %s started....Requeuing", s.Name)

	return nil
}
