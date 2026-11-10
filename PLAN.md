# lazyprint — Chunked Delivery Requirements & Plan

Status: **Scaffolding & Feature Grilling Phase**

This document establishes the architecture, process rules, testing standards, and chunked delivery roadmap for **`lazyprint`**, following the exact delivery methodology of [`lazytask`](../lazytask/PLAN.md).

Work is broken into small, independently-testable chunks delivered **one chunk at a time with mandatory human checkpoints between chunks.**

---

## 1. Project Summary

A TUI frontend for **CUPS printing and SANE scanning**, written in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea), with a [lazygit](https://github.com/jesseduffield/lazygit)-inspired multi-panel layout.

`lazyprint` shells out to standard Linux/macOS printing and scanning CLI binaries (`lpstat`, `lp`, `cancel`, `lpoptions`, `scanimage`) rather than linking against native C libraries (`libcups`, `libsane`), ensuring zero CGO dependencies and seamless portability.

---

## 2. Tech Stack

- **Language:** Go 1.24
- **Module Path:** `github.com/tkilb/lazyprint`
- **TUI Framework:** Charm stack (`bubbletea`, `bubbles`, `lipgloss`)
- **System Integration:** Shell out to system binaries:
  - CUPS: `lpstat`, `cancel`, `lpoptions`, `lp`, `lpadmin` (optional)
  - SANE / Driverless: `scanimage`, `driverless`, `airscan-discover`
- **Config Format:** YAML (`go.yaml.in/yaml/v3`)
- **Testing:** Standard `testing` package + `stretchr/testify` (table-driven tests, isolated fixture parsing, command runner interfaces for mockable CLI execution).

---

## 3. Testing Bar (Per Chunk)

Every chunk must ship with:
- **Unit tests:** Table-driven tests for all parsing logic (`lpstat -p -d`, `lpstat -o`, `scanimage -L`), UI state transitions, and flag/config parsing.
- **Isolated CLI Runners:** An internal execution interface (`Runner`) so tests never execute real hardware commands on the developer's system unless explicitly tagged as integration tests.
- **Manual QA Note:** Concrete steps telling the human reviewer exactly how to run and verify the chunk interactively.

---

## 4. Human-in-the-Loop Checkpoints (Mandatory)

The driving agent must treat every chunk as a **hard stop boundary**:
1. **Announce before starting** — State the single chunk being tackled and its explicit objective.
2. **Deliver that chunk only** — No unrequested additions or pre-building future chunks.
3. **Stop and report** — Display files touched, test results (`go test ./...`), and manual QA verification steps.
4. **Wait for explicit human sign-off** — Never assume approval.
5. **No automated commits** — The user reviews the diff and commits manually.

---

## 5. Chunking Rules

- **One objective per chunk.**
- **Bounded diff size:** Target ~150–250 lines changed across ~4–6 files.
- **Green builds:** Each chunk must build and pass tests cleanly on its own.

---

## 6. Feature Specifications (Grilling in Progress)

- **Printer Configuration & Management:**
  - View configured printers and discover network printers (`driverless`).
  - **Alias Rename (Approach A - System-Level):** True CUPS rename by querying existing queue URI/options via `lpstat -v`/`lpoptions`, creating the new queue name via `lpadmin -p <new> -E -v <uri> -m everywhere`, transferring default destination status, and removing the old queue (`lpadmin -x <old>`). Shows confirmation popup.
  - Set default printer destination (`lpoptions -d` / `lpadmin -d`).
- **Job & Queue Management:**
  - Real-time monitor of active and pending print jobs (`lpstat -o`).
  - Cancel specific job (`x`), purge all jobs (`X`).
- **Scanning Workflows:**
  - Scanner auto-discovery (`scanimage -L` / `sane-airscan`).
  - Interactive scan modal: Flatbed vs. ADF, resolution picker (e.g., 150/300/600 DPI), output format (PDF vs. image: PNG/JPEG).
  - **Autosave & Path Override:** Default autosave with timestamp (`scan_YYYYMMDD_HHMMSS.<ext>`), with an optional path/filename override field in the scan modal.
  - **Configuration:** Default scan save directory read from `~/.config/lazyprint.yaml` (fallback `~/.config/lazyprint/config.yaml`), defaulting to `~/Scans`.
- **Print Submitter:**
  - **Hybrid Selection Flow:**
    1. CLI Argument: Running `lazyprint <file>` bypasses the picker and opens directly into the Print Options modal.
    2. In-TUI Picker: Pressing `p` opens an in-TUI file picker modeled on `~/Git/para`'s Yazi-style file navigation (`h`/`j`/`k`/`l` column traversal, directory expansion, filtering for printable extensions: `.pdf`, `.png`, `.jpg`, `.jpeg`).
  - **Print Options Modal:**
    - 1-sided vs. 2-sided (duplex: off, long-edge, short-edge).
    - Resolution / print quality options.
    - Copies count.
    - Target printer selector (defaults to current system default).
    - Confirmation before dispatching `lp`.
- **Panel Layout & Keybindings:**
  - **Dynamic Layout & Mouse Support:**
    - Left column: Panels `1` (Printers), `2` (Jobs), `3` (Scanners).
    - Right column: Panel `0` (Details / Inspector).
    - Full mouse support (click to focus/select, wheel to scroll, outside click to dismiss popups).
    - Dynamic window resizing via `tea.WindowSizeMsg`.
  - **Global Keybindings (Any panel focused):**
    - `q` / `ctrl+c`: Quit
    - `0`: Focus Details panel
    - `1`: Focus Printers panel
    - `2`: Focus Jobs panel
    - `3`: Focus Scanners panel
    - `Tab` / `Shift+Tab`: Cycle panel focus forward / backward
    - `p`: Open Print Submitter (Yazi-style file picker modal)
    - `s`: Open Scan modal (targets selected/default scanner)
    - `r`: Force refresh from CUPS and SANE
    - `?`: Open Help popup
  - **Printers Panel (`1`) Keybindings (Focused):**
    - `j` / `k` (or `↓` / `↑`): Move selection up / down
    - `d`: Set selected printer as default (`lpoptions -d`)
    - `R` (or `e`): Alias Rename (prompts new name, confirms `y`/`n`)
    - `a`: Add Printer (discovers via `driverless`, displays picker)
    - `space`: Pause / Resume printer queue
    - `x`: Delete printer queue (`lpadmin -x`, confirms `y`/`n`)
    - `Enter`: Open Print Submitter modal targeting this printer
  - **Jobs Panel (`2`) Keybindings (Focused):**
    - `j` / `k` (or `↓` / `↑`): Move selection up / down
    - `[` / `]`: Switch between Active / Completed tabs
    - `x` (or `d`): Cancel selected job (`cancel <job-id>`, confirms `y`/`n`)
    - `X`: Purge all jobs (`cancel -a`, confirms `y`/`n`)
    - `h`: Hold / Release selected job
    - `Enter`: Expand full job metadata in Details panel (`0`)
  - **Scanners Panel (`3`) Keybindings (Focused):**
    - `j` / `k` (or `↓` / `↑`): Move selection up / down
    - `s` (or `Enter`): Open interactive Scan Modal (DPI, format, flatbed/ADF toggle if supported, path override)
    - `f`: Quick Scan Flatbed (defaults: 300 DPI, PDF, autosave path)
    - `a`: Quick Scan ADF (defaults: 300 DPI, PDF; disabled if device lacks ADF)
    - `r`: Re-scan for hardware devices (`scanimage -L`)
    - *Hardware Capability Detection:* Queries device capabilities (`scanimage --help -d ...`) to automatically enable/disable ADF options depending on whether the scanner has a feeder or is flatbed-only.

---

## 7. Delivery Roadmap (Chunked Work)

Each chunk is a single independently-testable delivery (~150–250 lines changed), followed by a mandatory human checkpoint.

### Chunk 1: Scaffolding, CLI Runner, Config & AGENTS.md Creation
- **Objective:** 
  1. **Create `AGENTS.md`**: The implementing agent must author `AGENTS.md` in the project root before writing code, modeled on `~/Git/para/AGENTS.md`. It must document:
     - Project summary and relationship to `lazytask` and `para`
     - Exact build commands (`make build` compiling `./cmd/lazyprint` to git-ignored `./lazyprint`, `make test` running `go test ./...`, `make install` copying to `~/bin`)
     - Testing policy: table-driven unit tests for parsers/engine, hardware isolation via `internal/runner` (never execute real hardware commands in automated tests), and manual QA notes for UI packages
     - Workflow rules: one chunk per commit, hard stop boundaries with explicit human sign-off, no automated commits, styling parity (`paletteGreen = "2"`, `paletteBlue = "4"`, embedded top-border titles), and prohibition against creating `CLAUDE.md`.
  2. **Project Scaffolding**: Initialize Go 1.24 module (`github.com/tkilb/lazyprint`), `.gitignore`, and `Makefile` (`build`, `test`, `install`).
  3. **Mockable Runner**: Create `internal/runner` interface for executing external CLI commands (`lpstat`, `scanimage`, etc.) with a mock implementation for tests and an `os/exec` implementation for production.
  4. **Config Loader**: Create `internal/config` to read `~/.config/lazyprint.yaml` (default scan directory `~/Scans`, default format, etc.) with table-driven tests.
- **Verification:** Unit tests passing (`go test ./...`), `AGENTS.md` created matching all specifications, clean `make build`.

### Chunk 2: CUPS Driver & Parsers (`lpstat`, `cancel`, `lpoptions`)
- **Objective:** Pure parser package for `lpstat -p -d` (printers/defaults), `lpstat -o` (active queue jobs), and `cancel`.
- **Verification:** Comprehensive table-driven unit tests with fixture outputs (including real outputs captured from the Brother HL-L2395DW).

### Chunk 3: SANE Scanner Driver & Parsers (`scanimage`)
- **Objective:** Parser package for `scanimage -L` (devices), capabilities parsing (`--help -d`), and scan command construction (format, resolution, source).
- **Verification:** Table-driven parser tests with mock SANE/eSCL outputs.

### Chunk 4: Multi-Panel TUI Layout & Navigation (Read-Only)
- **Objective:** Bubble Tea model implementing the 2-column grid: Panels `1` (Printers), `2` (Jobs), `3` (Scanners), and `0` (Details). Number keys `0`-`3`, `Tab`, arrow/vim keys `j`/`k`, dynamic resizing, and mouse click focus.
- **Verification:** Interactive TUI run (`go run ./cmd/lazyprint`) displaying live system printers and scanners.

### Chunk 5: Queue & Printer Actions (Cancel, Default, Rename)
- **Objective:** Implement action popups: cancel job (`x`), set default (`d`), pause/resume (`space`), and Alias Rename (`R` - recreate queue via `lpadmin`).
- **Verification:** Interactive test cancelling/renaming a test queue with confirmation modals (`y`/`n`).

### Chunk 6: Interactive Scanning & Progress Spinner
- **Objective:** Implement the Scan Modal (`s`, `Enter`), Flatbed/ADF options, DPI picker, autosave timestamp pathing, and async scan execution with Bubble Tea progress spinner.
- **Verification:** Trigger scan to `~/Scans` from TUI, verify non-blocking UI during acquisition and resulting PDF.

### Chunk 7: Print Submitter (`para`-style Yazi Picker & Duplex Modal)
- **Objective:** Implement file picker modal (`h`/`j`/`k`/`l` column traversal filtered for `.pdf`, `.png`, `.jpg`), CLI arg invocation (`lazyprint <file>`), and print options modal (1-sided / 2-sided duplex, resolution, copies).
- **Verification:** Select a test PDF, choose 2-sided, verify exact `lp` command constructed and dispatched.

### Chunk 8: Polish, Help Popup & Packaging
- **Objective:** Help popup (`?`), command-line flags (`--version`, `-h`), install script, and manual QA pass.
- **Verification:** All tests passing (`go test ./...`), clean build, complete README.

---

## 8. Open-Ended Feedback & User Directives

- **Visual Consistency & Color Scheme:**
  - Must strictly match `lazytask`'s palette (`internal/ui/panel`):
    - Unfocused borders: terminal default (`""`)
    - Focused borders/titles: Green (`"2"`)
    - Active row / selection background: Blue (`"4"`)
    - Warning/Error: Orange (`"214"`) and Red (`"196"`)
- **Embedded Top-Border Titles:**
  - Panel headers and modal titles must be embedded directly inside the top border line (e.g. `╭─ 1 Printers ──────╮`) exactly as implemented in `lazytask`'s `panel.Frame` and `para`, saving vertical screen space.
- **Modals & Popups Format:**
  - All popups (prompts, confirmation dialogs, scan options, print submission forms) must use the identical format, borders, and severity treatment as `lazytask/internal/ui/popup`.
