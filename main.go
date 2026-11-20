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
	"github.com/shenghsi/ailock/internal/i18n"
	"github.com/shenghsi/ailock/internal/platform"
	"github.com/shenghsi/ailock/internal/selfupdate"
	"github.com/shenghsi/ailock/internal/svcwin"
)

//go:embed blocklist
var embeddedBlocklist []byte

const version = "0.2.0"

const usageEn = `ailock — sinkhole US AI providers while the proxy is down

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
  update     download the latest release over this binary and restart the
             service (--check to only look)
  version    print the version

The default config path is /etc/ailock/config.json (%ProgramData%\ailock on
Windows). If the config or blocklist file is missing, embedded defaults are
used, so a broken config can never silently disable the kill switch.`

const usageZh = `ailock — 代理断开时黑洞（熔断）美国 AI 服务商域名

用法：
  ailock [-c 配置文件] 命令

命令：
  run        前台运行守护进程（系统服务执行的即是此命令）
  install    写入默认配置/屏蔽列表并注册系统服务
  uninstall  停止并移除系统服务（保留配置与屏蔽列表）
  status     查看屏蔽状态，并执行一次健康探测
  check      执行一次健康探测并报告结果
  block      立即屏蔽（手动/测试用）
  unblock    立即解除屏蔽（手动逃生通道）
  update     从 GitHub 下载最新版本并替换自身、重启服务（--check 仅检查）
  version    打印版本号

默认配置路径为 /etc/ailock/config.json（Windows 为
%ProgramData%\ailock\config.json）。配置或屏蔽列表缺失时自动使用内置
默认值——损坏的配置绝不会让熔断保护悄悄失效。`

func usageText() string { return i18n.T(usageEn, usageZh) }

func main() {
	fs := flag.NewFlagSet("ailock", flag.ContinueOnError)
	cfgPath := fs.String("c", defaultConfigPath(), "config file path")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	args := fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usageText())
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
	case "update":
		err = cmdUpdate(args[1:])
	case "version":
		fmt.Println("ailock", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s\n", args[0], usageText())
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
	Lang           string         `json:"lang"`   // "auto" (default), "en", "zh"
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
		Lang:         "auto",
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
			return cfg, fmt.Sprintf(i18n.T("config %s missing — using embedded defaults",
				"配置 %s 不存在——使用内置默认值"), path)
		}
		return cfg, fmt.Sprintf(i18n.T("config %s unreadable (%v) — using embedded defaults",
			"配置 %s 无法读取（%v）——使用内置默认值"), path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		cfg = defaultAppConfig()
	}
	i18n.SetForced(cfg.Lang)
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
		return mustEmbedded(), fmt.Sprintf(i18n.T("blocklist %s invalid (%v) — using embedded default",
			"屏蔽列表 %s 无效（%v）——使用内置默认列表"), path, perr)
	}
	return mustEmbedded(), fmt.Sprintf(i18n.T("blocklist %s missing/unreadable (%v) — using embedded default",
		"屏蔽列表 %s 不存在或无法读取（%v）——使用内置默认列表"), path, err)
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
			return check.Invalid{Err: fmt.Errorf("%s: %w", i18n.T("check invalid", "检测配置无效"), err)},
				fmt.Sprintf(i18n.T("check invalid (%v) — staying blocked (fail closed)",
					"check 配置无效（%v）——保持屏蔽（故障关闭）"), err)
		}
		probes = append(probes, single)
	}
	for i := range cfg.Checks {
		c := cfg.Checks[i].WithDefaults()
		if err := c.Validate(); err != nil {
			return check.Invalid{Err: fmt.Errorf("%s: %w",
					fmt.Sprintf(i18n.T("checks[%d] invalid", "checks[%d] 配置无效"), i), err)},
				fmt.Sprintf(i18n.T("checks[%d] invalid (%v) — staying blocked (fail closed)",
					"checks[%d] 配置无效（%v）——保持屏蔽（故障关闭）"), i, err)
		}
		probes = append(probes, c)
	}
	if len(probes) == 0 {
		return check.Invalid{Err: errors.New(i18n.T("no check configured", "未配置任何健康检测"))},
			i18n.T("no check/checks configured — staying blocked (fail closed)",
				"未配置 check/checks——保持屏蔽（故障关闭）")
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
		fmt.Printf(i18n.T("proxy check FAILED: %v\n", "代理检测失败：%v\n"), err)
		if warn != "" || pwarn != "" {
			fmt.Println(i18n.T("note: ", "提示："), firstNonEmpty(warn, pwarn))
		}
		return err
	}
	fmt.Printf(i18n.T("proxy check OK (%.0fms)\n", "代理检测正常（%.0fms）\n"),
		float64(time.Since(start).Microseconds())/1000)
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

	fmt.Printf(i18n.T("config:       %s\n", "配置文件：    %s\n"), cfgPath)
	fmt.Printf(i18n.T("hosts file:   %s\n", "hosts 文件：  %s\n"), d.HostsPath)
	if d.BlockedNow() {
		fmt.Printf(i18n.T("state:        BLOCKED (%d domains sinkholed)\n",
			"状态：        已屏蔽（黑洞了 %d 个域名）\n"),
			blockedDomainCount(d.HostsPath))
	} else {
		fmt.Println(i18n.T("state:        not blocked", "状态：        未屏蔽"))
	}
	if d.DnsmasqConfDir != "" {
		fmt.Printf(i18n.T("dnsmasq:      %s (%v)\n", "dnsmasq：     %s（%v）\n"),
			d.DnsmasqConfDir, platform.DnsmasqBlocked(d.DnsmasqConfDir))
	}
	start := time.Now()
	err := d.Probe.Run(context.Background())
	verdict := fmt.Sprintf(i18n.T("OK (%dms)", "正常（%dms）"), time.Since(start).Milliseconds())
	if err != nil {
		verdict = fmt.Sprintf(i18n.T("FAILED: %v", "失败：%v"), err)
	}
	fmt.Printf(i18n.T("proxy probe:  %s\n", "代理探测：    %s\n"), verdict)
	for _, w := range []string{warn, lwarn, pwarn} {
		if w != "" {
			fmt.Println(i18n.T("warning: ", "警告："), w)
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
	action := map[bool]string{
		true:  i18n.T("blocked (entries written)", "写入屏蔽条目"),
		false: i18n.T("unblocked (entries removed)", "移除屏蔽条目"),
	}[wantBlocked]
	fmt.Printf(i18n.T(
		"done: %[2]s %[1]d domains in %[3]s\n",
		"完成：已在 %[3]s 中%[2]s %[1]d 个域名\n"),
		len(d.Domains), action, d.HostsPath)
	for _, w := range []string{warn, lwarn} {
		if w != "" {
			fmt.Println(i18n.T("warning: ", "警告："), w)
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

// ---------------------------------------------------------------- update

func cmdUpdate(args []string) error {
	checkOnly := false
	for _, a := range args {
		if a == "--check" || a == "-check" || a == "check" {
			checkOnly = true
			continue
		}
		fmt.Fprintf(os.Stderr, "unknown argument %q for update\n", a)
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	rel, err := selfupdate.Latest(ctx)
	if err != nil {
		return err
	}
	fmt.Printf(i18n.T("current: %s, latest: %s\n", "当前版本：%s，最新版本：%s\n"), version, rel.Tag)
	if !selfupdate.Newer(rel.Tag, version) {
		fmt.Println(i18n.T("already up to date", "已是最新版本"))
		return nil
	}
	if checkOnly {
		fmt.Println(i18n.T("newer release available — run 'ailock update' as root/admin to apply",
			"有新版本——以 root/管理员身份运行 ailock update 应用"))
		return nil
	}

	asset := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	url, ok := rel.Assets[asset]
	if !ok {
		return errors.New(fmt.Sprintf(i18n.T("release %s has no asset %s — update manually",
			"release %s 没有适用于本平台的文件 %s——请手动更新"), rel.Tag, asset))
	}
	fmt.Println(i18n.T("downloading...", "下载中…"))
	data, err := selfupdate.Download(ctx, url)
	if err != nil {
		return err
	}
	fmt.Printf(i18n.T("downloaded %s (%.1f MiB)\n", "已下载 %s（%.1f MiB）\n"), asset, float64(len(data))/(1<<20))

	if sumsURL, ok := rel.Assets["SHA256SUMS"]; ok {
		sums, serr := selfupdate.Download(ctx, sumsURL)
		if serr != nil {
			fmt.Println(i18n.T("warning: SHA256SUMS unreadable, skipping checksum",
				"警告：无法读取 SHA256SUMS，跳过校验"))
		} else if verr := selfupdate.VerifyChecksum(sums, asset, data); verr != nil {
			return errors.New(fmt.Sprintf(i18n.T("refusing to install: %v", "拒绝安装：%v"), verr))
		} else {
			fmt.Println(i18n.T("checksum verified", "校验和已通过"))
		}
	} else {
		fmt.Println(i18n.T("note: release ships no SHA256SUMS, skipping checksum",
			"提示：该 release 未附带 SHA256SUMS，跳过校验"))
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := selfupdate.ReplaceExecutable(self, data); err != nil {
		return err
	}
	fmt.Printf(i18n.T("updated %s → %s\n", "已更新 %s → %s\n"), version, selfupdate.NormalizeTag(rel.Tag))

	if err := restartInstalledService(); err != nil {
		fmt.Println(i18n.T("warning: service not restarted, start it manually: ",
			"警告：服务未重启，请手动启动："), err)
	} else {
		fmt.Println(i18n.T("service restarted onto the new version", "服务已在新版本上重启"))
	}
	return nil
}

func restartInstalledService() error {
	switch {
	case runtime.GOOS == "windows":
		return svcwin.RestartService()
	case isOpenWrt():
		return runCmd("/etc/init.d/ailock", "restart")
	case hasSystemd():
		return runCmd("systemctl", "restart", "ailock.service")
	case runtime.GOOS == "darwin":
		return runCmd("launchctl", "kickstart", "-k", "system/com.ailock.daemon")
	default:
		return errors.New(i18n.T("no supported init system found", "未找到支持的 init 系统"))
	}
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
		fmt.Printf(i18n.T("wrote default config %s — edit check.target to point at your proxy\n",
			"已写入默认配置 %s——请把 check.target 改成你的代理地址\n"), cfgPath)
	}
	blPath := defaultBlocklistPath()
	if _, err := os.Stat(blPath); os.IsNotExist(err) {
		if err := os.WriteFile(blPath, embeddedBlocklist, 0o644); err != nil {
			return err
		}
		fmt.Printf(i18n.T("wrote default blocklist %s\n", "已写入默认屏蔽列表 %s\n"), blPath)
	}
	return nil
}

// copySelf replaces dest with the running binary. The write is atomic
// (temp file + rename): overwriting a *running* binary in place fails
// with ETXTBSY on Linux, while renaming over it is fine — this is what
// makes `ailock install` work as an updater.
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
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".ailock-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
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
		fmt.Println(i18n.T("ailock service installed and started", "ailock 服务已安装并启动"))
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
		return fmt.Errorf("%s: %w", fmt.Sprintf(i18n.T("copy binary to %s (need root?)",
			"复制二进制到 %s 失败（需要 root 权限？）"), bin), err)
	}
	fmt.Printf(i18n.T("binary: %s\n", "二进制： %s\n"), bin)

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
		fmt.Println(i18n.T("ailock procd service enabled and started (logs: logread)",
			"ailock procd 服务已启用并启动（日志：logread）"))
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
		// enable --now starts only inactive units: an update must also
		// restart the service onto the freshly copied binary.
		if err := runCmd("systemctl", "restart", "ailock.service"); err != nil {
			return err
		}
		fmt.Println(i18n.T("ailock.service enabled and started (journalctl -u ailock -f)",
			"ailock.service 已启用并启动（journalctl -u ailock -f 查看日志）"))
	case runtime.GOOS == "darwin":
		if err := writeTemplate(launchdTmpl, "/Library/LaunchDaemons/com.ailock.daemon.plist", 0o644, bin, cfgPath); err != nil {
			return err
		}
		_ = runCmd("launchctl", "unload", "-w", "/Library/LaunchDaemons/com.ailock.daemon.plist")
		if err := runCmd("launchctl", "load", "-w", "/Library/LaunchDaemons/com.ailock.daemon.plist"); err != nil {
			return err
		}
		fmt.Println(i18n.T("ailock LaunchDaemon loaded (log: /var/log/ailock.log)",
			"ailock LaunchDaemon 已加载（日志：/var/log/ailock.log）"))
	default:
		return errors.New(i18n.T("no supported init system found (systemd, procd/OpenWrt, launchd)",
			"未找到支持的 init 系统（systemd、procd/OpenWrt、launchd）"))
	}
	fmt.Println(i18n.T("check it: ailock status", "验证：ailock status"))
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
		return errors.New(i18n.T("no supported init system found", "未找到支持的 init 系统"))
	}
}
