# Audit Follow-up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development task-by-task. Each behavioral fix starts with a failing regression.

**Goal:** Resolve all actionable follow-up findings in AUDIT.md, preserving existing fixes and user images.

**Architecture:** Native boundaries get explicit outcomes and session identities. Filesystem and image operations use shared durability and resource rules; independent capture, persistence, frontend, and tooling work proceeds in parallel.

**Tech Stack:** Go 1.27.3, MyGo 0.9.0, TypeScript/Vite, existing dependencies.

**Spec:** AUDIT.md and the user's instruction to fix the remaining issues.

## Global Constraints

- Preserve the uncommitted first audit and README media in outputs/.
- Generate frontend/src/mygo.ts with npm run generate only.
- No new dependencies, releases, commits, or real desktop selftests.
- Image limit: 64 MiB encoded, 32 Mi decoded pixels; preflight before allocation.
- Keep editable annotations on failed actions or canceled save dialogs.
- Preserve aspect ratio; allow narrow pins down to one DIP. Archive overflow beyond 60 active restored pins explicitly; never delete their images.
- Native validation limitations must remain explicit in the report.

## Review Focus

- Old async events/actions must not mutate newer sessions.
- Disk failures must preserve effective settings and live pins.
- Capture retries must not duplicate history or leak temporary previews.
- Mixed DPI and negative monitor origins must use monitor-local conversion.
- Ambiguous scroll content must not invent matching confidence.

### Task 1: Capture and image budgets

- [x] Add imagebudget preflight tests and implementation; integrate decoding boundaries.
- [x] Test monitor-local DIP conversion, checked arithmetic, cursor geometry; implement Windows native boundary.
- [x] Add small-step/narrow-change/repetitive stitch fixtures; bound accumulator allocation and matcher work.

### Task 2: Persistence and pins

- [x] Test multiple handles, interrupted writes, invalid thumbnails/pin images; add directory locking and parent sync.
- [x] Test pin persistence failures before close/resize, aspect-preserving bounds and explicit overflow archive.

### Task 3: App lifecycle and contracts

- [x] Test annotation validation/action/cancel failures and deduplicated retries; return explicit CaptureActionResult.
- [x] Test cache ownership cleanup, partial history failure, and session identity guards.
- [x] Exclude scroll control/frame from capture, with deterministic placement fallback and compositor delay where required.
- [x] Test shortcut diff/rollback and settings partial errors; publish effective state.
- [x] Generate API bindings and integrate frontend regressions.

### Task 4: Frontend and tooling

- [x] Test watermark defaults precedence, tall pin sizing, canceled finish, stale scroll events.
- [x] Remove dead bridge/event APIs and obsolete prototypes after reference checks.
- [x] Test isolated selftest artifacts, nonzero assertion failures, and whole-case timeouts.

### Task 5: Validation and reporting

- [x] Run focused regressions, Go full/race/vet and Windows cross-builds, frontend/tool suites, typecheck/build/generate, shell and diff checks.
- [x] Review interactions and revise AUDIT.md to distinguish resolved source issues from pending native proof.

Final verification: Go full/race/vet, Windows amd64/arm64 builds, 126 frontend/tooling tests, typecheck, six-page web build, generation unchanged, gofmt, shell checks, and diff checks passed. Native acceptance and heuristic/runtime limits are documented in AUDIT.md. Changes remain uncommitted.
