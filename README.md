# ailock

English | [简体中文](README.zh-CN.md)

| | |
| --- | --- |
| Language | Go ≥ 1.24, stdlib + `golang.org/x/sys` only, no cgo |
| Platforms | Linux, OpenWrt (arm/mips/mipsle), Windows, macOS |
| Module | `github.com/shenghsi/ailock` |
| License | MIT |
| CI | Linux + Windows + macOS test matrix, 9-target cross-compile, release on `v*` tags |
| Agent docs | [AGENTS.md](AGENTS.md) — ground rules for AI coding agents |

A tiny always-on daemon that **sinkholes US AI provider domains whenever
your proxy/VPN is not running**, and lifts the block the moment it is.
Runs on Linux, Windows, macOS and OpenWrt (one Go binary per platform, no
runtimes, CGO-free). CLI output switches to 简体中文 automatically on
Chinese locales.

The problem it solves: with a proxy stopped (or crash-looping), clients
fall back to direct connections and expose your real IP to OpenAI,
Anthropic, Google, Microsoft, xAI and friends — risking account flags and
region blocks. ailock makes those domains *fail* instead of *leak*.
Everything else on the machine keeps working: only the listed domains are
affected, and only while the proxy is down.

## How it works

- A health probe (your proxy client's local port, an HTTP endpoint, or a
  shell command) runs every 2 seconds.
- First failure → write `0.0.0.0` / `::` entries for every blocklist
  domain into a **marked region of the hosts file**, flush DNS caches.
- 2 consecutive successes → remove the region, flush again.
- **Fail closed**: at startup the block is applied *before* the first
  probe (a healthy proxy is unblocked ~2s later); a stopped daemon leaves
  the current state in place (`ailock unblock` is the manual escape
  hatch); a broken config keeps the block on.
- On OpenWrt (or anywhere dnsmasq runs), an optional `dnsmasq_conf_dir`
  additionally maintains `address=/domain/` wildcard rules, giving the
  whole LAN true subdomain-wildcard coverage.

While the proxy is up, normal DNS/routing applies; the hosts region is
simply absent.

## Install

Build everything (`build.sh`) or grab a binary from the releases:

    bash build.sh          # → dist/ailock-{linux,windows,darwin}-{amd64,arm64,...}

Then, **as root** (sudo / Administrator):

    ./ailock install

That copies the binary to `/usr/local/bin` (OpenWrt: `/usr/bin`), writes
`/etc/ailock/config.json` + `/etc/ailock/blocklist` if missing (Windows:
`%ProgramData%\ailock\`), and registers + starts the OS service
(systemd / procd / launchd / Windows service).

### Installing a downloaded release binary

Grab the matching file from the [releases page][releases] (`uname -m`:
`x86_64` → amd64, `aarch64` → arm64) and run its `install` as root/admin —
it installs itself, so the downloaded file can be deleted afterwards.

Linux:

    curl -LO https://github.com/shenghsi/ailock/releases/latest/download/ailock-linux-amd64
    chmod +x ailock-linux-amd64
    sudo ./ailock-linux-amd64 install

OpenWrt: download or `scp` the right arch (`arm`, `mips`, `mipsle`) and
run `./ailock-linux-<arch> install` — installs to `/usr/bin`, registers a
procd service and defaults `dnsmasq_conf_dir` to `/tmp/dnsmasq.d` for
LAN-wide wildcard blocking.

macOS (Apple Silicon; Intel uses `-amd64`):

    curl -LO https://github.com/shenghsi/ailock/releases/latest/download/ailock-darwin-arm64
    chmod +x ailock-darwin-arm64
    sudo ./ailock-darwin-arm64 install
    # browser-downloaded binaries: clear the quarantine flag first:
    # xattr -d com.apple.quarantine ailock-darwin-arm64

Windows: download `ailock-windows-amd64.exe`, rename to `ailock.exe`, in
an elevated PowerShell run `.\ailock.exe install` (SmartScreen: *More
info → Run anyway* — the binary is unsigned).

After installing, **point the probe at your proxy** — edit
`/etc/ailock/config.json` (`%ProgramData%\ailock\config.json` on
Windows; see the table below) and restart the service so it is re-read:

    sudo systemctl restart ailock                        # Linux
    /etc/init.d/ailock restart                           # OpenWrt
    sudo launchctl kickstart -k system/com.ailock.daemon # macOS
    sc stop ailock && sc start ailock                     # Windows (elevated)

Until the probe target actually matches your proxy, ailock stays
**blocked** (fail-closed by design) — `ailock status` shows which side
you are on.

### Updating

    ailock update --check   # is there a newer release?
    sudo ailock update      # download, verify, replace the binary, restart

The update command queries the GitHub release for the asset matching
your OS/arch, verifies it against the release's SHA256SUMS, atomically
replaces the installed binary (fine while the service runs; on Windows
the old exe is renamed to `ailock.exe.old` first) and restarts the
service. Config and blocklist are never touched. If GitHub is not
reachable directly, set `https_proxy` first — the download honors proxy
environment variables.

The manual fallback still works: download the new release binary and run
its `install` again (atomic replace, config/blocklist kept, service
restarted). A release may ship an updated default blocklist: yours is
preserved; peek at the repo's `blocklist` file and delete
`/etc/ailock/blocklist` to regenerate the new default.

Uninstall: `ailock uninstall` (config and blocklist are kept).
Stop the service: `ailock stop` (root/admin). Stopping never touches the
hosts file — an active block stays on until `ailock unblock` (fail-closed).
Start it again with the restart commands above (`start` in place of
`restart`).
Manual override for testing: `ailock block` / `ailock unblock` / `ailock status`.

[releases]: https://github.com/shenghsi/ailock/releases

## Config (`/etc/ailock/config.json`)

```json
{
  "checks": [
    { "kind": "tcp",  "target": "127.0.0.1:10808", "timeout_sec": 1 },
    { "kind": "tcp",  "target": "127.0.0.1:7890",  "timeout_sec": 1 },
    { "kind": "exec", "target": "tailscale status", "timeout_sec": 2 }
  ],
  "lang": "auto",
  "interval_sec": 2,
  "unblock_after": 2,
  "hosts_file": "",
  "blocklist_file": "",
  "dnsmasq_conf_dir": ""
}
```

- `lang` — CLI/log language: `auto` (default) follows the locale, and on
  OpenWrt (which carries no locale environment) it follows the LuCI web
  UI language; set `en` or `zh` to force it.
- `check` — a single probe (simple setups). `checks` — an **any-of list**
  for machines running several clients: the proxy counts as up when *any*
  entry succeeds. Both may be combined.
- Probe kinds: `tcp` (connect), `http` (GET, 2xx = up), `exec` (shell
  command, exit 0 = up; `sh -c` on Unix, `cmd /C` on Windows).
- `dnsmasq_conf_dir`: e.g. `/tmp/dnsmasq.d` on OpenWrt.

### What to probe per client

| Client | check |
| --- | --- |
| sing-box / v2rayN (default mixed port) | `tcp 127.0.0.1:10808` |
| your own server on the LAN | `tcp 192.168.1.2:10808` |
| Clash / Mihomo / Clash Verge | `tcp 127.0.0.1:7890` (Verge often `7897`) |
| sing-box stock example config | `tcp 127.0.0.1:2080` |
| Xray / V2Ray core | `tcp 127.0.0.1:1080` or `10808` |
| Shadowsocks clients | `tcp 127.0.0.1:1080` |
| clash-compatible API liveness | `http http://127.0.0.1:9090/version` |
| Tailscale up | `exec tailscale status` (or `ip link show tailscale0`) |
| WireGuard up | `exec wg show interfaces` (non-empty output = up) |

Prefer the **proxy client's local port**: an interface being up (e.g.
`tailscale0`) does not mean traffic to AI providers is actually proxied —
use interface checks only as any-of companions when the proxy binds to
that tunnel.

## Blocklist (`/etc/ailock/blocklist`)

Plain text, one bare domain per line, `[section]` headers and `#` comments
allowed. Sections currently: `[openai]` `[anthropic]` `[google]` (wide:
accounts, YouTube, Gmail, APIs, static CDNs) `[ms-copilot]`
(+$ GitHub Copilot) `[xai-grok]` `[cursor]` `[doh-bypass]` (well-known
DoH endpoints, so DoH-configured browsers fall back to the system
resolver and hit the sinkhole).

The running daemon keeps the copy it loaded at startup: changes take
effect after a service restart (the manual `ailock block` / `ailock
unblock` commands re-read the file immediately). If the file is missing
or invalid, the **embedded copy** is used — a broken blocklist can never
silently disable the kill switch.

## Honest limitations

- **Leak window**: up to one `interval_sec` after the proxy dies, new
  resolutions still succeed directly; already-established connections
  persist. Defaults make this ~2s.
- **hosts has no wildcards**: dynamic-subdomain CDNs (googlevideo,
  googleusercontent) cannot be fully enumerated — the entry domains are
  blocked so clients fail early. dnsmasq mode (OpenWrt) has no such gap.
- **Apps with hardcoded DoH** bypass hosts files unless their resolver
  endpoint is sinkholed (`doh-bypass` section covers the common ones).
- **Existing connections** are not cut — blocking affects new lookups.
- Firewall IP-range blocking is deliberately out of scope: provider
  domains are CDN-fronted and IP lists would be both over- and
  under-broad.

## Quirks & troubleshooting

- **Binaries are unsigned.** Windows SmartScreen will warn on first run
  (*More info → Run anyway*); macOS Gatekeeper quarantines
  browser-downloaded files — clear it with
  `xattr -d com.apple.quarantine <file>` (or download with `curl`, which
  does not set the flag). Linux only needs `chmod +x`.
- **Where the logs are**: Linux `journalctl -u ailock -f`; OpenWrt
  `logread -f | grep ailock`; macOS `/var/log/ailock.log`; Windows
  `%ProgramData%\ailock\ailock.log` (services have no console, so stderr
  is redirected there).
- **Config is read once at startup.** After editing
  `config.json`/`blocklist`, restart the service (commands listed under
  "Installing a downloaded release binary"). Manual `ailock block` /
  `unblock` / `status` always re-read both files.
- **`dnsmasq_conf_dir` living on tmpfs is fine.** OpenWrt's default
  conf-dir `/tmp/dnsmasq.d` is RAM and wiped at reboot — that matches how
  OpenWrt builds dnsmasq config at every boot (generated config is
  tmpfs; UCI in `/etc` is the persistent truth). ailock recreates its
  address file on each boot as part of the fail-closed start, creates the
  directory if dnsmasq has not yet, and self-heals the file if it goes
  missing while blocked. Nothing there needs backing up; persistent
  state lives in `/etc/ailock`.
- **Everything looks blocked and won't lift?** Almost always the probe
  target: `ailock check` runs your configured probe once and prints the
  error. Until it passes, the block stays on — that is the fail-closed
  design, not a bug. `ailock unblock` is the escape hatch.
- **Blocked at boot for a few seconds** even with a healthy proxy: the
  daemon applies the block before its first probe (~2s to lift).
- **Windows: hosts edits need the service.** Run `ailock` commands from
  an elevated prompt when they touch the hosts file or the service.

## Testing the installation

On a Linux/systemd host the whole sequence below is automated:

    sudo ./scripts/selftest.sh [proxy-service] [test-domain]
    # e.g. sudo ./scripts/selftest.sh proxyctl youtube.com

It stops your proxy service, asserts the block appears and general
connectivity survives, restarts the proxy and asserts recovery; the
proxy is always restored, even on failure.

1. `ailock status` — probe OK, state not blocked, proxy running.
2. Stop your proxy (e.g. `sudo systemctl stop <proxy>`) → within ~3s
   `ailock status` shows BLOCKED; `ping chatgpt.com` / `getent hosts
   chatgpt.com` resolve to `0.0.0.0`. **Other sites still work** (curl
   something domestic) — general connectivity must survive.
3. Start the proxy → within ~4-6s the block lifts and AI sites resolve
   normally again.

## Development

    go build ./...     # build
    go test ./...      # unit tests (also run with -race)
    go vet ./...
    gofmt -l .         # must print nothing
    bash build.sh      # cross-compile all platforms → dist/

Layout:

    main.go                  CLI (run/install/stop/uninstall/status/check/block/unblock)
    internal/daemon          fail-closed toggle state machine
    internal/check           tcp/http/exec probes, any-of combinator
    internal/hostsedit       marked-region hosts rewriting (atomic, CRLF-safe)
    internal/blocklist       blocklist parser
    internal/platform        per-OS paths, DNS flush, dnsmasq emitter
    internal/svcwin          Windows service host
    blocklist                default blocklist data (embedded in the binary)
    build.sh                 cross-compile matrix → dist/

Platform notes: per-OS code lives behind build tags (`internal/platform`,
`internal/svcwin`); run `GOOS=windows go vet ./...` (and `GOOS=darwin`)
before touching those, since a plain Linux build does not compile the
tagged files. CI runs the test suite on Linux, Windows and macOS.
AI coding agents should start from [AGENTS.md](AGENTS.md) (invariants,
exact build/test commands, conventions).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) — including the rules for adding
domains to the blocklist, which is the most common contribution.

## License

[MIT](LICENSE)
