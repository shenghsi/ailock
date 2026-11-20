# Contributing to ailock

Thanks for helping! This file covers the basics; the README explains what
ailock does and how the pieces fit together.

## Development setup

- Go ≥ 1.24
- No cgo, no external services — everything runs from a plain checkout.

    go build ./...      # must compile
    go test ./...       # unit tests; also run go test -race ./...
    go vet ./...
    gofmt -l .          # must print nothing
    bash build.sh       # cross-compiles all platforms into dist/

Platform-specific code is behind build tags (`internal/platform`,
`internal/svcwin`). A Linux-only build does **not** compile the Windows or
macOS files, so before touching them run:

    GOOS=windows go vet ./...
    GOOS=darwin go vet ./...

CI (Linux, Windows, macOS runners) will catch this too, but local is
faster.

## Ground rules

- **Never weaken the fail-closed property.** At startup ailock blocks
  before the first probe; a broken config must keep the block on; a dying
  daemon must not lift it. If your change can leave a state where AI
  domains resolve while the proxy is down, it is wrong.
- **Only the marked hosts region may be touched.** Everything outside
  `# BEGIN/END ailock (managed)` must survive byte-for-byte (see
  `internal/hostsedit` and its tests). If you touch that package, extend
  the tests first.
- **General connectivity must never depend on ailock.** Blocking applies
  to the blocklisted domains only; everything else keeps working with the
  daemon stopped, running, or crashed.
- Match the existing style: standard library first, small dependencies
  only (`golang.org/x/sys` is the sole one today), comment density like
  the surrounding code.

## Adding domains to the blocklist

The most common contribution. The blocklist is plain data
(`blocklist`, embedded into the binary at build time and copied to
`/etc/ailock/blocklist` at install). Rules:

1. **Bare domains only** — no schemes, no `*.wildcards`, no IP ranges.
   hosts files support none of these; subdomains must be listed
   explicitly. (dnsmasq mode wildcards them automatically on routers.)
2. **First-party domains of AI providers** that would see the user's IP.
   Skip third-party CDNs/analytics used by unrelated services too
   (e.g. don't sinkhole all of `sentry.io`).
3. Put them in the right `[section]`, or add a new lowercase section.
4. No duplicates (case-insensitive; CI/parser will dedupe but keep it
   tidy), one domain per line, `#` comments welcome.
5. Verify your addition parses: `go test ./internal/blocklist/` and
   `ailock check` after install.

A PR that only edits `blocklist` is a perfectly good PR.

## Pull requests

- Keep commits focused; style follows the log (`lowercase summary`,
  imperative).
- PRs must pass CI: gofmt clean, `go vet`, `go test -race` on all three
  OSes.
- New behavior needs a test — the daemon state machine and hosts editing
  have table/unit tests to extend.

By contributing you agree your contributions are licensed under the
project's [MIT license](LICENSE).
