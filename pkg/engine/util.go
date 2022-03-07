package engine

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
		if err := kubePodman(path); err != nil {
			return err
		}
	case "ansible":
		if err := ansiblePodman(path); err != nil {
			return err
		}
	}
	return nil
}
