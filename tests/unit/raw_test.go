package unit

import (
	"testing"

	"go.podman.io/common/libnetwork/types"
)

// TestPortMappingTypeCompatibility tests that PortMapping type is correctly imported from Podman v5
func TestPortMappingTypeCompatibility(t *testing.T) {
	pm := types.PortMapping{
		HostPort:      8080,
		ContainerPort: 80,
		Protocol:      "tcp",
	}

	if pm.HostPort != 8080 {
		t.Fatalf("Expected HostPort 8080, got %d", pm.HostPort)
	}

	if pm.ContainerPort != 80 {
		t.Fatalf("Expected ContainerPort 80, got %d", pm.ContainerPort)
	}

	if pm.Protocol != "tcp" {
		t.Fatalf("Expected Protocol 'tcp', got %s", pm.Protocol)
	}
}

// TestPortMappingArray tests array of PortMapping
func TestPortMappingArray(t *testing.T) {
	ports := []types.PortMapping{
		{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"},
		{HostPort: 8443, ContainerPort: 443, Protocol: "tcp"},
	}

	if len(ports) != 2 {
		t.Fatalf("Expected 2 port mappings, got %d", len(ports))
	}

	if ports[0].HostPort != 8080 {
		t.Fatalf("Expected first port mapping HostPort 8080, got %d", ports[0].HostPort)
	}

	if ports[1].HostPort != 8443 {
		t.Fatalf("Expected second port mapping HostPort 8443, got %d", ports[1].HostPort)
	}
}

// TestPortMappingWithHostIP tests PortMapping with HostIP
func TestPortMappingWithHostIP(t *testing.T) {
	pm := types.PortMapping{
		HostIP:        "0.0.0.0",
		HostPort:      8080,
		ContainerPort: 80,
		Protocol:      "tcp",
	}

	if pm.HostIP != "0.0.0.0" {
		t.Fatalf("Expected HostIP '0.0.0.0', got %s", pm.HostIP)
	}
}
