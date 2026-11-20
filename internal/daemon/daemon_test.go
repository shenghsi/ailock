package daemon

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shenghsi/ailock/internal/check"
	"github.com/shenghsi/ailock/internal/hostsedit"
)

type fakeProbe struct {
	mu  sync.Mutex
	err error
}

func (f *fakeProbe) setErr(e error) {
	f.mu.Lock()
	f.err = e
	f.mu.Unlock()
}

func (f *fakeProbe) Run(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// runUntil steps the daemon loop until cond or timeout.
func runUntil(t *testing.T, ctx context.Context, cfg Config, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		select {
		case <-ctx.Done():
			t.Fatal("context cancelled")
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("condition never met")
		}
	}
}

func newTestConfig(t *testing.T, probe check.Probe) Config {
	t.Helper()
	cfg := Config{
		Probe:        probe,
		Interval:     10 * time.Millisecond,
		UnblockAfter: 2,
		HostsPath:    filepath.Join(t.TempDir(), "hosts"),
		Domains:      []string{"a.com", "b.com"},
		Log:          log.New(os.Stderr, "", 0),
	}
	if err := os.WriteFile(cfg.HostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestFailClosedStart(t *testing.T) {
	cfg := newTestConfig(t, &fakeProbe{}) // healthy probe
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, cfg) }()
	// Even with a healthy proxy, the daemon must block first.
	runUntil(t, ctx, cfg, func() bool { return hostsedit.IsBlocked(cfg.HostsPath) })
	cancel()
}

func TestBlockOnFailureAndUnblockAfterStreak(t *testing.T) {
	probe := &fakeProbe{}
	probe.setErr(errors.New("down"))
	cfg := newTestConfig(t, probe)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	runUntil(t, ctx, cfg, func() bool { return hostsedit.IsBlocked(cfg.HostsPath) })

	// Proxy comes back: needs UnblockAfter consecutive successes to lift.
	probe.setErr(nil)
	runUntil(t, ctx, cfg, func() bool { return !hostsedit.IsBlocked(cfg.HostsPath) })

	// Proxy dies again: blocks on the FIRST failure.
	probe.setErr(errors.New("down again"))
	runUntil(t, ctx, cfg, func() bool { return hostsedit.IsBlocked(cfg.HostsPath) })

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run returned %v", err)
	}
	// Shutting down leaves the blocked state in place (fail closed).
	if !hostsedit.IsBlocked(cfg.HostsPath) {
		t.Fatal("shutdown must leave the block in place")
	}
}

type flapProbe struct{ n int }

// Run alternates healthy / unhealthy, so consecutive successes never
// reach UnblockAfter no matter how probes land in time.
func (f *flapProbe) Run(context.Context) error {
	f.n++
	if f.n%2 == 1 {
		return nil
	}
	return errors.New("flap")
}

func TestUnblockRequiresStreak(t *testing.T) {
	probe := &flapProbe{}
	cfg := newTestConfig(t, probe)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Run(ctx, cfg) }()
	runUntil(t, ctx, cfg, func() bool { return hostsedit.IsBlocked(cfg.HostsPath) })
	// Let it flap for a while; the block must persist.
	time.Sleep(300 * time.Millisecond)
	if !hostsedit.IsBlocked(cfg.HostsPath) {
		t.Fatal("block lifted despite flapping proxy")
	}
	cancel()
}
