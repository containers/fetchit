package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/pods"
)

var defaultRemovalsPath = "/opt/.fetchit/method-removals.json"

// Receipts contain identifiers only, never Git credentials, age keys or manifests.
type removalReceipt struct {
	Kind    string          `json:"kind"`
	Owner   string          `json:"owner"`
	Name    string          `json:"name"`
	Quadlet *quadletRemoval `json:"quadlet,omitempty"`
}
type quadletRemoval struct {
	Parent      string `json:"parent"`
	Runtime     string `json:"runtime"`
	Namespace   string `json:"namespace"`
	Home        string `json:"home"`
	HelperImage string `json:"helperImage"`
}
type removalState struct {
	Version  int                       `json:"version"`
	Receipts map[string]removalReceipt `json:"receipts"`
}
type removalStore struct {
	mu     sync.Mutex
	path   string
	state  removalState
	active map[string]bool
	remove func(context.Context, removalReceipt) error
}

func loadRemovalStore(path string) (*removalStore, error) {
	s := &removalStore{path: path, state: removalState{Version: 1, Receipts: map[string]removalReceipt{}}, active: map[string]bool{}, remove: removeOwnedWorkloads}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("invalid method-removal state file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s.state); err != nil {
		return nil, errors.New("cannot decode method-removal state")
	}
	if s.state.Version != 1 || s.state.Receipts == nil {
		return nil, errors.New("unsupported method-removal state")
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("invalid method-removal state trailer")
	}
	for key, r := range s.state.Receipts {
		if key != receiptKey(r) || !validRemovalReceipt(r) {
			return nil, errors.New("invalid method-removal receipt")
		}
	}
	return s, nil
}
func receiptKey(r removalReceipt) string { return r.Kind + "/" + r.Owner }
func validRemovalReceipt(r removalReceipt) bool {
	if len(r.Owner) != 32 {
		return false
	}
	for _, c := range r.Owner {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	if r.Kind == kubeMethod || r.Kind == rawMethod {
		return r.Quadlet == nil
	}
	if r.Kind != quadletMethod || r.Quadlet == nil {
		return false
	}
	q := r.Quadlet
	return filepath.IsAbs(q.Parent) && filepath.Clean(q.Parent) == q.Parent && q.Parent != "/" && quadletSafeName.MatchString(q.Namespace) && filepath.IsAbs(q.Home) && (q.Runtime == "" || filepath.IsAbs(q.Runtime))
}

func methodRemovalReceipt(m Method) (removalReceipt, bool, error) {
	var common *CommonMethod
	switch method := m.(type) {
	case *Kube:
		common = &method.CommonMethod
	case *Raw:
		common = &method.CommonMethod
	case *Quadlet:
		common = &method.CommonMethod
	default:
		if flag, ok := m.(interface{ removalEnabled() bool }); ok && flag.removalEnabled() {
			return removalReceipt{}, false, fmt.Errorf("cleanupOnRemoval is unsupported for %s", m.GetKind())
		}
		return removalReceipt{}, false, nil
	}
	r := removalReceipt{Kind: m.GetKind(), Owner: common.workloadLabels()[kubeOwnerLabel], Name: common.Name}
	if q, ok := m.(*Quadlet); ok {
		parent, runtime, err := q.paths()
		if err != nil {
			return r, false, err
		}
		namespace, home := q.hostIdentity()
		r.Quadlet = &quadletRemoval{Parent: parent, Runtime: runtime, Namespace: namespace, Home: home, HelperImage: q.HelperImage}
		// Activation changes retain ownership; moving to another host directory does not.
		r.Owner = kubeOwner(parent, runtime, namespace, home)
	}
	return r, common.CleanupOnRemoval, nil
}
func (m *CommonMethod) removalEnabled() bool { return m.CleanupOnRemoval }

// Persist intent before a configured job can create any workloads. Disabling the
// flag on an active method deliberately relinquishes cleanup responsibility.
func (s *removalStore) configure(methods map[Method]SchedInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]removalReceipt, len(s.state.Receipts))
	for key, r := range s.state.Receipts {
		next[key] = r
	}
	active := map[string]bool{}
	for m := range methods {
		r, enabled, err := methodRemovalReceipt(m)
		if err != nil {
			return err
		}
		if r.Kind == "" {
			continue
		}
		key := receiptKey(r)
		active[key] = true
		if enabled {
			next[key] = r
		} else {
			delete(next, key)
		}
	}
	if err := s.save(removalState{Version: 1, Receipts: next}); err != nil {
		return err
	}
	s.state.Receipts = next
	s.active = active
	return nil
}
func (s *removalStore) save(state removalState) error {
	if len(state.Receipts) == 0 {
		if _, err := os.Stat(s.path); os.IsNotExist(err) {
			return nil
		}
	}
	parent := filepath.Dir(s.path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid removal-state directory")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	stage, err := stageConfig(s.path, data)
	if err != nil {
		return err
	}
	defer os.Remove(stage)
	return os.Rename(stage, s.path)
}
func (s *removalStore) reconcile(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.state.Receipts))
	for key := range s.state.Receipts {
		if !s.active[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var result error
	for _, key := range keys {
		if err := s.remove(ctx, s.state.Receipts[key]); err != nil {
			result = errors.Join(result, fmt.Errorf("cleanup pending for %s: %w", key, err))
			continue
		}
		next := make(map[string]removalReceipt, len(s.state.Receipts))
		for k, r := range s.state.Receipts {
			if k != key {
				next[k] = r
			}
		}
		if err := s.save(removalState{Version: 1, Receipts: next}); err != nil {
			result = errors.Join(result, err)
			continue
		}
		s.state.Receipts = next
	}
	return result
}
func matchesRemoval(labels map[string]string, r removalReceipt) bool {
	return labels[kubeManagedByLabel] == "fetchit" && labels[kubeOwnerLabel] == r.Owner && (r.Kind != rawMethod || labels[kubeMethodLabel] == rawMethod)
}
func removeOwnedWorkloads(conn context.Context, r removalReceipt) error {
	filters := map[string][]string{"label": {kubeManagedByLabel + "=fetchit", kubeOwnerLabel + "=" + r.Owner}}
	switch r.Kind {
	case kubeMethod:
		listed, err := pods.List(conn, new(pods.ListOptions).WithFilters(filters))
		if err != nil {
			return err
		}
		for _, pod := range listed {
			if pod == nil || !matchesRemoval(pod.Labels, r) {
				continue
			}
			// Recheck the immutable ID so a same-name replacement cannot be removed.
			inspected, err := pods.Inspect(conn, pod.Id, nil)
			if err != nil {
				return err
			}
			if !matchesRemoval(inspected.Labels, r) {
				return errors.New("Pod ownership changed during cleanup")
			}
			report, err := pods.Remove(conn, pod.Id, new(pods.RemoveOptions).WithForce(true))
			if err != nil {
				return err
			}
			if report != nil {
				if report.Err != nil {
					return report.Err
				}
				for _, err := range report.RemovedCtrs {
					if err != nil {
						return err
					}
				}
			}
		}
	case rawMethod:
		filters["label"] = append(filters["label"], kubeMethodLabel+"="+rawMethod)
		listed, err := containers.List(conn, new(containers.ListOptions).WithAll(true).WithFilters(filters))
		if err != nil {
			return err
		}
		for _, container := range listed {
			if !matchesRemoval(container.Labels, r) {
				continue
			}
			inspected, err := containers.Inspect(conn, container.ID, nil)
			if err != nil {
				return err
			}
			if !matchesRemoval(inspected.Config.Labels, r) {
				return errors.New("container ownership changed during cleanup")
			}
			reports, err := containers.Remove(conn, container.ID, new(containers.RemoveOptions).WithForce(true))
			if err != nil {
				return err
			}
			for _, report := range reports {
				if report != nil && report.Err != nil {
					return report.Err
				}
			}
		}
	case quadletMethod:
		q := r.Quadlet
		if q == nil {
			return errors.New("missing Quadlet removal identity")
		}
		method := &Quadlet{HelperImage: q.HelperImage}
		return method.deploy(context.Background(), conn, quadletPlan{previous: quadletBundle{}, desired: quadletBundle{files: map[string][]byte{}, modes: map[string]int64{}}, parent: q.Parent, runtime: q.Runtime, namespace: q.Namespace, home: q.Home, current: "0000000000000000000000000000000000000000", desiredRevision: "0000000000000000000000000000000000000000", configID: "removal", removal: true})
	default:
		return errors.New("unsupported removal receipt")
	}
	return nil
}
