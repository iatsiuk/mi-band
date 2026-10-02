# MIBAND Project Instructions

## Scope

- `miband` reads Xiaomi Smart Band data only through the Xiaomi health cloud REST API, where Mi Fitness uploads it
- Keep the binary pure Go: no Bluetooth stack, no cgo (`CGO_ENABLED=0` must build)
- Gadgetbridge is forbidden in any form: no source, no ports, no knowledge of its code; derive protocol facts only from the decompiled Mi Fitness app, the band firmware, and live traffic

## Project Files

- `research/notes/` - protocol research from the decompiled Mi Fitness app and firmware; `70-cloud-data.md` is the spec for the cloud API used by `internal/cloud`, `20-fitness.md` and `21-workout.md` for the binary files parsed by `internal/fitness`; the other notes cover the Bluetooth path that is not implemented
- `research/golden/cloud/run.sh` - regenerates `internal/cloud/testdata/golden*.json` by running the app's own Java classes in Docker
- `research/golden/fitness/gen.sh` - regenerates `internal/fitness/tables_gen.go` and `internal/fitness/testdata/*.json` the same way
- `research/` is local and git-ignored as a whole (also `research/apk/`, `research/src/` jadx output, `research/golden/*/work/` dex2jar jars, `research/fw/` unpacked firmware); a fresh clone has none of it, so the committed testdata is the only copy of the golden vectors

## Package Structure

- `cmd/miband` - cobra CLI
  - `root.go` - root command, global flags (`--format`, `--region`, `--user`, `--user-id`, `--pass-token`, `--verbose`), exit codes (0 ok, 1 error, 3 auth, 130 interrupted), `exitCode`
  - `session.go` - saved session in `os.UserConfigDir()/miband/cloud.json` (mode 0600, atomic `writeSecret`), `cloudFlags.login` (explicit credentials or saved session), `client` with one pass-token re-login on `cloud.ErrUnauthorized` (`withRetry`)
  - `echo_unix.go` (darwin, linux; ioctl numbers in `echo_darwin.go`, `echo_linux.go`) - password prompt turns echo off via termios before the cancelable read starts and restores it on return; `echo_other.go` falls back to `term.ReadPassword`
  - `login.go`, `logout.go`, `devices.go`, `data.go` - subcommands; `logout` ends the server session (`Session.Logout`), then removes `cloud.json`; `login` prompts for region, account, password and email code that no flag gives (`askMissing`, `verifyEmail`); `data.go` builds a `dataResult` with sections in `--kind` order
  - `summary.go` - device filter, tombstone filter, steps per device and local day
  - `output.go`, `data_output.go` - `--format` rendering: `markdown` (default) via the `markdowner` interface and the `table` helper, `json` via `encoding/json`; `dataResult.MarshalJSON` keeps the flat JSON shape (`from`, `to`, one key per section, `partial`)
- `internal/cloud` - Xiaomi account and health cloud client
  - `passport.go` - account.xiaomi.com login (`Login`, `LoginWithPassToken`, `Resume`, `Session.Logout`), `Session`, verification and captcha errors
  - `identity.go` - email code security check behind `NotificationError` (`SendEmailCode`, `EmailVerification.Finish`) via a cookie jar, then `LoginWithPassToken`
  - `sign.go` - RC4-encrypted, SHA1-signed requests (`nonce`, `signedNonce`, `rc4_hash__`, `signature`); GET signs only `data`, `locale` stays plaintext
  - `client.go` - `Options` (region host `https://<region>.hlth.io.mi.com`, locale, pseudo device id, test URLs, `Logf`)
  - `devices.go` - device list with paging, shared `request`/`call` envelope handling
  - `data.go` - `FitnessData` (records by key), `Workouts`, `Workout.FileID`
  - `values.go` - `Record.Decode` into `Step`, `Calories`, `HeartRate`, `SpO2`, `Stress`, `Sleep`
  - `fds.go` - `RawFile` download of workout detail files and `ParseRawFile` (AES-CBC decrypt, then `internal/fitness`)
  - `errors.go` - `requestError` without URLs or secrets in messages
- `internal/fitness` - pure parsers of the band's binary fitness files (`ParseFile`, `ParseBody`, `ParseID`): daily records and reports, sleep, measurements, sport record and report, GPS, recover rate; `tables_gen.go` is generated, do not edit

## Code Style

### Imports

Group imports in order, separated by blank lines:
1. Standard library
2. External packages
3. Local packages (`miband/...`)

```go
import (
    "context"
    "fmt"

    "github.com/spf13/cobra"

    "miband/internal/cloud"
)
```

### Naming

- Package names: short, lowercase, no underscores (`cloud`, `fitness`)
- Exported types: PascalCase (`Session`, `Record`, `Workout`)
- Unexported: camelCase (`writeSecret`, `stepsByDay`)
- Acronyms: consistent case (`URL`, `HTTP`, `DID` or `url`, `http`, `did`)
- Receivers: short, 1-2 letters (`s` for `*Session`, `r` for `*Record`)
- Errors: `Err` prefix for sentinel errors (`ErrUnauthorized`)

### Functions

- Max 80 lines, 50 statements (enforced by `funlen` linter)
- Max cyclomatic complexity: 10 (enforced by `cyclop` linter)
- Max nesting depth: 5 (enforced by `nestif` linter)
- Early returns for error handling
- Group related functions together

### Error Handling

- Wrap errors with context: `fmt.Errorf("operation: %w", err)`
- Check all errors (enforced by `errcheck` linter)
- Use `errors.Is`/`errors.As` for error comparison
- Sentinel errors as package-level variables
- Never put tokens, passwords, cookies or signed URLs into error messages or logs

### Comments

- Only for non-obvious logic
- English, lowercase, brief
- No comments for self-explanatory code
- `//nolint:<linter> // <reason>` only on the exact line, e.g. weak crypto required by the Xiaomi protocol or an intentional two's-complement wire conversion

### Structs

- JSON tags on all exported fields: `json:"field_name"`
- Use `omitempty` for optional fields
- Pointer types for optional values (`*float64`, `*int`)
- Group related fields together

### Variables

- Package-level constants in `const` block
- Related constants grouped together
- Unexported package variables with `var`

### Control Flow

- Use `range` with index for modifying slices
- Prefer `for i := range n` over `for i := 0; i < n; i++` (Go 1.22+)
- Use `switch` over long `if-else` chains

### Concurrency

- Use `context.Context` as first parameter
- Use `sync.Mutex` for simple locking
- Use `errgroup` for parallel operations

## Testing

- Use table-driven tests for multiple scenarios
- Use stdlib `testing` package only (no testify)
- Test error paths: timeouts, context cancellation
- Run with race detector: `go test -race ./...`
- Use `t.Parallel()` for independent tests; tests that set `HOME` with `t.Setenv` cannot be parallel
- Test files: `*_test.go` in same package
- CLI tests run the cobra root command (`execute`/`executeIn` in `cmd/miband/root_test.go`) with an isolated `HOME`; login tests point `testServer` at an `httptest` account server
- Cloud tests use `httptest` servers via `Options.AccountURL`/`HealthURL`; signing and parser tests compare against golden vectors produced by the app's Java code
- Tests never reach the real Xiaomi cloud and never contain real account data

## Language

All documentation, comments, and text must be in English.

## Building

- Always build with `make build` (runs linter automatically)
- Direct `go build` skips linting - avoid it
- Run tests with `make test` (race detector, writes `coverage.out`)

## Linting and Formatting

- Run `golangci-lint run` before committing (executed automatically via `make build`)
- Fix formatting issues with `goimports -w <file>` or `gofmt -w <file>`
- Config: `.golangci.yml` defines enabled linters
- No trailing whitespace, proper import grouping (stdlib, external, local)
