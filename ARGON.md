# Hand-off Notes & State: `lazyprint`

## 1. Project Overview & Context

- **Repository:** `github.com/tkilb/lazyprint` (local path: `/home/tylerkilburn/Git/lazyprint`)
- **Language / Runtime:** Go 1.24, Bubble Tea + Lip Gloss (following architecture and styling conventions from `~/Git/lazytask` and `~/Git/para`).
- **Core Design:** Zero-CGO CLI wrapper around standard system utilities:
  - CUPS: `lpstat`, `lpoptions`, `cancel`, `lp`, `lpadmin`
  - SANE: `scanimage` (driverless eSCL / sane-airscan)
- **Hardware Isolation:** All execution is routed through `internal/runner.Runner` interface. Unit tests use `runner.MockRunner` and never touch real hardware.
- **Build / Test:**
  - Build: `make build` (outputs `./lazyprint`)
  - Test: `make test` or `go test -count=1 ./...`
  - Install: `make install` (installs to `~/bin/lazyprint`)
- **Key Hardware Environment:**
  - Brother HL-L2395DW series network multi-function printer/scanner on LAN (`192.168.0.246`).
  - SANE discovery via `airscan:e0:Brother HL-L2395DW series` and `escl:http://192.168.0.246:80`.
  - Network discovery takes ~9.8s on this LAN, so discovery contexts require >=25s timeout.

---

## 2. Completed Milestones (Chunks 1–5 Committed)

1. **Chunk 1 (`9c69b8c`):** Scaffolding, CLI Runner, Config loader (`~/.config/lazyprint.yaml` fallback `~/.config/lazyprint/config.yaml`), `AGENTS.md`.
2. **Chunk 2 (`2155aeb`):** CUPS driver & table-driven parsers (`lpstat -p -d`, `lpstat -v`, `lpstat -o`, `lpoptions -l`).
3. **Chunk 3 (`0c7cb39`):** SANE driver & table-driven parsers (`scanimage -L`, `scanimage --help -d`, timestamp naming).
4. **Chunk 4 (`cf9a45a`):** Multi-panel TUI layout:
   - Panel `[1] Printers`: Compact (outer height 5: border + 3 rows + border).
   - Panel `[2] Jobs` & `[3] Scanners`: Evenly divided left-column remainder.
   - Panel `[0] Details`: Right column inspector.
   - Top-border titles embedded: `╭──[n] Title────────╮`.
5. **Chunk 5 (`468b293`):** CUPS printer and job management actions (`Pause`, `Resume`, `Delete`, `Cancel Job`, `Purge Jobs`, `Rename Printer` with prompt/confirm popups).

---

## 3. Current Working Tree State (Chunk 6 in progress)

### What Has Been Implemented in Chunk 6:
1. **Interactive Scan Modal (`internal/ui/scanmodal`):**
   - Activated via `s` or `Enter` on `[3] Scanners`.
   - Fields: `Source` (Flatbed/ADF), `Resolution` (DPI), `Color Mode` (`Color`, `Gray`, `Lineart`), `Format` (`pdf`, `png`, `jpeg`), `Auto-Crop` (`Yes`/`No`), `Destination File` path input.
   - Keyboard navigation: `Tab` / `Shift+Tab` / `Up` / `Down` to navigate fields, `h` / `l` / `Left` / `Right` to cycle options.
   - Synchronizes file extension in path input dynamically when format changes.
2. **In-Memory Settings Persistence:**
   - `app.Model` stores `LastScanResolution`, `LastScanFormat`, `LastScanSource`, `LastScanMode`, and `LastScanAutoCrop`.
   - Any adjusted option is persisted in memory immediately and upon `Esc` dismiss or `Enter` submit, carried over into subsequent modal openings and quick-scans.
3. **Color Scanning Restored:**
   - Defaulted `--mode Color` across `ScanConfig`, `scanmodal`, and quick-scans (`f` for flatbed, `a` for ADF) so the scanner acquires full 24-bit RGB scans instead of 1-bit Lineart dithering.
4. **Non-Blocking Progress Spinner:**
   - Active spinner rendered during asynchronous scan execution (`scanimage`).

---

## 4. The Unresolved Defect: Auto-Crop / Boundary Detection

### Problem Statement
When scanning a greeting card, receipt, or document placed on the flatbed glass, the output image remains the entire physical scanner glass bed (e.g. full 8.5" × 11.7" Letter area, ~4080 × 5336 pixels) instead of auto-cropping down to the physical card.

### Visual & Physical Evidence
- The test document used by the user is a Hallmark greeting card (`WHO'S A GOOD DAD?...` dog photo on front, Hallmark barcode / licensing text on back).
- Along the bottom of the card, just under the barcode line, there is an obvious, distinct dark edge line (the physical cut boundary and shadow of the paper against the glass: `________|`).
- Similarly, along the edge of the card, the paper edge forms a clear line separating the card from the scanner bed.

### Why Previous Attempts Failed
1. **Naive ImageMagick `-trim`:**
   - ImageMagick's `-trim` determines background color strictly by reading pixel `(0, 0)`.
   - Because the card was placed in the top-left origin corner of the scanner bed, `(0, 0)` was inside the photo/card. `-trim` treated the card as background and the white scanner lid as foreground, trimming 0 pixels.
2. **Naive Pixel Luminance Column/Row Sums:**
   - Flatbed scanner glass has outer black bezel borders, glass bevel reflections, and dust specs at the extreme borders.
   - Simple thresholding (`pixel < 220`) without edge filtering triggers on single dust/bezel pixels at row 0 and column 4064, incorrectly declaring that content spans the entire bed width and height.

### Solution Requirements for Next Agent
Commercial software (Apple Image Capture, VueScan, NAPS2, Simple Scan) reliably detects the document using **Edge Detection & Contour / Line Finding**, not simple color thresholding:
1. **Gradient / Edge Detection (Sobel / Canny filter):**
   - Apply an edge/gradient filter to detect the physical cut paper line and shadow against the glass.
   - The boundary between the paper and the scanner lid produces a clear high-gradient line across the image.
2. **Hough Line Transform or Connected Component / Contour Bounding Box:**
   - Detect the dominant rectangular boundary formed by the paper's physical cut lines.
   - Find the bounding box `[minX, minY, maxX, maxY]` corresponding to the paper rectangle.
3. **Crop Original:**
   - Crop the full-resolution image (or PDF) to that detected bounding box with a small margin (~10–15px).
   - Ensure the crop works on Color (RGB) images natively.

---

## 5. Upcoming Roadmap (Post-Chunk 6)

- **Chunk 7: Print Submitter (`para`-style Yazi Picker & Duplex Modal):**
  - Modeled on `~/Git/para`: `h`/`j`/`k`/`l` column traversal filtered for `.pdf`, `.png`, `.jpg`, `.jpeg`.
  - CLI direct invocation: `lazyprint <file>` bypasses picker and opens print options modal.
  - Print options: Duplex (`off`, `long-edge`, `short-edge`), copies count, target printer selector, `lp` execution.
- **Chunk 8: Polish, Help Popup & Packaging:**
  - `?` Help popup listing all contextual keybindings.
  - CLI flags (`--version`, `-h`, `--help`).
  - Verification of `make install` to `~/bin/lazyprint`.

---

## 6. Process Reminders
- **Human-in-the-loop checkpoints:** Stop after every chunk, report files touched, run tests, and wait for human approval.
- **No automated commits:** The user commits manually. Do not run `git commit` or `git push`.
