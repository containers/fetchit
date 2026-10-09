package engine

import (
	"context"
	"testing"
	"time"

	"github.com/go-co-op/gocron"
)

func waitShutdownTest(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shutdown test job")
	}
}

func TestShutdownCancelsAndWaitsForRunningJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := gocron.NewScheduler(time.UTC)
	fc := &FetchitConfig{lifetime: ctx, scheduler: s}
	f := newFetchit()
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	m := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{}}, kind: rawMethod, process: func() {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
	}}
	if _, err := s.Every(1).Hour().Do(func() { f.runMethod(m, ctx, context.Background(), 0) }); err != nil {
		t.Fatal(err)
	}
	s.StartAsync()
	waitShutdownTest(t, entered)
	cancel()
	done := make(chan struct{})
	go func() { fc.shutdown(time.Second); close(done) }()
	waitShutdownTest(t, canceled)
	select {
	case <-done:
		t.Fatal("shutdown returned before running job finished")
	case <-time.After(20 * time.Millisecond):
	}
	release <- struct{}{}
	waitShutdownTest(t, done)
	if s.IsRunning() {
		t.Fatal("scheduler still running after shutdown")
	}
}

func TestShutdownBoundsUncooperativeJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := gocron.NewScheduler(time.UTC)
	fc := &FetchitConfig{lifetime: ctx, scheduler: s}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	if _, err := s.Every(1).Hour().Do(func() { close(entered); <-release; close(finished) }); err != nil {
		t.Fatal(err)
	}
	s.StartAsync()
	waitShutdownTest(t, entered)
	cancel()
	started := time.Now()
	fc.shutdown(40 * time.Millisecond)
	if elapsed := time.Since(started); elapsed < 35*time.Millisecond || elapsed > time.Second {
		t.Fatalf("shutdown did not respect grace period: %s", elapsed)
	}
	release <- struct{}{}
	waitShutdownTest(t, finished)
}

func TestShutdownRejectsQueuedMethodsAndReloads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := newFetchit()
	for _, kind := range []string{rawMethod, configFileMethod} {
		m := &lifecycleFake{CommonMethod: CommonMethod{target: &Target{}}, kind: kind, process: func() {
			t.Fatal("canceled job ran")
		}}
		f.runMethod(m, ctx, context.Background(), 0)
	}
	// A canceled reload must return before accessing/replacing the active engine.
	fc := &FetchitConfig{lifetime: ctx}
	fc.Restart()
}

func TestShutdownBoundsStartupAndReloadLock(t *testing.T) {
	configRestartMu.Lock()
	defer configRestartMu.Unlock()
	fc := &FetchitConfig{}
	done := make(chan struct{})
	go func() { fc.shutdown(40 * time.Millisecond); close(done) }()
	waitShutdownTest(t, done)
}

func TestShutdownBeforeInitialization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fc := newFetchitConfig()
	if err := fc.run(ctx); err != nil {
		t.Fatalf("canceled startup returned an error: %v", err)
	}
	if fc.scheduler != nil || fc.conn != nil {
		t.Fatal("canceled startup initialized the engine")
	}
}
