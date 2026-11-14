# Project audit — 2026-11-13 / follow-up 2026-11-14

Reviewed the application, all six frontend entry points, capture backends, image conversion, history and pin persistence, stitching, tests, build configuration, CI, documentation, and historical tooling. The starting tree was clean at `b8b6879` (`chore(release): 0.1.0`). Changes remain uncommitted for review.

The fixes below address reproduced defects and clear source inconsistencies. The original 17 follow-up findings are mapped below to their implemented fixes and remaining native verification limits. Passing automated checks does not establish native desktop correctness or that every defect has been found.

## Fixed

| Area | Previous behavior | Current behavior and evidence |
| --- | --- | --- |
| Scroll completion | Manual finish stopped the loop, then the finalizer rejected the stopped session, leaving capture active. Old finalizers could clear newer state. | Manual finish joins the loop and finalizes; cancellation does not auto-finalize; cleanup only clears its own session. A stop timeout returns an error before accessing the accumulator. Regression tests cover finish, cancellation, and an old finalizer. |
| Delayed capture | A cancelled preparation could return pixels and write a session image into a later capture. Failed selection capture also disabled the backend session while the overlay offered retry. | Backend generations reject obsolete preparations before/after capture and before writing. Failed selection remains retryable. Tests reproduced unwanted files and unusable retry before the fixes. |
| Frontend asynchronous work | Late preparation, decoding, settings, history, and action responses could replace newer state or reenable controls mid-action. | Per-attempt/session guards, serialized/coalesced settings updates, and stale history-response rejection. History previews retain their entry identity across insertions; transient refresh failures retain the existing list. Synthetic tests execute the production TypeScript with a mocked DOM and bridge. |
| Annotation and preferences | Shrinking a selection baked annotations/watermarks into the new base; closed pen loops disappeared; reused overlays could retain old preferences; Enter ignored the copy setting. A first saved width of 4 could be mistaken for a legacy default. | Local crop uses original pixels and resets annotations, closed paths remain undoable, overlay preferences reload, keyboard copy respects its setting, and migration is marked on first load. Image bitmaps and fallback object URLs are released. |
| Resize failure recovery | Failed or stalled recapture lost the editable selection instead of restoring it as documented. A stalled decode was outside the resize watchdog. | Request, timeout, decode, and render failures restore the previous rectangle, pixels, annotations, watermark, and undo history. The 4-second watchdog covers fetching and decoding. Old bitmaps remain alive until successful replacement; late completions are discarded. Twelve follow-up tests also cover stale/current scroll permission callbacks. |
| Result windows | Delayed action cleanup could close a newer preview; repeated clicks could repeat a successful action during the close delay. Preview metadata could name another history entry. | Actions snapshot pixels and the owning window together, close that window only, and remain locked through the close delay. Cancelled saving remains retryable. Capture storage returns its own retained history ID. Native window behavior still needs GUI verification. |
| History retention and recovery | Retention only ran when settings changed. Byte accumulation could overflow. A future index schema could be treated as corruption and overwritten; null/missing entries and truncated images recovered incorrectly. | Every saved capture applies retention, serialized against settings changes. Pruning uses remaining budget. Unsupported future schemas are preserved; structurally empty indexes recover valid PNGs; truncated originals remain untouched and unindexed. |
| Pin persistence | Deleting one pin could delete a shared image. A late timer could recreate a removed pin. Failed persistence could report success or leave a newly created image orphaned. Header-only PNG data could replace a valid image. | Shared images survive until the last reference, including filesystem aliases. Removal and snapshots serialize; pending timers stop. Updates persist before publishing memory state. Creation rolls back its image on index-write failure. Full decoding precedes replacement. |
| Export integrity | Concurrent folder exports overwrote each other; a filename error could loop forever. Selecting `foo.jpeg` could overwrite an unconfirmed existing `foo.jpg` after extension normalization. Truncated same-format images passed through. | Folder exports exclusively create collision-free names and return filesystem errors. A changed extension exclusively creates that exact destination or returns a conflict; an unchanged dialog path retains confirmed overwrite behavior. Both export paths share the helper. Images are fully validated. |
| Capture and stitching | Nonzero image origins/strides caused a slice panic. An early difference check rejected valid overlaps after localized noise. Changed dimensions mutated state. Every append recopied the entire accumulated canvas. | Origin-aware row access, a valid early-rejection bound, dimension validation before mutation, correct last-overlap metadata, and geometric canvas growth. Geometry rejects NaN, infinity, and integer-conversion overflow. |
| Media helpers | Sampling missed isolated visible pixels and falsely reported a blank/permission failure. Sound subprocesses were started without being reaped. | Blank detection scans all alpha values when the fast sample is empty. Each successfully started sound process gets an asynchronous `Wait`. Tests use synthetic images and child processes, without playing sound. |
| Startup and native-test isolation | Fatal storage setup errors allowed startup to continue; legacy migration preceded the single-instance lock. Selftests could use real app storage/preferences and mutate login-item state. | Fatal storage errors stop startup. Migration follows the lock and skips explicitly isolated data. Selftests require `PINSTILL_DATA_DIR` before startup, use temporary WebView storage, skip login-item mutation, and reject unknown modes with a nonzero exit. |
| Tooling and documentation | E2E reused data, killed an existing app, and could continue after build/process failures. Local builds depended on signing setup. Prototype scripts could overwrite current HTML or claim obsolete verification. | Fresh data per E2E case (only persistence setup/check share), isolated logs, running-app refusal, fail-fast build/process handling, correct Intel/Apple Silicon paths, and no E2E notarization. Local signing defaults to ad-hoc; release identity/notary are explicit. Obsolete writers/checker and unused prototype assets are removed after reference checks. CI runs frontend/tooling regressions. README commands, pin restore cap, and scroll-stop wording are corrected. |

Representative observed failures before fixes:

- 48 simultaneous exports produced only 2 distinct paths, with overwritten contents; all 48 now produce distinct paths with correct contents.
- Selecting `foo.jpeg` overwrote an existing `foo.jpg`; the conflict now preserves both files and returns an error naming the adjusted destination.
- A cancelled delayed capture returned a usable payload and left a session PNG; both cancellation variants now return an error without writing a file.
- Stitching subimages panicked with `slice bounds out of range [:-1532]`; origin/stride regressions now pass.
- The fixed synthetic allocation test dropped from 17,043,657 to 1,040,832 allocated bytes for a 524,288-byte result. This demonstrates reduced accumulator allocation in that workload, not a native capture-speed benchmark.
- A 200×200 image with one opaque pixel at `(1,1)` was called blank; it is now recognized as visible.
- Resize failure and delayed permission checks produced nine failing regressions before the follow-up fix; all now pass, along with success and stale-session controls.
- Future-schema indexes, shared pin images, corrupt replacements, failed persistence, and late pin snapshots each have regression coverage.

## Follow-up fixes

The user authorized the remaining fixes. These choices preserve editable captures and user images, make failures visible, and retire unused prototype code. The numbers correspond to the original review list.

| Original finding | Implemented change | Evidence |
| --- | --- | --- |
| 1. Windows coordinate units | Convert MyGo 0.9.0 DIPs at the native boundary using monitor-local scale, logical monitor origins and MyGo-compatible negative-coordinate rounding. Selection, fullscreen and scroll share this boundary. Checked arithmetic precedes GDI allocation. | Synthetic 100%/150%/200%, mixed-DPI, negative-origin and overflow fixtures; both Windows architectures cross-build. |
| 2. Annotation action failure/cancel | `CaptureActionResult` distinguishes completion and save cancellation. Validate before hiding; preserve editing on error/cancel. Identical retry reuses committed history identity instead of saving twice. | Invalid PNG, failed action, canceled save and successful retry regressions in Go and frontend. |
| 3. Unbounded temporary storage | Own one selection preview and one result preview. Successful replacement releases the previous file; staged writes preserve it on failure. Failed cleanup retains ownership and blocks further additions until retry. Capture storage no longer duplicates session files. | Repeated preparation leaves one file; cancel removes it; failed cleanup cannot grow the directory; failed replacement preserves old pixels. |
| 4. Capture control contamination | Request OS capture exclusion and hide the control/frame during capture, with a compositor delay. Restore only for the still-active session. | Synthetic hide/capture/show and obsolete-session regressions; actual captured pixels need native verification. |
| 5. Scroll session identity | Add session IDs to init/progress and pause/auto/finish requests. Frontend rejects obsolete init/events. Run identity check plus presentation on the UI thread; canceled finalization cannot publish a later preview. Old overlay commands cannot interrupt scroll. | Stale actions/events, canceled finalization, obsolete capture, old finalizer and manual finish regressions. |
| 6. Settings/history failure handling | Publish effective backend settings after native effects. Login-item failures report either direction; shortcut rollback errors remain explicit. Failed history disabling stays enabled for retry. History save errors fail the capture unless the index already committed; committed sync/prune failures report partial success. Cleanup records persist until actual deletion succeeds. | Native seams and filesystem blockers cover rollback, clear/prune retries, committed save identity and serialized retention/settings. |
| 7. Pin persistence failures | Persist close/resize/opacity before native mutation. Failed persistence keeps the live window and reports an error; late snapshots cannot resurrect deleted records. Failed pin image deletion restores its index record for retry and restart. Quit snapshots must succeed before stopping capture. | Synthetic window callbacks, blocked indexes, late timers/snapshots and failed quit regressions. |
| 8. Extreme pin aspect ratio | Fit both axes with one scale during creation/restore/resize; allow a minimum of one DIP when needed. Restored archives persist display-clamped geometry. | Tall/wide image, tiny display, creation, restore and frontend sizing fixtures. |
| 9. Windows cursor/thread lifetime | Draw cursor with its hotspot into the capture DC when requested; lock the GDI sequence to one OS thread and release native objects. | Hotspot/clipping geometry tests, source review and Windows builds. Native cursor fidelity remains unverified. |
| 10. Image resource budgets | Accept at most 64 MiB compressed data and 32 Mi pixels. Bound file/base64 reads, preflight headers before decoding/allocation, bound encoders, and cap scroll height by canvas width. Index reads are capped at 8 MiB and oversized originals are preserved. Darwin capture has bounded delay/subprocess duration. | Oversized headers/files, bounded encoder, arithmetic and accumulator cap regressions without dangerous real allocations. |
| 11. Stitch matching cost/accuracy | Row fingerprints and KMP nominate exact overlap, then every RGB pixel verifies it. Noisy matching examines a bounded candidate set. Small movements, isolated columns and static sticky prefixes are supported; canvas grows geometrically. | Exact-result fixtures for small scroll steps, repeating patterns with distinct features, sticky headers, nonzero origins and localized noise; synthetic allocation/CPU checks. |
| 12. Corrupt restored assets | Fully decode restored pin PNGs; validate/regenerate JPEG thumbnails from valid originals. Preserve damaged originals and omit unusable pin records. | Corrupt thumbnail repair, truncated pin rejection and original preservation regressions. |
| 13. Durability/multiwriter coordination | Permanent directory lock files coordinate processes/handles and stale-temp cleanup. Atomic writes sync the file, rename, and sync the directory on Unix; Windows uses write-through replacement. A post-rename error preserves committed identity and referenced images. | Independent handle/process tests and injected directory-sync failure. These prove ordering/error semantics, not arbitrary power-loss behavior. |
| 14. Selftest isolation/failure/timeouts | Require an existing absolute disposable directory with exact marker, reject production paths/symlinks, write artifacts under its root, fail all assertions/errors, and emit real-quit success only from `OnQuit`. Every E2E case has a bounded timeout, owns only its child PID and requires both exit zero and its success marker. | Synthetic harness, marker/path, child failure/timeout and shell checks. No desktop E2E was run. |
| 15. Shortcut churn/OCR reservation | Diff native shortcut ownership; unchanged keys stay registered, dependent swaps rollback as a group. OCR shows unavailable and reserves no global key; remembered local preference is retained. | Fake native registration ownership, rollback and OCR UI regressions. |
| 16. Restore-cap resurfacing | Persist older active records beyond 60 as archived. Show them in History and restore explicitly after checking live count, duplicates, image validity and display geometry. Never automatically resurface or delete archived images. | Archive across reopen/removal, manual restore, persistence failure and frontend archive-list tests. |
| 17. Watermark precedence | Changing general watermark text/opacity supersedes remembered toolbar values; toolbar edits persist until the next relevant general change. Unrelated settings preserve toolbar memory. | General-change, unrelated-change and overlay reuse regressions. |

Additional integration fixes found during follow-up:

- Each long-image preview carries an ephemeral ID; copy/save/pin/close rejects requests from an older preview. Actions snapshot the pixels and owning window together.
- Failed preview writes preserve the old URL/file; canceled result publication removes its staged file and leaves the previous preview intact. Close/ready callbacks check window/file ownership.
- Successful quit invalidates delayed fullscreen capture before history and external actions. Failed quit keeps a running scroll usable. Overlay cancellation that was ignored during scroll returns an error instead of claiming completion.
- Pin resize/opacity/persistence requests share one serial queue, with coalesced pending sizes and accepted-value persistence. Backend partial mutations serialize under the state lock; UI callbacks reread the latest persisted record before applying it.
- Refreshed history indexes are sanitized before filesystem use; a tampered path cannot cause deletion outside the history directory. Pending file deletion is durable for single deletion, clear and prune.

Representative follow-up RED observations included `late overlay cancellation interrupted scrolling`, an exact sticky-header overlap of 0 instead of 55, failed deletion leaving a thumbnail on retry, oversized indexes being rewritten, and a refreshed path escaping the history directory. Regression suites pass after the corresponding fixes. New contract helpers first failed compilation where the API did not yet exist, then passed after integration.

## Removed and preserved material

Removed after confirming no production references:

- `work/install-settings.py`, `work/update-toolbar.py`, `work/check-prototype.js`
- `work/settings.js`, `work/settings.css`, `work/toolbar.js`
- `work/naming/github-tu-research.json`
- Unused editor-era bridge wrappers, action type and overlay-capture event; generated bindings were regenerated.

`work/plan.md` remains as explicitly marked historical context. README screenshots/design references in `outputs/` are preserved. The implementation plan is in `docs/superpowers/plans/2026-11-13-audit-follow-up.md`.

## Remaining verification and product limits

These are not established as fixed by synthetic tests:

1. **Native desktop acceptance:** macOS/Windows mixed-DPI monitors, cursor pixels, control-window capture exclusion/compositor timing, save-dialog cancellation, clipboard/pin errors, shortcut registration and login-item behavior. The current maintenance did not open the app or run desktop E2E. Windows cross-builds cannot substitute for this.
2. **Ambiguous scrolling content:** animated/sticky regions, fixed footers and visually indistinguishable repeated content can still defeat matching. Static sticky headers and distinguishable repeats have exact fixtures; representative application captures remain useful. Prefer selecting the inner scrolling content for such pages.
3. **Runtime cost:** the 32 Mi-pixel bound limits one RGBA canvas to 128 MiB, not total process memory. Decoded frames, encoder buffers, retained PNGs and WebView surfaces add overhead. Synthetic timings/allocation reductions are not a real-machine peak-memory or capture-speed benchmark. Windows GDI calls have no hard cancellation boundary; the UI reports an unfinished stop instead of reading the active accumulator.
4. **Settings presentation/startup timing:** failed fields remain visibly marked as not applied, while the page retains the requested preference for retry and backend `Settings()` exposes effective state. Initial native settings arrive asynchronously from the main page; first-launch timing under an immediate shortcut still needs native acceptance.
5. **Unavailable features:** OCR, translation and QR decoding remain explicitly unavailable; the cleanup removes OCR shortcut reservation. Implementing these services requires separate product work.
6. **Filesystem limits:** locks coordinate cooperating processes; unrelated external writers are not synchronized. Directory fsync/write-through behavior depends on the actual filesystem. A failed pin image deletion attempts to restore its index record; if that rollback also fails, both errors are returned and the original image is preserved for recovery. Corrupt/oversized originals remain preserved and may need manual recovery; startup intentionally fails instead of rewriting unsupported indexes.

## Verification

Every Go command uses `GOCACHE=/tmp/pinstill-audit-go-cache-20261113` because the shared cache previously returned missing-object errors. Those infrastructure failures were separated from regression failures.

Final verification after the last source edits (2026-11-14):

| Command | Result |
| --- | --- |
| `go test ./... -count=1` | All seven internal packages passed. |
| `go test -race ./... -count=1` | All seven internal packages passed; no race reports. |
| `go vet ./...` | Passed, exit 0. |
| `GOOS=windows GOARCH=amd64 go build ./...` | Passed, exit 0. |
| `GOOS=windows GOARCH=arm64 go build ./...` | Passed, exit 0. |
| `npm test` | 126 passed, zero failed/skipped: 100 frontend and 26 tooling tests. |
| `npm run typecheck` | Passed. |
| `npm run build:web` | Passed; all six entry pages built. |
| `npm run generate` | Passed; SHA-256 unchanged for the already generated bindings. |
| `gofmt -l cmd internal` | No unformatted files. |
| `bash -n work/e2e.sh`, `shellcheck work/e2e.sh` | Passed. |
| `git diff --check` | Passed. |

Logs are retained under `/tmp/pinstill-followup-final-*-20261114.log`. These are local synthetic/build checks; CI and desktop acceptance were not run.

 No dependencies were added. All changes remain uncommitted; no pushes, releases, native capture, clipboard writes, login-item changes, signing or notarization were performed.
