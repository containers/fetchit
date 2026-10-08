package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestRollbackRecoveryAndMethodScopedTracking(t *testing.T) {
	old := plumbing.NewHash("1111111111111111111111111111111111111111")
	next := plumbing.NewHash("2222222222222222222222222222222222222222")
	target := &Target{rollback: true, trackBadCommits: true}
	failure := errors.New("apply failure")
	calls := 0
	method := &lifecycleFake{CommonMethod: CommonMethod{Name: "first", target: target}, kind: rawMethod}
	method.apply = func(from, to plumbing.Hash) error {
		calls++
		if calls%2 == 1 {
			if from != old || to != next {
				t.Fatal("wrong apply direction")
			}
			return failure
		}
		if from != next || to != old {
			t.Fatal("wrong rollback direction")
		}
		return nil
	}
	err := applyWithRecovery(context.Background(), context.Background(), method, old, next, nil)
	var recovery *ApplyRecoveryError
	if !errors.As(err, &recovery) || !recovery.RolledBack || !errors.Is(err, failure) || calls != 2 {
		t.Fatal("failed to recover and retain original error")
	}
	if err := applyWithRecovery(context.Background(), context.Background(), method, old, next, nil); !errors.Is(err, ErrBadCommitSkipped) || calls != 2 {
		t.Fatal("bad commit retried")
	}
	method.Name = "second"
	if err := applyWithRecovery(context.Background(), context.Background(), method, old, next, nil); err == nil || calls != 4 {
		t.Fatal("another method inherited failed commit")
	}
}
func TestRollbackInitialDeploymentAndFailedRecovery(t *testing.T) {
	desired := plumbing.NewHash("2222222222222222222222222222222222222222")
	for _, rollbackFails := range []bool{false, true} {
		target := &Target{rollback: true, trackBadCommits: true}
		calls := 0
		applyFailure, rollbackFailure := errors.New("apply"), errors.New("rollback")
		method := &lifecycleFake{CommonMethod: CommonMethod{target: target}, kind: kubeMethod}
		method.apply = func(from, to plumbing.Hash) error {
			calls++
			if calls%2 == 1 {
				return applyFailure
			}
			if from != desired || !to.IsZero() {
				t.Fatal("initial rollback did not target empty state")
			}
			if rollbackFails {
				return rollbackFailure
			}
			return nil
		}
		err := applyWithRecovery(context.Background(), context.Background(), method, plumbing.ZeroHash, desired, nil)
		var recovery *ApplyRecoveryError
		if !errors.As(err, &recovery) || recovery.RolledBack == rollbackFails || !errors.Is(err, applyFailure) {
			t.Fatal("incorrect recovery result")
		}
		if rollbackFails {
			if !errors.Is(err, rollbackFailure) {
				t.Fatal("lost rollback error")
			}
			applyWithRecovery(context.Background(), context.Background(), method, plumbing.ZeroHash, desired, nil)
			if calls != 4 {
				t.Fatal("failed recovery suppressed retry")
			}
		}
	}
}
func TestRollbackPreflightAndDisabledPolicy(t *testing.T) {
	desired := plumbing.NewHash("2222222222222222222222222222222222222222")
	for _, enabled := range []bool{false, true} {
		calls := 0
		preflight := &SOPSPreflightError{cause: ErrSOPSInput}
		method := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{rollback: enabled, trackBadCommits: true}}, kind: kubeMethod, apply: func(plumbing.Hash, plumbing.Hash) error { calls++; return preflight }}
		err := applyWithRecovery(context.Background(), context.Background(), method, plumbing.ZeroHash, desired, nil)
		if !errors.Is(err, ErrSOPSInput) || calls != 1 {
			t.Fatal("preflight caused mutation/recovery")
		}
		applyWithRecovery(context.Background(), context.Background(), method, plumbing.ZeroHash, desired, nil)
		if calls != 2 {
			t.Fatal("preflight failure suppressed retry")
		}
	}
}
