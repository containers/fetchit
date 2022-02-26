package engine

import (
	"fmt"
)

const (
	harpoonImage  = "quay.io/harpoon/harpoon:latest"
	harpoonVolume = "harpoon-volume"
)

func EngineMethod(path string, method string) error {
	switch method {
	case "raw":
		if err := RawPodman(path); err != nil {
			return err
		}
	case "systemd":
		if err := systemdPodman(path); err != nil {
			return err
		}
	case "kube":
		return fmt.Errorf("TBD")
	}
	return nil
}
