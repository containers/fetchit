package engine

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Leave room for Podman's default ten-second stop timeout.
const shutdownGracePeriod = 5 * time.Second

func (fc *FetchitConfig) runtimeContext() context.Context {
	if fc.lifetime != nil {
		return fc.lifetime
	}
	return context.Background()
}

func (fc *FetchitConfig) run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	fc.lifetime = ctx
	started := make(chan error, 1)
	go func() {
		// Shutdown and reload must not race initial scheduler creation/startup.
		configRestartMu.Lock()
		defer configRestartMu.Unlock()
		if err := ctx.Err(); err != nil {
			started <- err
			return
		}
		f := fc.InitConfig(true)
		started <- f.startTargets()
	}()

	select {
	case err := <-started:
		if err != nil && ctx.Err() == nil {
			cancel()
			fc.shutdown(shutdownGracePeriod)
			return err
		}
		if err == nil {
			<-ctx.Done()
		}
	case <-ctx.Done():
	}
	fc.shutdown(shutdownGracePeriod)
	return nil
}

// The lifetime context is canceled before this call, so queued methods and
// reloads cannot start new work. Stop waits for scheduled jobs to finish, but
// initialization, Git operations, or helpers may not honor cancellation yet.
func (fc *FetchitConfig) shutdown(grace time.Duration) {
	done := make(chan struct{})
	go func() {
		configRestartMu.Lock()
		scheduler := fc.scheduler
		configRestartMu.Unlock()
		// Never hold the reload mutex while waiting for jobs: a reload job may
		// itself be waiting for that mutex before observing cancellation.
		if scheduler != nil {
			scheduler.Stop()
		}
		close(done)
	}()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		fmt.Fprintln(os.Stderr, "FetchIt shutdown complete")
	case <-timer.C:
		fmt.Fprintln(os.Stderr, "FetchIt shutdown grace period expired; exiting with work still in flight")
	}
}
