package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/cobra"
)

// Host receipts identify a destination and manager, never file contents or Git credentials.
type hostRemoval struct {
	Destination string `json:"destination"`
	Root        bool   `json:"root,omitempty"`
	Home        string `json:"home,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
}

type hostArtifactPlan struct {
	Receipt  removalReceipt `json:"receipt"`
	Action   string         `json:"action"`
	Source   string         `json:"source,omitempty"`
	Previous string         `json:"previous,omitempty"`
	Enable   bool           `json:"enable,omitempty"`
	Restart  bool           `json:"restart,omitempty"`
}

func init() {
	fetchitCmd.AddCommand(&cobra.Command{
		Use: "host-artifact PLAN", Hidden: true, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var plan hostArtifactPlan
			decoder := json.NewDecoder(strings.NewReader(args[0]))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&plan); err != nil {
				return err
			}
			return executeHostArtifactPlan(plan, "/host", "/", hostSystemctl)
		},
	})
}

func cleanHostPath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && p != "/" && !strings.ContainsAny(p, "\n\r\x00")
}
func validHostRemoval(kind string, h *hostRemoval) bool {
	if h == nil || !cleanHostPath(h.Destination) {
		return false
	}
	if kind == filetransferMethod {
		return !h.Root && h.Home == "" && h.Runtime == ""
	}
	if kind != systemdMethod {
		return false
	}
	if h.Root {
		return h.Destination == systemdPathRoot && h.Home == "" && h.Runtime == ""
	}
	return cleanHostPath(h.Home) && cleanHostPath(h.Runtime) && h.Destination == filepath.Join(h.Home, ".config", "systemd", "user")
}

func hostMethodReceipt(m Method) (removalReceipt, bool, error) {
	var common *CommonMethod
	h := &hostRemoval{}
	switch method := m.(type) {
	case *FileTransfer:
		common = &method.CommonMethod
		h.Destination = method.DestinationDirectory
	case *Systemd:
		common = &method.CommonMethod
		if method.autoUpdateAll {
			if common.CleanupOnRemoval {
				return removalReceipt{}, false, errors.New("cleanupOnRemoval is unsupported for automatic Podman update services")
			}
			return removalReceipt{}, false, nil
		}
		h.Root = method.Root
		if h.Root {
			h.Destination = systemdPathRoot
		} else {
			h.Home, h.Runtime = os.Getenv("HOME"), os.Getenv("XDG_RUNTIME_DIR")
			h.Destination = filepath.Join(h.Home, ".config", "systemd", "user")
		}
	default:
		return removalReceipt{}, false, errors.New("unsupported host artifact method")
	}
	// Preserve validation and behavior of configurations which have not opted in.
	if !validHostRemoval(m.GetKind(), h) {
		if !common.CleanupOnRemoval {
			return removalReceipt{}, false, nil
		}
		return removalReceipt{}, false, errors.New("cleanupOnRemoval requires canonical absolute destination paths; user Systemd also requires HOME and XDG_RUNTIME_DIR")
	}
	owner := kubeOwner(common.workloadLabels()[kubeOwnerLabel], m.GetKind(), h.Destination, fmt.Sprintf("%t/%s/%s", h.Root, h.Home, h.Runtime))
	return removalReceipt{Kind: m.GetKind(), Owner: owner, Name: m.GetName(), Host: h}, common.CleanupOnRemoval, nil
}

func deployHostArtifact(conn context.Context, p hostArtifactPlan) (result error) {
	if !validRemovalReceipt(p.Receipt) || p.Receipt.Host == nil {
		return errors.New("invalid host artifact identity")
	}
	if err := detectOrFetchImage(conn, fetchitImage, false); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	h := p.Receipt.Host
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.CapDrop = []string{"ALL"}
	s.CapAdd = []string{"DAC_OVERRIDE", "FOWNER"}
	s.SelinuxOpts = []string{"disable"}
	s.NetNS = specgen.Namespace{NSMode: specgen.NoNetwork}
	s.Entrypoint = []string{"/usr/local/bin/fetchit"}
	s.Command = []string{"host-artifact", string(data)}
	s.Mounts = []specs.Mount{
		{Source: "/", Destination: "/host", Type: "bind", Options: []string{"ro", "rbind", "rslave"}},
		{Source: h.Destination, Destination: "/host" + h.Destination, Type: "bind", Options: []string{"rw"}},
	}
	if p.Receipt.Kind == systemdMethod {
		s.CapAdd = append(s.CapAdd, "SYS_CHROOT")
		s.PidNS = specgen.Namespace{NSMode: specgen.Host}
	}
	if p.Action == "apply" {
		s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"ro"}}}
	}
	created, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, removeHelperContainer(conn, created.ID)) }()
	if err := containers.Start(conn, created.ID, nil); err != nil {
		return err
	}
	code, err := containers.Wait(conn, created.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{define.ContainerStateExited}))
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	// Drain diagnostics before deleting the helper; never retain file contents in state.
	logs := make(chan string)
	done := make(chan struct{})
	var diagnostics strings.Builder
	go func() {
		defer close(done)
		for line := range logs {
			if remaining := 4096 - diagnostics.Len(); remaining > 0 {
				if len(line) > remaining {
					line = line[:remaining]
				}
				diagnostics.WriteString(line)
			}
		}
	}()
	logErr := containers.Logs(conn, created.ID, new(containers.LogOptions).WithStdout(false).WithStderr(true), nil, logs)
	close(logs)
	<-done
	return errors.Join(fmt.Errorf("host artifact helper exited with status %d: %s", code, strings.TrimSpace(diagnostics.String())), logErr)
}
