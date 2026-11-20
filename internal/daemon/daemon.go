// Package daemon runs the fail-closed toggle loop: block the AI domains
// whenever the proxy health probe fails, lift the block once the proxy has
// been healthy for UnblockAfter consecutive probes.
package daemon

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shenghsi/ailock/internal/check"
	"github.com/shenghsi/ailock/internal/hostsedit"
	"github.com/shenghsi/ailock/internal/platform"
)

const (
	defaultInterval     = 2 * time.Second
	defaultUnblockAfter = 2
	minimumInterval     = 200 * time.Millisecond
	minimumUnblockAfter = 1
)

// Config wires the daemon to a hosts file, a blocklist and a probe.
type Config struct {
	Probe          check.Probe
	Interval       time.Duration // probe period; default 2s
	UnblockAfter   int           // consecutive successes before unblocking; default 2
	HostsPath      string
	Domains        []string
	DnsmasqConfDir string // optional: also maintain a dnsmasq address= file here
	Log            *log.Logger
}

func (c *Config) normalize() error {
	if c.Probe == nil {
		return fmt.Errorf("daemon: no probe configured")
	}
	if len(c.Domains) == 0 {
		return fmt.Errorf("daemon: empty blocklist")
	}
	if c.HostsPath == "" {
		c.HostsPath = platform.HostsPath()
	}
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	if c.Interval < minimumInterval {
		c.Interval = minimumInterval
	}
	if c.UnblockAfter < minimumUnblockAfter {
		c.UnblockAfter = defaultUnblockAfter
	}
	if c.Log == nil {
		c.Log = log.Default()
	}
	return nil
}

// Block applies the sinkhole (hosts region + optional dnsmasq file) and
// flushes DNS caches.
func (c Config) Block() error {
	if err := hostsedit.SetBlocked(c.HostsPath, c.Domains); err != nil {
		return fmt.Errorf("write hosts block: %w", err)
	}
	if c.DnsmasqConfDir != "" {
		if err := platform.SetDnsmasqBlocked(c.DnsmasqConfDir, c.Domains); err != nil {
			c.logf("WARNING: dnsmasq block not applied: %v", err)
		}
	}
	platform.FlushDNS() // advisory
	return nil
}

// Unblock removes the sinkhole everywhere it was applied.
func (c Config) Unblock() error {
	if err := hostsedit.Remove(c.HostsPath); err != nil {
		return fmt.Errorf("remove hosts block: %w", err)
	}
	if c.DnsmasqConfDir != "" {
		if err := platform.RemoveDnsmasqBlocked(c.DnsmasqConfDir); err != nil {
			c.logf("WARNING: dnsmasq block not removed: %v", err)
		}
	}
	platform.FlushDNS() // advisory
	return nil
}

// BlockedNow reports the on-disk state (true = AI domains sinkholed).
func (c Config) BlockedNow() bool { return hostsedit.IsBlocked(c.HostsPath) }

func (c Config) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log.Printf(format, args...)
	}
}

// Run blocks until ctx is cancelled. It starts fail-closed (immediately
// applies the block, then probes), blocks on the first probe failure and
// unblocks only after UnblockAfter consecutive successes.
func Run(ctx context.Context, cfg Config) error {
	if err := cfg.normalize(); err != nil {
		return err
	}
	cfg.logf("ailock starting: hosts=%s domains=%d interval=%s unblock_after=%d dnsmasq=%q",
		cfg.HostsPath, len(cfg.Domains), cfg.Interval, cfg.UnblockAfter, cfg.DnsmasqConfDir)

	// Fail closed: assume the proxy is down until proven healthy.
	if err := cfg.Block(); err != nil {
		return fmt.Errorf("initial fail-closed block: %w", err)
	}
	cfg.logf("blocked (fail-closed start; unblocks after %d healthy probes)", cfg.UnblockAfter)

	streak := 0
	for {
		probeErr := cfg.Probe.Run(ctx)
		switch probeErr {
		case nil:
			streak++
			if streak >= cfg.UnblockAfter && cfg.BlockedNow() {
				if err := cfg.Unblock(); err != nil {
					cfg.logf("ERROR: unblock failed: %v", err)
				} else {
					cfg.logf("proxy healthy %d/%d — unblocked", streak, cfg.UnblockAfter)
				}
			}
		default:
			streak = 0
			if !cfg.BlockedNow() {
				if err := cfg.Block(); err != nil {
					cfg.logf("ERROR: block failed: %v", err)
				} else {
					cfg.logf("proxy down (%v) — blocked", probeErr)
				}
			}
		}

		select {
		case <-ctx.Done():
			// Leave the current on-disk state in place: if we are blocked,
			// staying blocked is the safe direction ("ailock unblock" is the
			// manual escape hatch).
			cfg.logf("shutting down (state left as-is, blocked=%v)", cfg.BlockedNow())
			return nil
		case <-time.After(cfg.Interval):
		}
	}
}
