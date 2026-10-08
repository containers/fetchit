package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestCommonGitProcessRetriesAndAdvancesOnlyOnSuccess(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	initial := mirrorCommit(t, source, "initial")
	common := CommonMethod{Name: "shared", target: target, initialRun: true}
	method := &lifecycleFake{CommonMethod: common, kind: rawMethod}
	calls := 0
	fail := true
	method.apply = func(from, to plumbing.Hash) error {
		calls++
		if fail {
			return errors.New("apply failed")
		}
		return nil
	}
	ctx := context.Background()
	method.processGit(ctx, ctx, method, 0, nil)
	current, err := getCurrent(target, rawMethod, method.Name)
	if err != nil || !current.IsZero() || !method.initialRun || calls != 1 {
		t.Fatalf("failure advanced state: %s %v calls=%d", current, err, calls)
	}
	fail = false
	method.processGit(ctx, ctx, method, 0, nil)
	current, err = getCurrent(target, rawMethod, method.Name)
	if err != nil || current != initial || method.initialRun || calls != 2 {
		t.Fatalf("retry failed: %s %v calls=%d", current, err, calls)
	}
	method.processGit(ctx, ctx, method, 0, nil)
	if calls != 2 {
		t.Fatal("unchanged commit applied again")
	}
	// Startup replay must still run once after a configuration reload.
	method.initialRun = true
	method.processGit(ctx, ctx, method, 0, nil)
	if method.initialRun || calls != 3 {
		t.Fatal("saved revision was not replayed")
	}
	next := mirrorCommit(t, source, "next")
	method.processGit(ctx, ctx, method, 0, nil)
	current, err = getCurrent(target, rawMethod, method.Name)
	if err != nil || current != next || calls != 4 {
		t.Fatalf("new revision not applied: %s %v calls=%d", current, err, calls)
	}
}
