// ailock — sinkhole US AI provider domains whenever the local proxy is
// not running. See README.md for the full story.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/shenghsi/ailock/internal/blocklist"
	"github.com/shenghsi/ailock/internal/check"
	"github.com/shenghsi/ailock/internal/daemon"
	"github.com/shenghsi/ailock/internal/platform"
	"github.com/shenghsi/ailock/internal/svcwin"
)

//go:embed blocklist
var embeddedBlocklist []byte

const version = "0.1.0"

const usage = `ailock — sinkhole US AI providers while the proxy is down

Usage:
  ailock [-c CONFIG] COMMAND

Commands:
  run        run the watcher in the foreground (what the service runs)
  install    write default config/blocklist and register the OS service
  uninstall  stop and remove the OS service (config and blocklist are kept)
  status     show block state and one probe result
  check      run the health probe once and report
  block      apply the block immediately (manual/testing)
  unblock    remove the block immediately (manual escape hatch)
  version    print the version

The default config path is /etc/ailock/config.json (%ProgramData%\ailock on
Windows). If the config or blocklist file is missing, embedded defaults are
used, so a broken config can never silently disable the kill switch.`

func main() {
	fs := flag.NewFlagSet("ailock", flag.ContinueOnError)
	cfgPath := fs.String("c", defaultConfigPath(), "config file path")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	args := fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch args[0] {
	case "run":
		err = cmdRun(*cfgPath)
	case "install":
		err = cmdInstall(*cfgPath)
	case "uninstall":
		err = cmdUninstall()
	case "status":
		err = cmdStatus(*cfgPath)
	case "check":
		err = cmdCheck(*cfgPath)
	case "block":
		err = cmdBlock(*cfgPath, true)
	case "unblock":
		err = cmdBlock(*cfgPath, false)
	case "version":
		fmt.Println("ailock", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s\n", args[0], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ailock:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------- config

type appConfig struct {
	Check          check.Config   `json:"check"`  // single probe (simple setups)
	Checks         []check.Config `json:"checks"` // any-of probe list; devices running several clients
	IntervalSec    float64        `json:"interval_sec"`
	UnblockAfter   int            `json:"unblock_after"`
	HostsFile      string         `json:"hosts_file"`
	BlocklistFile  string         `json:"blocklist_file"`
	DnsmasqConfDir string         `json:"dnsmasq_conf_dir"`
}

func defaultConfigPath() string { return filepath.Join(platform.ConfigDir(), "config.json") }

func defaultBlocklistPath() string { return filepath.Join(platform.ConfigDir(), "blocklist") }

func defaultAppConfig() appConfig {
	return appConfig{
		Check:        check.Config{Kind: "tcp", Target: "127.0.0.1:10808", TimeoutSec: 1.0},
		IntervalSec:  2.0,
		UnblockAfter: 2,
	}
}

// loadConfig reads the config file, falling back to embedded defaults
// (with a warning) when it is missing so the daemon still runs fail-closed.
func loadConfig(path string) (appConfig, string) {
	warn := ""
	cfg := defaultAppConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Sprintf("config %s missing — using embedded defaults", path)
		}
		return cfg, fmt.Sprintf("config %s unreadable (%v) — using embedded defaults", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultAppConfig(), fmt.Sprintf("config %s invalid JSON (%v) — using embedded defaults", path, err)
	}
	return cfg, warn
}

// loadBlocklist resolves the blocklist file, falling back to the embedded
// copy so a missing file can never disable the kill switch.
func loadBlocklist(cfg appConfig) (*blocklist.List, string) {
	path := cfg.BlocklistFile
	if path == "" {
		path = defaultBlocklistPath()
	}
	data, err := os.ReadFile(path)
	if err == nil {
		l, perr := blocklist.Parse(data)
		if perr == nil {
			return l, ""
		}
		return mustEmbedded(), fmt.Sprintf("blocklist %s invalid (%v) — using embedded default", path, perr)
	}
	return mustEmbedded(), fmt.Sprintf("blocklist %s missing/unreadable (%v) — using embedded default", path, err)
}

func mustEmbedded() *blocklist.List {
	l, err := blocklist.Parse(embeddedBlocklist)
	if err != nil {
		panic(fmt.Sprintf("embedded blocklist broken: %v", err))
	}
	return l
}

// buildProbe turns the config into one Probe: the singular check plus any
// entries of checks, combined any-of. A broken/missing check config yields
// an always-failing probe so the kill switch stays fail-closed.
func buildProbe(cfg appConfig) (probe check.Probe, warn string) {
	var probes []check.Probe
	single := cfg.Check.WithDefaults()
	if cfg.Check.Target != "" {
		if err := single.Validate(); err != nil {
			return check.Invalid{Err: fmt.Errorf("check invalid: %w", err)},
				fmt.Sprintf("check invalid (%v) — staying blocked (fail closed)", err)
		}
		probes = append(probes, single)
	}
	for i := range cfg.Checks {
		c := cfg.Checks[i].WithDefaults()
		if err := c.Validate(); err != nil {
			return check.Invalid{Err: fmt.Errorf("checks[%d] invalid: %w", i, err)},
				fmt.Sprintf("checks[%d] invalid (%v) — staying blocked (fail closed)", i, err)
		}
		probes = append(probes, c)
	}
	if len(probes) == 0 {
		return check.Invalid{Err: errors.New("no check configured")},
			"no check/checks configured — staying blocked (fail closed)"
	}
	if len(probes) == 1 {
		return probes[0], ""
	}
	return check.AnyOf(probes), ""
}

func daemonConfig(probe check.Probe, cfg appConfig, domains []string, logger *log.Logger) daemon.Config {
	return daemon.Config{
		Probe:          probe,
		Interval:       time.Duration(cfg.IntervalSec * float64(time.Second)),
		UnblockAfter:   cfg.UnblockAfter,
		HostsPath:      cfg.HostsFile,
		Domains:        domains,
		DnsmasqConfDir: cfg.DnsmasqConfDir,
		Log:            logger,
	}
}

// ---------------------------------------------------------------- commands

func cmdRun(cfgPath string) error {
	cfg, warn := loadConfig(cfgPath)
	list, lwarn := loadBlocklist(cfg)
	probe, pwarn := buildProbe(cfg)
	logger := log.New(os.Stderr, "", log.LstdFlags)
	for _, w := range []string{warn, lwarn, pwarn} {
		if w != "" {
			logger.Print("WARNING: ", w)
		}
	}

	if runtime.GOOS == "windows" && svcwin.IsService() {
		// Service processes have no stderr worth writing to.
		if f, err := os.OpenFile(filepath.Join(platform.ConfigDir(), "ailock.log"),
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			logger.SetOutput(f)
		}
		return svcwin.RunService(func() (func(ctx context.Context) error, error) {
			d := daemonConfig(probe, cfg, list.Domains(), logger)
			return func(ctx context.Context) error { return daemon.Run(ctx, d) }, nil
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.Run(ctx, daemonConfig(probe, cfg, list.Domains(), logger))
}

func cmdCheck(cfgPath string) error {
	cfg, warn := loadConfig(cfgPath)
	probe, pwarn := buildProbe(cfg)
	start := time.Now()
	err := probe.Run(context.Background())
	if err != nil {
		fmt.Printf("proxy check FAILED: %v\n", err)
		if warn != "" || pwarn != "" {
			fmt.Println("note:", firstNonEmpty(warn, pwarn))
		}
		return err
	}
	fmt.Printf("proxy check OK (%.0fms)\n", float64(time.Since(start).Microseconds())/1000)
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cmdStatus(cfgPath string) error {
	cfg, warn := loadConfig(cfgPath)
	list, lwarn := loadBlocklist(cfg)
	probe, pwarn := buildProbe(cfg)
	d := daemonConfig(probe, cfg, list.Domains(), log.New(os.Stderr, "", 0))

	fmt.Printf("config:       %s\n", cfgPath)
	fmt.Printf("hosts file:   %s\n", d.HostsPath)
	if d.BlockedNow() {
		fmt.Printf("state:        BLOCKED (%d domains sinkholed)\n",
			blockedDomainCount(d.HostsPath))
	} else {
		fmt.Println("state:        not blocked")
	}
	if d.DnsmasqConfDir != "" {
		fmt.Printf("dnsmasq:      %s (%v)\n", d.DnsmasqConfDir, platform.DnsmasqBlocked(d.DnsmasqConfDir))
	}
	start := time.Now()
	err := d.Probe.Run(context.Background())
	verdict := fmt.Sprintf("OK (%dms)", time.Since(start).Milliseconds())
	if err != nil {
		verdict = fmt.Sprintf("FAILED: %v", err)
	}
	fmt.Printf("proxy probe:  %s\n", verdict)
	for _, w := range []string{warn, lwarn, pwarn} {
		if w != "" {
			fmt.Println("warning:", w)
		}
	}
	return nil
}

func cmdBlock(cfgPath string, wantBlocked bool) error {
	cfg, warn := loadConfig(cfgPath)
	list, lwarn := loadBlocklist(cfg)
	d := daemonConfig(check.Invalid{}, cfg, list.Domains(), log.New(os.Stderr, "", log.LstdFlags))
	var err error
	if wantBlocked {
		err = d.Block()
	} else {
		err = d.Unblock()
	}
	if err != nil {
		return err
	}
	fmt.Printf("done: %d domains %s in %s\n", len(d.Domains),
		map[bool]string{true: "blocked", false: "unblocked"}[wantBlocked], d.HostsPath)
	for _, w := range []string{warn, lwarn} {
		if w != "" {
			fmt.Println("warning:", w)
		}
	}
	return nil
}

func blockedDomainCount(hostsPath string) int {
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		return 0
	}
	n := 0
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSuffix(ln, "\r"), "0.0.0.0 ") {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- install

const systemdUnitTmpl = `[Unit]
Description=ailock - sinkhole US AI providers while the proxy is down
After=network.target

[Service]
Type=simple
ExecStart={{.Bin}} -c {{.Cfg}} run
Restart=always
RestartSec=1

[Install]
WantedBy=multi-user.target
`

const procdInitTmpl = `#!/bin/sh /etc/rc.common

START=19
STOP=89
USE_PROCD=1

start_service() {
	procd_open_instance
	procd_set_param command {{.Bin}} -c {{.Cfg}} run
	procd_set_param respawn 3600 5 0
	procd_set_param stdout 1
	procd_set_param stderr 1
	procd_close_instance
}
`

const launchdTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.ailock.daemon</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Bin}}</string>
		<string>-c</string>
		<string>{{.Cfg}}</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardErrorPath</key>
	<string>/var/log/ailock.log</string>
</dict>
</plist>
`

func isOpenWrt() bool {
	_, err := os.Stat("/etc/openwrt_release")
	return err == nil
}

func hasSystemd() bool {
	fi, err := os.Stat("/run/systemd/system")
	return err == nil && fi.IsDir()
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeTemplate(tmpl, path string, perm os.FileMode, bin, cfg string) error {
	t := template.Must(template.New("t").Parse(tmpl))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	err = t.Execute(f, struct{ Bin, Cfg string }{bin, cfg})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// ensureConfigFiles creates the config directory, default config and
// blocklist if they do not exist yet (existing files are never touched).
func ensureConfigFiles(cfgPath string) error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := defaultAppConfig()
		if runtime.GOOS != "windows" && isOpenWrt() {
			// Router default: also cover the whole LAN via dnsmasq
			// wildcard rules. Point check.target at your proxy.
			cfg.DnsmasqConfDir = "/tmp/dnsmasq.d"
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(cfgPath, append(data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote default config %s — edit check.target to point at your proxy\n", cfgPath)
	}
	blPath := defaultBlocklistPath()
	if _, err := os.Stat(blPath); os.IsNotExist(err) {
		if err := os.WriteFile(blPath, embeddedBlocklist, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote default blocklist %s\n", blPath)
	}
	return nil
}

func copySelf(dest string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self == dest {
		return nil
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o755)
}

func cmdInstall(cfgPath string) error {
	if runtime.GOOS == "windows" {
		if err := ensureConfigFiles(cfgPath); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := svcwin.Install(exe, cfgPath); err != nil {
			return err
		}
		fmt.Println("ailock service installed and started")
		return nil
	}

	if err := ensureConfigFiles(cfgPath); err != nil {
		return err
	}

	binDir := "/usr/local/bin"
	if isOpenWrt() {
		binDir = "/usr/bin"
	}
	bin := filepath.Join(binDir, "ailock")
	if err := copySelf(bin); err != nil {
		return fmt.Errorf("copy binary to %s (need root?): %w", bin, err)
	}
	fmt.Printf("binary: %s\n", bin)

	switch {
	case isOpenWrt():
		if err := writeTemplate(procdInitTmpl, "/etc/init.d/ailock", 0o755, bin, cfgPath); err != nil {
			return err
		}
		if err := runCmd("/etc/init.d/ailock", "enable"); err != nil {
			return err
		}
		if err := runCmd("/etc/init.d/ailock", "restart"); err != nil {
			return err
		}
		fmt.Println("ailock procd service enabled and started (logs: logread)")
	case hasSystemd():
		if err := writeTemplate(systemdUnitTmpl, "/etc/systemd/system/ailock.service", 0o644, bin, cfgPath); err != nil {
			return err
		}
		if err := runCmd("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := runCmd("systemctl", "enable", "--now", "ailock.service"); err != nil {
			return err
		}
		fmt.Println("ailock.service enabled and started (journalctl -u ailock -f)")
	case runtime.GOOS == "darwin":
		if err := writeTemplate(launchdTmpl, "/Library/LaunchDaemons/com.ailock.daemon.plist", 0o644, bin, cfgPath); err != nil {
			return err
		}
		_ = runCmd("launchctl", "unload", "-w", "/Library/LaunchDaemons/com.ailock.daemon.plist")
		if err := runCmd("launchctl", "load", "-w", "/Library/LaunchDaemons/com.ailock.daemon.plist"); err != nil {
			return err
		}
		fmt.Println("ailock LaunchDaemon loaded (log: /var/log/ailock.log)")
	default:
		return fmt.Errorf("no supported init system found (systemd, procd/OpenWrt, launchd)")
	}
	fmt.Println("check it: ailock status")
	return nil
}

func cmdUninstall() error {
	switch {
	case runtime.GOOS == "windows":
		return svcwin.Uninstall()
	case isOpenWrt():
		if err := runCmd("/etc/init.d/ailock", "stop"); err != nil {
			return err
		}
		if err := runCmd("/etc/init.d/ailock", "disable"); err != nil {
			return err
		}
		return os.Remove("/etc/init.d/ailock")
	case hasSystemd():
		if err := runCmd("systemctl", "disable", "--now", "ailock.service"); err != nil {
			return err
		}
		if err := runCmd("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return os.Remove("/etc/systemd/system/ailock.service")
	case runtime.GOOS == "darwin":
		_ = runCmd("launchctl", "unload", "-w", "/Library/LaunchDaemons/com.ailock.daemon.plist")
		return os.Remove("/Library/LaunchDaemons/com.ailock.daemon.plist")
	default:
		return fmt.Errorf("no supported init system found")
	}
}
