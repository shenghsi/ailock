# AGENTS.md

Ground rules for AI coding agents working in this repo. Human
contributors: see CONTRIBUTING.md. Full docs: README.md (English) and
README.zh-CN.md (Chinese — keep both in sync when changing documented
behavior).

## What this is

ailock is a fail-closed hosts-file kill switch: when the configured
proxy health check fails, US AI provider domains are sinkholed via a
marked region of the OS hosts file (plus optional dnsmasq wildcards);
when the proxy recovers, the block lifts. One Go binary, cross-compiled
for Linux (incl. OpenWrt), Windows, macOS.

## Invariants — do not break these

1. **Fail closed.** The daemon blocks *before* its first probe at
   startup; a broken/missing config must keep the block on (embedded
   fallbacks); a dying daemon must never lift the block. Any change
   that can leave AI domains resolvable while the proxy is down is
   wrong.
2. **Only the marked hosts region may be touched.** Everything outside
   `# BEGIN/END ailock (managed)` must survive byte-for-byte, including
   line-ending style. Extend `internal/hostsedit` tests before touching
   that package.
3. **General connectivity must never depend on ailock.** Only
   blocklisted domains may be affected; everything else keeps working
   with the daemon stopped, running, or crashed.
4. **User state is precious.** `install`/`update` never overwrite an
   existing `/etc/ailock/config.json` or `blocklist`.

## Build / test / lint (exact commands)

    go build ./...
    go test ./...          # also: go test -race ./...
    go vet ./...
    gofmt -l .             # must print nothing
    bash build.sh          # cross-compile all platforms → dist/

Platform code is behind build tags; a plain Linux build does NOT compile
them, so before touching `internal/platform` or `internal/svcwin` run:

    GOOS=windows go vet ./...
    GOOS=darwin go vet ./...

CI runs the matrix on ubuntu/windows/macos. Known vet gotcha: the
non-constant-format check fires on printf/Errorf/logf calls with no
variadic args — wrap such strings as `fmt.Printf("%s", i18n.T(...))`.

## Conventions

- **Every user-facing string is bilingual**, wrapped in
  `i18n.T("english", "中文")` (`internal/i18n`). Use positional verbs
  (`%[1]s`) when word order differs between languages. Locale sources:
  env (LC_ALL/LC_MESSAGES/LANG/LANGUAGE), LuCI UI language on OpenWrt,
  Windows preferred UI languages; `"lang"` in config.json forces it.
- **The blocklist is data**, not code (`blocklist` at repo root,
  embedded via go:embed): bare domains only (no schemes/wildcards),
  first-party AI-provider domains, `[section]` headers, `#` comments.
  A blocklist-only change needs no code change.
- Windows has no POSIX permission bits (Chmod toggles read-only only);
  keep mode assertions platform-aware in tests.
- Updates replace binaries atomically (rename-over; on Windows the
  running exe is renamed to `.old`). Never open a running executable
  for in-place writing (ETXTBSY on Linux).

## Release process

Bump `const version` in main.go, commit, tag `vX.Y.Z`, push the tag —
CI builds all binaries, attaches them plus SHA256SUMS to the GitHub
release. `ailock update` consumes exactly those assets, so dist naming
(`ailock-<goos>-<goarch>` from build.sh) must keep matching
`selfupdate.AssetName`.
