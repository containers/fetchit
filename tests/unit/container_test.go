package unit

import (
	"testing"

	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// TestSpecGeneratorCreation tests that SpecGenerator can be created with Podman v5 API
func TestSpecGeneratorCreation(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	if s == nil {
		t.Fatal("Failed to create SpecGenerator")
	}

	if s.Image != image {
		t.Fatalf("Expected image %s, got %s", image, s.Image)
	}
}

// TestPrivilegedFieldPointer tests that Privileged field accepts *bool in Podman v5
func TestPrivilegedFieldPointer(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	privileged := true
	s.Privileged = &privileged

	if s.Privileged == nil {
		t.Fatal("Privileged field is nil after assignment")
	}

	if *s.Privileged != true {
		t.Fatal("Privileged field value is not true")
	}
}

// TestNamespaceConfiguration tests PidNS namespace configuration
func TestNamespaceConfiguration(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	if s.PidNS.NSMode != "host" {
		t.Fatalf("Expected PidNS mode 'host', got %s", s.PidNS.NSMode)
	}
}

// TestMountsConfiguration tests mounts array configuration
func TestMountsConfiguration(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	testMount := specs.Mount{
		Source:      "/tmp",
		Destination: "/data",
		Type:        "bind",
		Options:     []string{"rw"},
	}

	s.Mounts = []specs.Mount{testMount}

	if len(s.Mounts) != 1 {
		t.Fatalf("Expected 1 mount, got %d", len(s.Mounts))
	}

	if s.Mounts[0].Source != "/tmp" {
		t.Fatalf("Expected mount source /tmp, got %s", s.Mounts[0].Source)
	}
}

// TestNamedVolumesConfiguration tests named volumes configuration
func TestNamedVolumesConfiguration(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	vol := &specgen.NamedVolume{
		Name:    "fetchit-data",
		Dest:    "/opt",
		Options: []string{"rw"},
	}

	s.Volumes = []*specgen.NamedVolume{vol}

	if len(s.Volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(s.Volumes))
	}

	if s.Volumes[0].Name != "fetchit-data" {
		t.Fatalf("Expected volume name fetchit-data, got %s", s.Volumes[0].Name)
	}
}

// TestDeviceConfiguration tests device configuration
func TestDeviceConfiguration(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	device := specs.LinuxDevice{
		Path: "/dev/sda1",
	}

	s.Devices = []specs.LinuxDevice{device}

	if len(s.Devices) != 1 {
		t.Fatalf("Expected 1 device, got %d", len(s.Devices))
	}

	if s.Devices[0].Path != "/dev/sda1" {
		t.Fatalf("Expected device path /dev/sda1, got %s", s.Devices[0].Path)
	}
}

// TestCommandConfiguration tests command array configuration
func TestCommandConfiguration(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	cmd := []string{"sh", "-c", "echo hello"}
	s.Command = cmd

	if len(s.Command) != 3 {
		t.Fatalf("Expected 3 command parts, got %d", len(s.Command))
	}

	if s.Command[0] != "sh" {
		t.Fatalf("Expected first command 'sh', got %s", s.Command[0])
	}
}

// TestCapabilitiesConfiguration tests capability add/drop configuration
func TestCapabilitiesConfiguration(t *testing.T) {
	image := "quay.io/fetchit/fetchit:latest"
	s := specgen.NewSpecGenerator(image, false)

	s.CapAdd = []string{"NET_ADMIN", "SYS_TIME"}
	s.CapDrop = []string{"MKNOD"}

	if len(s.CapAdd) != 2 {
		t.Fatalf("Expected 2 capabilities to add, got %d", len(s.CapAdd))
	}

	if len(s.CapDrop) != 1 {
		t.Fatalf("Expected 1 capability to drop, got %d", len(s.CapDrop))
	}

	if s.CapAdd[0] != "NET_ADMIN" {
		t.Fatalf("Expected first cap add 'NET_ADMIN', got %s", s.CapAdd[0])
	}
}
