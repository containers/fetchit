package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
)

var ErrBadCommitSkipped = errors.New("previously rolled-back commit skipped; disable trackBadCommits to retry")

// ApplyRecoveryError preserves both failures without embedding response contents
// in logs. Rollback is best effort, not an atomic or health-checked deployment.
type ApplyRecoveryError struct {
	ApplyError    error
	RollbackError error
	RolledBack    bool
}

func (e *ApplyRecoveryError) Error() string {
	if e.RolledBack {
		return "apply failed; previous Git state restored"
	}
	if e.RollbackError != nil {
		return "apply failed and rollback failed; workload state may be partial"
	}
	return "apply failed; rollback was not attempted"
}
func (e *ApplyRecoveryError) Unwrap() []error {
	result := []error{e.ApplyError}
	if e.RollbackError != nil {
		result = append(result, e.RollbackError)
	}
	return result
}

func applyWithRecovery(ctx, conn context.Context, m Method, current, desired plumbing.Hash, tags *[]string) error {
	target := m.GetTarget()
	if !target.rollback {
		return m.Apply(ctx, conn, current, desired, tags)
	}
	switch m.GetKind() {
	case rawMethod, kubeMethod, quadletMethod:
	default:
		return fmt.Errorf("rollback is unsupported for %s", m.GetKind())
	}
	key := m.GetKind() + "/" + m.GetName() + "/" + current.String()
	target.badCommitMu.Lock()
	skip := target.trackBadCommits && target.badCommits[key] == desired
	target.badCommitMu.Unlock()
	if skip {
		return ErrBadCommitSkipped
	}
	applyErr := m.Apply(ctx, conn, current, desired, tags)
	if applyErr == nil {
		return nil
	}
	// A Kube SOPS preparation error guarantees that no mutation was attempted.
	var preflight *SOPSPreflightError
	if errors.As(applyErr, &preflight) {
		return &ApplyRecoveryError{ApplyError: applyErr}
	}
	rollbackErr := m.Apply(ctx, conn, desired, current, tags)
	if rollbackErr == nil && target.trackBadCommits {
		target.badCommitMu.Lock()
		if target.badCommits == nil {
			target.badCommits = map[string]plumbing.Hash{}
		}
		// Bound process-local retry history; never share failure state between methods.
		if len(target.badCommits) >= 128 {
			clear(target.badCommits)
		}
		target.badCommits[key] = desired
		target.badCommitMu.Unlock()
	}
	return &ApplyRecoveryError{ApplyError: applyErr, RollbackError: rollbackErr, RolledBack: rollbackErr == nil}
}
