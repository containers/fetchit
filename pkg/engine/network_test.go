package engine

import (
	"testing"

	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/spf13/viper"
	"strings"
)

func TestOptionalNetworks(t *testing.T) {
	for _, input := range []string{
		`{"Image":"example.invalid/image", "Networks":["frontend","backend"]}`,
		"Image: example.invalid/image\nNetworks: [frontend, backend]\n",
	} {
		raw, err := rawPodFromBytes([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		spec := createSpecGen(*raw)
		if spec.NetNS.NSMode != specgen.Bridge || len(spec.Networks) != 2 {
			t.Fatalf("wrong custom networking: %+v", spec.ContainerNetworkConfig)
		}
		for _, name := range []string{"frontend", "backend"} {
			if _, ok := spec.Networks[name]; !ok {
				t.Fatalf("missing network %s", name)
			}
		}
	}
	spec := createSpecGen(RawPod{Image: "example.invalid/image"})
	if spec.Networks != nil || spec.NetNS != specgen.NewSpecGenerator("example.invalid/image", false).NetNS {
		t.Fatal("default networking changed")
	}
	for _, networks := range [][]string{nil, {}} {
		if kubeNetworkOptions(networks) != nil {
			t.Fatal("empty networks override Podman defaults")
		}
	}
	options := kubeNetworkOptions([]string{"frontend", "backend"})
	if len(options.GetNetwork()) != 2 || options.GetNetwork()[1] != "backend" {
		t.Fatal("kube network list lost")
	}
}

func TestNetworkConfigurationDecode(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("targetConfigs:\n- url: https://example.com/repo\n  raw:\n  - name: raw\n    networks: [frontend, backend]\n  kube:\n  - name: kube\n    networks: [backend]\n")); err != nil {
		t.Fatal(err)
	}
	var config FetchitConfig
	if err := v.UnmarshalExact(&config); err != nil {
		t.Fatal(err)
	}
	if len(config.TargetConfigs[0].Raw[0].Networks) != 2 || config.TargetConfigs[0].Kube[0].Networks[0] != "backend" {
		t.Fatal("network configuration not decoded")
	}
}
