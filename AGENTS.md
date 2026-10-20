# Repository Guidelines

## Project Structure & Module Organization

- `cmd/hust-mail-cli/main.go`: executable entry point, signal handling, and exit codes.
- `pkg/cmd/root/`: Cobra command construction, flags, configuration precedence, and command tests.
- `internal/config/`: JSON configuration loading and validation.
- `internal/mailarchive/`: read-only IMAP receiving, local storage, and local message reading.
- Tests live beside their implementation in `*_test.go`. `config.example.json` contains placeholder settings; `README.md` documents user workflows.

Keep protocol and storage logic outside Cobra handlers. Commands include `sync`, `folders`, `list`, `read`, `search`, and `attachments list/export`; SMTP is deferred. Local MIME operations must preserve source messages, avoid network access, and prevent attachment path traversal or overwrites. `read --raw` preserves original bytes; default `read` shows decoded text. Sync recovery must preserve `.eml` and live locks, and never unlink the persistent `.sync.flock` inode.

Limit protocol support to IMAP for receiving and SMTP for sending. POP3 is out of scope: do not add or retain it as a compatibility option. IMAP must remain read-only. SMTP implementation remains deferred; do not enable IMAP writes as part of SMTP work.

Search indexes are disposable derived data: verify source fingerprints before reuse, preserve `--no-index` for read-only archives, and bump `searchIndexVersion` when decoded search semantics change.

Target Linux/macOS, excluding Windows. Preserve `CGO_ENABLED=0` builds. Cross-compile with `sh scripts/build-platforms.sh`; report compilation separately from target runtime validation.

## Build, Test, and Development Commands

Use Go 1.26 or newer, as specified in `go.mod`.

`make` builds `bin/hust-mail-cli`; `make install` uses GOBIN or GOPATH/bin. Use `make help` for targets, `make check` for lint/race tests, and `make cross-build` for platform builds.

```sh
go build -o hust-mail-cli ./cmd/hust-mail-cli # Build the executable
./hust-mail-cli --help                      # Inspect commands and flags
go test -race ./...                         # Run tests with race detection
go vet ./...                               # Run static checks
gofmt -w cmd internal pkg                   # Format Go source
git diff --check                           # Check whitespace errors
```

If sandbox restrictions prevent default build-cache writes, set `GOCACHE=/private/tmp/hust-mail-cli-go-build`.

## Coding Style & Naming Conventions

Use `gofmt` formatting and tab indentation. Follow Go naming conventions: lowercase package names, `MixedCaps` identifiers, and descriptive filenames such as `imap_test.go`. Construct independent Cobra command trees through `NewCmdRoot`; return errors from `RunE`, propagate contexts, and use command output writers. Avoid logging credentials or including secret configuration values in errors.

## Testing Guidelines

Use Go's standard `testing` package and `TestBehavior` function names. Protocol tests use `net.Pipe` peers that reject commands outside the receiving allowlist. Cover protocol changes, configuration precedence, incremental downloads, UID identity, and interrupted writes. Use temporary directories and isolated environments; tests must not require real mailbox credentials. No numeric coverage threshold is configured. Report real-account verification separately from automated tests.

## Commit & Pull Request Guidelines

History currently contains only `Initial commit`, so no established commit convention exists. Use concise, imperative subjects and focused changes. PR descriptions should explain behavior, relevant issues, validation commands, and limitations. Update documentation and configuration examples when interfaces change. Do not stage, commit, push, or open a PR without explicit authorization; preserve unrelated local changes.

## Security & Configuration

Default configuration is `~/.config/hust-mail-cli/config.json`, including on macOS. Precedence is explicit flags, supported environment variables, configuration, then defaults. Keep credential files at `0600`; never commit passwords or real `.eml` files. Preserve verified TLS, IMAP `EXAMINE`/`BODY.PEEK[]`, and atomic archive writes.

`days`/`--days` limits new IMAP downloads by server `INTERNALDATE`, with zero meaning all time. Preserve explicit zero flag overrides and exact cutoff checks after coarse date searches. Do not automatically delete archived mail or treat sender-provided dates as receive timestamps.
