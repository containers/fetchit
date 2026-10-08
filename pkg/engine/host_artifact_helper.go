package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const artifactJournalDirectory = ".fetchit-artifacts"

type artifactFingerprint struct {
	Digest string `json:"digest"`
	Mode   uint32 `json:"mode"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	UID    uint32 `json:"uid"`
	GID    uint32 `json:"gid"`
}
type artifactRecord struct {
	Owner   string               `json:"owner"`
	Kind    string               `json:"kind"`
	Current *artifactFingerprint `json:"current,omitempty"`
	Pending *artifactFingerprint `json:"pending,omitempty"`
	Stage   string               `json:"stage,omitempty"`
}
type artifactJournal struct {
	Version int                       `json:"version"`
	Files   map[string]artifactRecord `json:"files"`
}

type artifactControl func(host string, h *hostRemoval, args ...string) (string, error)

func hostSystemctl(host string, h *hostRemoval, args ...string) (string, error) {
	command := []string{host, "/usr/bin/env", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	if !h.Root {
		command = append(command, "HOME="+h.Home, "XDG_RUNTIME_DIR="+h.Runtime)
	}
	command = append(command, "systemctl")
	if !h.Root {
		command = append(command, "--user")
	}
	out, err := exec.Command("chroot", append(command, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("host systemctl %v: %w: %s", args, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func validArtifactName(name, kind string) bool {
	if name == "" || name == "." || name == ".." || name == artifactJournalDirectory || filepath.Base(name) != name || strings.ContainsAny(name, "\x00\n\r") {
		return false
	}
	if kind == systemdMethod {
		return quadletSafeName.MatchString(name) && strings.HasSuffix(name, ".service") && !strings.HasPrefix(name, "-") && name != ".service"
	}
	return kind == filetransferMethod
}

// Validate every destination component, rather than following a host symlink.
func artifactDestination(host, dest string) (string, error) {
	current := host
	for _, part := range strings.Split(strings.TrimPrefix(dest, "/"), "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("artifact destination is not a real directory: %s", current)
		}
	}
	return current, nil
}

func fingerprintArtifact(path string) (*artifactFingerprint, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file: %s", path)
	}
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(descriptor), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("artifact changed while opening")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return nil, err
	}
	stat := opened.Sys().(*syscall.Stat_t)
	return &artifactFingerprint{Digest: hex.EncodeToString(digest.Sum(nil)), Mode: uint32(opened.Mode().Perm()), Device: uint64(stat.Dev), Inode: uint64(stat.Ino), UID: stat.Uid, GID: stat.Gid}, nil
}
func sameFingerprint(a, b *artifactFingerprint) bool { return a != nil && b != nil && *a == *b }

func validArtifactRecord(name string, record artifactRecord) bool {
	if !validArtifactName(name, record.Kind) || !validRemovalReceipt(removalReceipt{Kind: rawMethod, Owner: record.Owner}) {
		return false
	}
	for _, fingerprint := range []*artifactFingerprint{record.Current, record.Pending} {
		if fingerprint == nil {
			continue
		}
		digest, err := hex.DecodeString(fingerprint.Digest)
		if err != nil || len(digest) != sha256.Size || fingerprint.Mode > 0777 {
			return false
		}
	}
	if record.Current == nil && record.Pending == nil {
		return false
	}
	if record.Pending == nil {
		return record.Stage == ""
	}
	return strings.HasPrefix(record.Stage, ".stage-") && filepath.Base(record.Stage) == record.Stage && !strings.ContainsAny(record.Stage, "\n\r\x00")
}

func loadArtifactJournal(path string) (artifactJournal, error) {
	j := artifactJournal{Version: 1, Files: map[string]artifactRecord{}}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return j, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) || info.Size() > 1<<20 {
		return j, errors.New("unsafe artifact journal")
	}
	f, err := os.Open(path)
	if err != nil {
		return j, err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&j); err != nil {
		return j, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || j.Version != 1 || j.Files == nil {
		return j, errors.New("invalid artifact journal")
	}
	for name, record := range j.Files {
		if !validArtifactRecord(name, record) {
			return j, errors.New("invalid artifact ownership record")
		}
	}
	return j, nil
}

func saveArtifactJournal(path string, j artifactJournal) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("artifact journal exceeds 1 MiB")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".journal-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncArtifactDirectory(filepath.Dir(path))
}
func syncArtifactDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func executeHostArtifactPlan(plan hostArtifactPlan, host, sourceRoot string, control artifactControl) error {
	r := plan.Receipt
	if !validRemovalReceipt(r) || r.Host == nil {
		return errors.New("invalid host artifact receipt")
	}
	if plan.Action != "apply" && plan.Action != "delete" && plan.Action != "cleanup" {
		return errors.New("invalid host artifact action")
	}
	if plan.Previous != "" && !validArtifactName(plan.Previous, r.Kind) {
		return errors.New("invalid previous artifact name")
	}
	if plan.Action == "apply" && (!cleanHostPath(plan.Source) || !strings.HasPrefix(plan.Source, "/opt/") || !validArtifactName(filepath.Base(plan.Source), r.Kind)) {
		return errors.New("invalid artifact source")
	}
	dest, err := artifactDestination(host, r.Host.Destination)
	if errors.Is(err, os.ErrNotExist) && plan.Action == "cleanup" {
		return nil
	}
	if err != nil {
		return err
	}
	journalDir := filepath.Join(dest, artifactJournalDirectory)
	if plan.Action != "apply" {
		if _, err := os.Lstat(journalDir); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	if err := os.Mkdir(journalDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(journalDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return errors.New("unsafe artifact journal directory")
	}
	lockFD, err := unix.Open(filepath.Join(journalDir, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(lockFD), "artifact-lock")
	defer lock.Close()
	lockInfo, err := lock.Stat()
	if err != nil {
		return err
	}
	if !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm() != 0600 || lockInfo.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return errors.New("unsafe artifact lock")
	}
	if err := unix.Flock(lockFD, unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(lockFD, unix.LOCK_UN)
	journalPath := filepath.Join(journalDir, "ownership.json")
	j, err := loadArtifactJournal(journalPath)
	if err != nil {
		return err
	}
	check := func(name string) (artifactRecord, *artifactFingerprint, error) {
		existing, err := fingerprintArtifact(filepath.Join(dest, name))
		if err != nil {
			return artifactRecord{}, nil, err
		}
		record, found := j.Files[name]
		if !found {
			if existing != nil {
				return record, nil, fmt.Errorf("refusing unmanaged artifact: %s", name)
			}
			return record, nil, nil
		}
		if record.Owner != r.Owner || record.Kind != r.Kind {
			return record, nil, fmt.Errorf("artifact belongs to another method: %s", name)
		}
		if existing != nil && !sameFingerprint(existing, record.Current) && !sameFingerprint(existing, record.Pending) {
			return record, nil, fmt.Errorf("artifact changed outside FetchIt: %s", name)
		}
		return record, existing, nil
	}
	unitOwned := func(name string) error {
		if r.Kind != systemdMethod {
			return nil
		}
		source, err := control(host, r.Host, "show", "--property=FragmentPath", "--value", "--", name)
		if err != nil {
			return err
		}
		if source != "" && source != filepath.Join(r.Host.Destination, name) {
			return fmt.Errorf("unit belongs to another source: %s (%s)", name, source)
		}
		return nil
	}
	remove := func(name string) error {
		record, existing, err := check(name)
		if err != nil {
			return err
		}
		if record.Owner == "" {
			return nil
		} // Missing/untracked files are never adopted for deletion.
		if err := unitOwned(name); err != nil {
			return err
		}
		if r.Kind == systemdMethod {
			source, err := control(host, r.Host, "show", "--property=LoadState", "--value", "--", name)
			if err != nil {
				return err
			}
			if source != "not-found" {
				if _, err := control(host, r.Host, "disable", "--now", "--", name); err != nil {
					return err
				}
			}
		}
		if existing != nil {
			if err := os.Remove(filepath.Join(dest, name)); err != nil {
				return err
			}
		}
		if record.Stage != "" {
			staged, err := fingerprintArtifact(filepath.Join(journalDir, record.Stage))
			if err != nil {
				return err
			}
			if staged != nil {
				if !sameFingerprint(staged, record.Pending) {
					return errors.New("pending artifact changed outside FetchIt")
				}
				if err := os.Remove(filepath.Join(journalDir, record.Stage)); err != nil {
					return err
				}
			}
		}
		delete(j.Files, name)
		if err := syncArtifactDirectory(dest); err != nil {
			return err
		}
		return saveArtifactJournal(journalPath, j)
	}
	if plan.Action == "cleanup" {
		var result error
		names := make([]string, 0, len(j.Files))
		for name, record := range j.Files {
			if record.Owner == r.Owner && record.Kind == r.Kind {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			result = errors.Join(result, remove(name))
		}
		if r.Kind == systemdMethod {
			_, err := control(host, r.Host, "daemon-reload")
			result = errors.Join(result, err)
		}
		return result
	}
	if plan.Action == "delete" {
		if plan.Previous == "" {
			return errors.New("artifact deletion needs a previous name")
		}
		if err := remove(plan.Previous); err != nil {
			return err
		}
		if r.Kind == systemdMethod {
			_, err := control(host, r.Host, "daemon-reload")
			return err
		}
		return nil
	}
	name := filepath.Base(plan.Source)
	record, _, err := check(name)
	if err != nil {
		return err
	}
	if err := unitOwned(name); err != nil {
		return err
	}
	sourcePath := filepath.Join(sourceRoot, strings.TrimPrefix(plan.Source, "/"))
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if !sourceInfo.Mode().IsRegular() {
		return errors.New("artifact source must be a regular file")
	}
	sourceFD, err := unix.Open(sourcePath, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	source := os.NewFile(uintptr(sourceFD), sourcePath)
	defer source.Close()
	openedInfo, err := source.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(sourceInfo, openedInfo) {
		return errors.New("artifact source changed while opening")
	}
	// An interrupted copy can own either the original inode or the staged inode.
	if record.Pending != nil {
		installed, err := fingerprintArtifact(filepath.Join(dest, name))
		if err != nil {
			return err
		}
		if sameFingerprint(installed, record.Pending) {
			record.Current = record.Pending
		}
		stage := filepath.Join(journalDir, record.Stage)
		staged, err := fingerprintArtifact(stage)
		if err != nil {
			return err
		}
		if staged != nil {
			if !sameFingerprint(staged, record.Pending) {
				return errors.New("pending artifact changed outside FetchIt")
			}
			if err := os.Remove(stage); err != nil {
				return err
			}
		}
	}
	stage, err := os.CreateTemp(journalDir, ".stage-")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	// Leave a journaled stage for crash/retry recovery, but remove unjournaled failures.
	journaled := false
	defer func() {
		if !journaled {
			os.Remove(stagePath)
		}
	}()
	if _, err := io.Copy(stage, source); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Chmod(sourceInfo.Mode().Perm()); err != nil {
		stage.Close()
		return err
	}
	if err := errors.Join(stage.Sync(), stage.Close()); err != nil {
		return err
	}
	pending, err := fingerprintArtifact(stagePath)
	if err != nil {
		return err
	}
	record.Owner, record.Kind, record.Pending, record.Stage = r.Owner, r.Kind, pending, filepath.Base(stagePath)
	j.Files[name] = record
	if err := saveArtifactJournal(journalPath, j); err != nil {
		return err
	}
	journaled = true
	if plan.Previous != "" && plan.Previous != name {
		if err := remove(plan.Previous); err != nil {
			return err
		}
	}
	if err := os.Rename(stagePath, filepath.Join(dest, name)); err != nil {
		return err
	}
	if err := syncArtifactDirectory(dest); err != nil {
		return err
	}
	record.Current, record.Pending, record.Stage = pending, nil, ""
	j.Files[name] = record
	if err := saveArtifactJournal(journalPath, j); err != nil {
		return err
	}
	if r.Kind == systemdMethod {
		if _, err := control(host, r.Host, "daemon-reload"); err != nil {
			return err
		}
		if plan.Enable {
			if _, err := control(host, r.Host, "enable", "--now", "--", name); err != nil {
				return err
			}
		}
		if plan.Restart {
			if _, err := control(host, r.Host, "restart", "--", name); err != nil {
				return err
			}
		}
	}
	return nil
}
