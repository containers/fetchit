package engine

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"github.com/go-git/go-git/v5"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/containers/podman/v5/pkg/systemd/parser"
	"github.com/containers/podman/v5/pkg/systemd/quadlet"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const quadletMethod = "quadlet"

//go:embed quadlet-host.sh
var quadletHostScript string

// Quadlet deploys a dedicated Git directory as a host-managed Quadlet bundle.
// Rootless deployments require explicit host paths; container HOME/UID are not used.
type Quadlet struct {
	CommonMethod   `mapstructure:",squash"`
	Root           bool   `mapstructure:"root"`
	Start          bool   `mapstructure:"start"`
	Restart        bool   `mapstructure:"restart"`
	HostHome       string `mapstructure:"hostHome"`
	HostConfigHome string `mapstructure:"hostConfigHome"`
	HostRuntimeDir string `mapstructure:"hostRuntimeDir"`
	HelperImage    string `mapstructure:"helperImage"`
	runHost        func(context.Context, context.Context, quadletPlan) error
}

type quadletBundle struct {
	files           map[string][]byte
	modes           map[string]int64
	services        []string
	restartServices []string
}

type quadletPlan struct {
	previous  quadletBundle
	desired   quadletBundle
	parent    string
	runtime   string
	namespace string
	home      string
}

var quadletSafeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:@-]*$`)

func (q *Quadlet) GetKind() string { return quadletMethod }

func (q *Quadlet) Process(ctx, conn context.Context, skew int) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(skew) * time.Millisecond):
	}
	target := q.GetTarget()
	target.mu.Lock()
	defer target.mu.Unlock()
	// Unlike legacy methods, do not replay the saved commit on startup: the host
	// files survive Fetchit restarts, and the saved commit is the cleanup baseline.
	if q.initialRun {
		if err := getRepo(target); err != nil {
			logger.Errorf("Quadlet clone: %v", err)
			return
		}
	}
	if err := currentToLatest(ctx, conn, q, target, nil); err != nil {
		logger.Errorf("Quadlet %s: %v", q.Name, err)
		return
	}
	q.initialRun = false
}

// Quadlet operations are deliberately batched, never dispatched per file.
func (q *Quadlet) MethodEngine(context.Context, context.Context, *object.Change, string) error {
	return fmt.Errorf("Quadlet requires bundle reconciliation through Apply")
}

func (q *Quadlet) paths() (string, string, error) {
	if !quadletSafeName.MatchString(q.Name) || strings.Contains(q.Name, "@") || strings.Contains(q.Name, ":") {
		return "", "", fmt.Errorf("invalid Quadlet method name %q", q.Name)
	}
	if q.TargetPath == "" || path.Clean(q.TargetPath) != q.TargetPath || path.IsAbs(q.TargetPath) || strings.HasPrefix(q.TargetPath, "..") {
		return "", "", fmt.Errorf("Quadlet requires a relative targetPath directory")
	}
	if q.Glob != nil {
		return "", "", fmt.Errorf("Quadlet glob is unsupported: targetPath must select a complete bundle directory")
	}
	if q.Root {
		return "/etc/containers", "", nil
	}
	for _, p := range []string{q.HostHome, q.HostConfigHome, q.HostRuntimeDir} {
		if !path.IsAbs(p) || path.Clean(p) != p || p == "/" || strings.ContainsAny(p, "\n\r") {
			return "", "", fmt.Errorf("rootless Quadlet requires absolute hostHome, hostConfigHome and hostRuntimeDir paths")
		}
	}
	return q.HostConfigHome, q.HostRuntimeDir, nil
}

func (q *Quadlet) bundle(hash plumbing.Hash) (quadletBundle, error) {
	b := quadletBundle{files: map[string][]byte{}, modes: map[string]int64{}}
	if hash.IsZero() {
		return b, nil
	}
	repo, err := git.PlainOpen(getDirectory(q.target))
	if err != nil {
		return b, err
	}
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return b, err
	}
	root, err := commit.Tree()
	if err != nil {
		return b, err
	}
	tree := root
	if q.TargetPath != "." {
		tree, err = root.Tree(q.TargetPath)
	}
	if errors.Is(err, object.ErrDirectoryNotFound) {
		return b, nil
	}
	if err != nil {
		return b, err
	}

	err = tree.Files().ForEach(func(f *object.File) error {
		if f.Mode != filemode.Regular && f.Mode != filemode.Executable {
			return fmt.Errorf("Quadlet bundle requires regular files: %s", f.Name)
		}
		if path.Clean(f.Name) != f.Name || path.IsAbs(f.Name) || strings.HasPrefix(f.Name, "../") || strings.ContainsAny(f.Name, "\n\r") {
			return fmt.Errorf("invalid bundle path %q", f.Name)
		}
		content, err := f.Contents()
		if err != nil {
			return err
		}
		b.files[f.Name] = []byte(content)
		b.modes[f.Name] = 0644
		if f.Mode == filemode.Executable {
			b.modes[f.Name] = 0755
		}
		return nil
	})
	if err != nil {
		return b, err
	}
	names := map[string]string{}
	for filename, content := range b.files {
		if !quadlet.IsExtSupported(filename) {
			continue
		}
		if path.Dir(filename) != "." {
			return b, fmt.Errorf("Quadlet source units must be at bundle root: %s", filename)
		}
		if !quadletSafeName.MatchString(strings.TrimSuffix(filename, path.Ext(filename))) {
			return b, fmt.Errorf("invalid Quadlet filename %q", filename)
		}
		u := parser.NewUnitFile()
		u.Filename = filename
		if err := u.Parse(string(content)); err != nil {
			return b, fmt.Errorf("parse %s: %w", filename, err)
		}
		// Match upstream drop-in specificity and alphabetical merge order.
		dropins := map[string][]byte{}
		for _, dir := range u.GetUnitDropinPaths() {
			for name, data := range b.files {
				if path.Dir(name) == dir && path.Ext(name) == ".conf" {
					if _, exists := dropins[path.Base(name)]; !exists {
						dropins[path.Base(name)] = data
					}
				}
			}
		}
		keys := make([]string, 0, len(dropins))
		for name := range dropins {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			drop := parser.NewUnitFile()
			if err := drop.Parse(string(dropins[name])); err != nil {
				return b, fmt.Errorf("parse drop-in %s: %w", name, err)
			}
			u.Merge(drop)
		}
		name, err := quadlet.GetUnitServiceName(u)
		if err != nil {
			return b, err
		}
		if !quadletSafeName.MatchString(name) || strings.HasSuffix(name, "@") {
			return b, fmt.Errorf("invalid or uninstantiated Quadlet service %q", name)
		}
		name += ".service"
		if prev, exists := names[name]; exists {
			return b, fmt.Errorf("duplicate service %s in %s and %s", name, prev, filename)
		}
		names[name] = filename
		b.services = append(b.services, name)
		if path.Ext(filename) != ".network" && path.Ext(filename) != ".volume" {
			b.restartServices = append(b.restartServices, name)
		}
	}

	sort.Strings(b.services)
	sort.Strings(b.restartServices)
	return b, err
}

func (q *Quadlet) Apply(ctx, conn context.Context, current, desired plumbing.Hash, _ *[]string) error {
	if desired.IsZero() {
		return fmt.Errorf("Quadlet desired commit is empty")
	}
	parent, runtime, err := q.paths()
	if err != nil {
		return err
	}
	old, err := q.bundle(current)
	if err != nil {
		return err
	}
	next, err := q.bundle(desired)
	if err != nil {
		return err
	}
	if len(next.services) == 0 && (current.IsZero() || (len(next.files) > 0 && len(old.services) == 0)) {
		return fmt.Errorf("Quadlet bundle contains no supported source units")
	}
	if current == desired {
		return nil
	}

	sum := sha256.Sum256([]byte(q.target.url + "\x00" + q.Name))
	home := q.HostHome
	if q.Root {
		home = "/root"
	}
	plan := quadletPlan{previous: old, desired: next, parent: parent, runtime: runtime, namespace: fmt.Sprintf("fetchit-%s-%x", q.Name, sum[:6]), home: home}
	run := q.runHost
	if run == nil {
		run = q.deploy
	}
	return run(ctx, conn, plan)
}

func quadletArchive(files map[string][]byte, modes map[string]int64) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "quadlet-bundle/", Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: "quadlet-bundle/" + name, Mode: modes[name], Size: int64(len(data))}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (q *Quadlet) deploy(ctx, conn context.Context, p quadletPlan) error {
	archive, err := quadletArchive(p.desired.files, p.desired.modes)
	if err != nil {
		return err
	}
	image := q.HelperImage
	if image == "" {
		image = fetchitImage
	}
	if err := detectOrFetchImage(conn, image, false); err != nil {
		return err
	}
	s := specgen.NewSpecGenerator(image, false)
	privileged := true
	s.Privileged = &privileged
	s.PidNS = specgen.Namespace{NSMode: "host"}
	// Host binaries/generator and their libraries remain read-only. Only the
	// containers/config directory is writable; systemd uses its private socket.
	s.Mounts = []specs.Mount{
		{Source: "/", Destination: "/host", Type: define.TypeBind, Options: []string{"ro", "rbind", "rslave"}},
		{Source: p.parent, Destination: "/host" + p.parent, Type: define.TypeBind, Options: []string{"rw"}},
	}
	s.Entrypoint = []string{"/bin/sh"}
	restartUnits := ""
	if q.Restart {
		restartUnits = strings.Join(p.desired.restartServices, "\n")
	}
	s.Command = []string{"-ceu", quadletHostScript, "quadlet", p.parent, p.runtime, p.namespace, strings.Join(p.previous.services, "\n"), strings.Join(p.desired.services, "\n"), fmt.Sprint(q.Start || q.Restart), restartUnits}
	s.Env = map[string]string{"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "HOME": p.home}
	if p.runtime != "" {
		s.Env["XDG_CONFIG_HOME"] = p.parent
		s.Env["XDG_RUNTIME_DIR"] = p.runtime
	}
	created, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	defer func() {
		if _, err := containers.Remove(conn, created.ID, new(containers.RemoveOptions).WithForce(true)); err != nil {
			logger.Warnf("Remove Quadlet helper: %v", err)
		}
	}()
	copyBundle, err := containers.CopyFromArchive(conn, created.ID, "/tmp", bytes.NewReader(archive))
	if err != nil {
		return err
	}
	if err := copyBundle(); err != nil {
		return err
	}

	if err := containers.Start(conn, created.ID, nil); err != nil {
		return err
	}
	code, err := containers.Wait(conn, created.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{define.ContainerStateExited}))
	if err != nil {
		return err
	}
	// Consume logs concurrently so generator diagnostics cannot block the API reader.
	logs := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for line := range logs {
			logger.Infof("Quadlet %s: %s", q.Name, strings.TrimSpace(line))
		}
	}()
	logErr := containers.Logs(conn, created.ID, new(containers.LogOptions).WithStdout(true).WithStderr(true), logs, logs)
	close(logs)
	<-done
	if code != 0 {
		return fmt.Errorf("Quadlet helper exited with status %d (see generator/systemd diagnostics)", code)
	}
	return logErr
}
