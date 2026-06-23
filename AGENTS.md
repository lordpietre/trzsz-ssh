# trzsz-ssh (tssh) — Agent Guide

## Project
Drop-in OpenSSH client replacement with TUI login prompt, trzsz/zmodem file transfer, UDP mode (QUIC/KCP), and batch login. Go module `github.com/trzsz/trzsz-ssh`.

## Key commands
```
go build -o ./bin/ ./cmd/tssh
go test -v -count=1 ./tssh    # must run from repo root
make test                      # same as above (prefers `gotest` on unix)
make install                   # copies ./bin/tssh to /usr/bin/tssh
```

## Toolchain
- **Go 1.25** required (`go.mod` declares `go 1.25.0`)
- No linter or typecheck config exists (no `.golangci.yml`, no `go vet` in CI)
- CGO must be **disabled** for all builds (`CGO_ENABLED=0`)
- All forked dependencies live under `github.com/trzsz/` (go-arg, ssh_config, promptui, quic-go, kcp-go, smux, etc.)

## Entrypoints
- **Binary**: `cmd/tssh/main.go` → `tssh.TsshMain(argv)` in `tssh/main.go`
- **Library**: `tssh.SshLogin(args)` in `tssh/main.go`
- **Internal packages**: `internal/table/`, `internal/krb5/`, `internal/ssh/`

## Architecture notes
- All business logic lives in `tssh/` (66 `.go` files). Main package is thin.
- Version constant `kTsshVersion` in `tssh/version.go` (line 36)
- Exit codes defined in `tssh/comm.go` (e.g. `kExitCodeLoginFailed=16`)
- `#!!` prefix enables tssh-specific options in `~/.ssh/config` (standard ssh treats it as comment)
- `enc` prefix on config values means encrypted with `tssh --enc-secret`
- Android/Termux quirk: `cmd/tssh/main.go` adjusts `os.Args` when `TERMUX_EXEC__PROC_SELF_EXE` is set
- Keepalive defaults: `ServerAliveInterval=30` (was 0), configurable via `~/.tssh.conf` `defaultServerAliveInterval`. Set `#!! ServerAliveInterval 0` in `~/.ssh/config` to disable.
- `~/.tssh.conf` settings: `promptThemeLayout=tiny|simple|table`, `promptDefaultMode=search`, `promptPageSize=N`, `promptDetailItems=...`, `defaultServerAliveInterval=N`

## CI workflows (`.github/workflows/`)
| Workflow | Trigger |
|---|---|
| `gotest.yml` | push / PR (3 OS matrix + win7 compat build) |
| `pre-release.yml` | push to `main` (GoReleaser snapshot → "dev" release) |
| `publish.yml` | GitHub Release published (full multi-platform release) |

## Testing
- 13 test files in `tssh/`, 1 in `internal/table/`
- `-count=1` disables test caching; always run from repo root
- Integration-level tests depend on SSH server fixtures within the repo

## Release
- GoReleaser multi-platform: linux, darwin, windows, android, freebsd; 386/amd64/arm/arm64/loong64
- Excludes: windows/arm, darwin/arm, android/arm, android/386, android/amd64, freebsd/arm, freebsd/386
- Win7 builds use fork `github.com/thongtech/go-legacy-win7`
