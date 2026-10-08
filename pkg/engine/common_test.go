package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestCommonGitProcessRetriesAndAdvancesOnlyOnSuccess(t *testing.T) {
	target, source, _ := mirrorTestTarget(t)
	initial := mirrorCommit(t, source, "initial")
	common := CommonMethod{Name: "shared", target: target, initialRun: true}
	method := &lifecycleFake{CommonMethod: common, kind: rawMethod}
	calls := 0
	fail := true
	expectedFrom, expectedTo := plumbing.ZeroHash, initial
	method.apply = func(from, to plumbing.Hash) error {
		calls++
		if from != expectedFrom || to != expectedTo {
			t.Fatalf("wrong transition: %s -> %s, want %s -> %s", from, to, expectedFrom, expectedTo)
		}
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
	expectedFrom, expectedTo = initial, next
	method.processGit(ctx, ctx, method, 0, nil)
	current, err = getCurrent(target, rawMethod, method.Name)
	if err != nil || current != next || calls != 4 {
		t.Fatalf("new revision not applied: %s %v calls=%d", current, err, calls)
	}
}

func TestCommonGitCancelledSkew(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	method := &lifecycleFake{CommonMethod: CommonMethod{initialRun: true}, kind: rawMethod}
	done := make(chan struct{})
	go func() { method.processGit(ctx, ctx, method, 60000, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled skew did not return")
	}
	if !method.initialRun {
		t.Fatal("cancelled startup advanced state")
	}
}
