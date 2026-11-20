// Package check implements the per-device proxy health probes.
package check

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Config describes one health probe. Kind selects the probe:
//
//	tcp  — TCP connect to Target ("host:port"); success if it connects
//	http — GET Target; success on any 2xx status
//	exec — run Target as a shell command; success on exit code 0
type Config struct {
	Kind       string  `json:"kind"`
	Target     string  `json:"target"`
	TimeoutSec float64 `json:"timeout_sec"`
}

// Probe is anything that can report whether the proxy is alive.
type Probe interface {
	Run(ctx context.Context) error
}

// AnyOf succeeds when any one of its sub-probes succeeds — a device is
// "proxied" when at least one of its proxy/VPN clients is up. Sub-probes
// run sequentially, so keep the list short and timeouts small.
type AnyOf []Probe

func (a AnyOf) Run(ctx context.Context) error {
	var errs []error
	for _, p := range a {
		if err := p.Run(ctx); err != nil {
			errs = append(errs, err)
		} else {
			return nil
		}
	}
	return errors.Join(errs...)
}

// Invalid is a probe that always fails; wiring one in on a broken config
// keeps the kill switch fail-closed.
type Invalid struct{ Err error }

func (i Invalid) Run(context.Context) error { return i.Err }

const defaultTimeoutSec = 1.0

// WithDefaults fills in unset fields.
func (c Config) WithDefaults() Config {
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = defaultTimeoutSec
	}
	if c.Kind == "" {
		c.Kind = "tcp"
	}
	return c
}

// Validate reports whether the configuration is usable.
func (c Config) Validate() error {
	switch c.Kind {
	case "tcp", "http", "exec":
	default:
		return fmt.Errorf("check.kind must be tcp, http or exec (got %q)", c.Kind)
	}
	if c.Target == "" {
		return errors.New("check.target is empty")
	}
	if c.TimeoutSec <= 0 {
		return errors.New("check.timeout_sec must be > 0")
	}
	return nil
}

// Run executes the probe once.
func (c Config) Run(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSec*float64(time.Second)))
	defer cancel()
	switch c.Kind {
	case "tcp":
		return runTCP(ctx, c.Target)
	case "http":
		return runHTTP(ctx, c.Target)
	case "exec":
		return runExec(ctx, c.Target)
	default:
		return fmt.Errorf("unknown check kind %q", c.Kind)
	}
}

func runTCP(ctx context.Context, target string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	if err != nil {
		return err
	}
	return conn.Close()
}

func runHTTP(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func runExec(ctx context.Context, command string) error {
	cmd, err := execCommand(ctx, command)
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("timed out: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("exit %v: %s", err, firstLine(out))
	}
	return nil
}

func firstLine(b []byte) string {
	s := string(b)
	if i := indexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
