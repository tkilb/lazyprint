# lazyprint

A TUI frontend for CUPS printing and SANE scanning written in Go with Bubble Tea, modeled on `~/Git/lazytask` and `~/Git/para`.
`PLAN.md` defines the architecture, chunked delivery roadmap, and process rules.

## Commands

- Go 1.24 (`go.mod` declares `module github.com/tkilb/lazyprint`).
- Build: `make build` (compiles `./cmd/lazyprint` into git-ignored `./lazyprint`).
- Install: `make install` (builds and installs the binary to `~/bin`, which must be on `PATH`).
- Test: `make test` (`go test ./...`).
- Both `go build ./...` and `go test ./...` must pass cleanly before any chunk checkpoint.

## Testing Policy

- **Engine & Parser Packages:** Table-driven unit tests for parsers (`internal/cups`, `internal/sane`), runner isolation (`internal/runner`), and config loading (`internal/config`).
- **Hardware Isolation:** Automated tests must never execute real hardware commands (`lpstat`, `cancel`, `lpoptions`, `lp`, `lpadmin`, `scanimage`). All system commands are routed through the `internal/runner.Runner` interface and mocked in tests using stubbed outputs.
- **UI Packages:** UI packages (`cmd/lazyprint`, `internal/ui/*`) do not require brittle terminal snapshot tests. Each UI chunk ships with a concrete manual QA note describing exact keystrokes and expected on-screen behavior.

## Workflow & Process

- **Chunked Delivery:** Work is broken into small, independently testable chunks (~150–250 lines changed across ~4–6 files).
- **Mandatory Human Checkpoints:**
  1. Announce before starting a chunk.
  2. Deliver that single chunk only — no speculative work or pre-building future chunks.
  3. Stop, report files touched, test results, and manual QA steps.
  4. Wait for explicit human approval before proceeding.
- **No Automated Commits:** The user reviews diffs and commits manually. Never commit or suggest committing changes.
- **Styling Parity:** Follow `lazytask` styling conventions (`paletteGreen = "2"`, `paletteBlue = "4"`, orange `"214"`, red `"196"`). Frame titles must be embedded directly in the top border line (e.g. `╭─ 1 Printers ──────╮`) to conserve vertical space.
- **Agent Instructions:** Agent instructions live in this file and `PLAN.md`; never create a `CLAUDE.md`.
