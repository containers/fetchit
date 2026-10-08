package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings/network"
)

// Validate names before any workload teardown. Runtime failures remain possible
// if a network disappears between this check and Podman's creation call.
func validateNetworks(conn context.Context, networks []string) error {
	for _, name := range networks {
		if name == "" || strings.TrimSpace(name) != name {
			return fmt.Errorf("network names must be nonempty without surrounding whitespace")
		}
		if _, err := network.Inspect(conn, name, nil); err != nil {
			return fmt.Errorf("inspect configured network %q before deployment: %w", name, err)
		}
	}
	return nil
}
