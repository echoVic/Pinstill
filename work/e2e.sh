#!/usr/bin/env bash
# End-to-end verification for Pinstill (macOS).
#
# Usage: bash work/e2e.sh [--skip-build]
# Native tests need an interactive macOS desktop and capture permissions.
# Each run retains isolated data, logs and captures in the temporary directory
# printed below. No selftest output uses shared /tmp filenames.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

# Background shells sometimes miss the nvm-installed node/npm on PATH.
if ! command -v npm >/dev/null 2>&1; then
  if command -v node >/dev/null 2>&1; then
    PATH="$(dirname "$(command -v node)"):$PATH"
    export PATH
  fi
fi
if ! command -v npm >/dev/null 2>&1; then
  for dir in "$HOME"/.nvm/versions/node/*/bin; do
    if [ -x "$dir/npm" ]; then
      PATH="$dir:$PATH"
      export PATH
      break
    fi
  done
fi

fail=0
step() { printf '\n== %s ==\n' "$1"; }
# expect greps a selftest log line and names the failing check.
expect() {
  if grep -q "$1" "$2"; then
    return 0
  fi
  printf 'FAIL  missing "%s" in %s\n' "$1" "$2"
  fail=1
}

skip_build=0
if [ "$#" -eq 1 ] && [ "$1" = "--skip-build" ]; then
  skip_build=1
elif [ "$#" -ne 0 ]; then
  echo "Usage: bash work/e2e.sh [--skip-build]" >&2
  exit 2
fi

if [ "$(uname -s)" != Darwin ]; then
  echo "Native selftests require macOS." >&2
  exit 1
fi
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  x86_64) arch=amd64 ;;
  *) echo "Unsupported macOS architecture" >&2; exit 1 ;;
esac

# A running instance can swallow a launch. Never terminate the user's app.
if pgrep -f 'Pinstill[.]app/Contents/MacOS/Pinstill([[:space:]]|$)' >/dev/null; then
  echo "Quit Pinstill before running native selftests." >&2
  exit 1
else
  rc=$?
  if [ "$rc" -ne 1 ]; then
    echo "Unable to check for a running Pinstill (pgrep exit $rc)." >&2
    exit 1
  fi
fi

step "Go tests"
GOTOOLCHAIN=auto go test ./... || exit 1

step "Go vet"
GOTOOLCHAIN=auto go vet ./... || exit 1

step "Windows cross-compile"
GOOS=windows GOARCH=amd64 GOTOOLCHAIN=auto go build ./... || exit 1

if [ "$skip_build" -eq 0 ]; then
  step "Frontend build"
  npm run build:web || exit 1

  step "Desktop app build"
  # MyGo rebuilds the frontend after regenerating bindings. Verification does
  # not create release images or submit to Apple's notary service.
  ./node_modules/.bin/mygo build -platform "darwin/$arch" -skip-dmg -skip-notarize || exit 1
fi

APP="build/darwin-$arch/Pinstill.app/Contents/MacOS/Pinstill"
if [ ! -x "$APP" ]; then
  echo "missing build output: $APP"
  exit 1
fi

# Override even an inherited PINSTILL_DATA_DIR. Every case starts empty; only
# the persistence setup/check pair explicitly shares a directory.
run_dir=$(mktemp -d "${TMPDIR:-/tmp}/pinstill-selftest.XXXXXX") || exit 1
echo "Selftest logs and data: $run_dir"
# Bound the complete native case, not just a readiness or quit phase. The
# test suite replaces sleep/kill through BASH_ENV; native defaults stay bounded.
case_timeout=${PINSTILL_SELFTEST_TIMEOUT_SECONDS:-120}
if ! [[ "$case_timeout" =~ ^[1-9][0-9]*$ ]] || [ "${#case_timeout}" -gt 4 ] || [ "$case_timeout" -gt 3600 ]; then
  echo "PINSTILL_SELFTEST_TIMEOUT_SECONDS must be 1..3600" >&2
  exit 2
fi
run_selftest() {
  local mode="$1" data_dir="${2:-$run_dir/data-$1}" rc pid deadline log
  mkdir -p "$data_dir/artifacts" || return 1
  printf 'pinstill-selftest\n' > "$data_dir/.pinstill-selftest" || return 1
  log="$data_dir/artifacts/$mode.log"
  PINSTILL_SELFTEST="$mode" PINSTILL_DATA_DIR="$data_dir" "$APP" > "$log" 2>&1 &
  pid=$!
  deadline=$((SECONDS + case_timeout))
  rc=0
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      printf 'FAIL  selftest %s timed out after %ss (owned PID %s)\n' "$mode" "$case_timeout" "$pid" >&2
      # Only the process started above; never a process name or process group.
      kill -9 "$pid" 2>/dev/null || true
      rc=124
      break
    fi
    sleep 0.2
  done
  if wait "$pid"; then :; else
    local wait_rc=$?
    if [ "$rc" -eq 0 ]; then rc=$wait_rc; fi
  fi
  # Display the retained log and account for output-command failures as well.
  tee /dev/stdout < "$log" || rc=1
  if ! grep -qx "SELFTEST $mode: checks=ok" "$log" || grep -q 'checks=FAILED' "$log"; then rc=1; fi
  if [ "$rc" -ne 0 ]; then
    printf 'FAIL  selftest %s exited unsuccessfully (%s); log: %s\n' "$mode" "$rc" "$log" >&2
  fi
  return "$rc"
}

step "Selftest: real quit path"
run_selftest quit || fail=1

step "Selftest: native capture, history and clipboard"
run_selftest capture || fail=1
expect 'SELFTEST capture: ok' "$run_dir/data-capture/artifacts/capture.log"
expect 'SELFTEST capture: clipboard ok' "$run_dir/data-capture/artifacts/capture.log"
expect 'SELFTEST capture: history entry' "$run_dir/data-capture/artifacts/capture.log"

step "Selftest: overlay rendering"
run_selftest overlay || fail=1
expect 'SELFTEST overlay: ok' "$run_dir/data-overlay/artifacts/overlay.log"

step "Selftest: overlay selection mapping"
run_selftest drag || fail=1
expect 'SELFTEST drag: delivered' "$run_dir/data-drag/artifacts/drag.log"
expect 'same=true' "$run_dir/data-drag/artifacts/drag.log"

step "Selftest: escape cancels the overlay"
run_selftest esc || fail=1
expect 'overlay visible=false' "$run_dir/data-esc/artifacts/esc.log"
expect 'main visible=true' "$run_dir/data-esc/artifacts/esc.log"
expect 'SELFTEST esc: main_after_hidden_cancel=false' "$run_dir/data-esc/artifacts/esc.log"
expect 'SELFTEST esc: checks=ok' "$run_dir/data-esc/artifacts/esc.log"

step "Selftest: desktop action flow (drag + toolbar)"
run_selftest flow || fail=1
expect 'SELFTEST flow: toolbar_drag=60,40' "$run_dir/data-flow/artifacts/flow.log"
expect 'SELFTEST flow: copy clipboard' "$run_dir/data-flow/artifacts/flow.log"
grep -q 'SELFTEST flow: copy clipboard EMPTY' "$run_dir/data-flow/artifacts/flow.log" && fail=1
expect 'SELFTEST flow: pin pins=1' "$run_dir/data-flow/artifacts/flow.log"
expect 'SELFTEST flow: cancel history before=.* after=' "$run_dir/data-flow/artifacts/flow.log"
expect 'SELFTEST flow: auto clipboard ' "$run_dir/data-flow/artifacts/flow.log"

step "Selftest: double-click copies the capture"
run_selftest dblclick || fail=1
expect 'SELFTEST dblclick: first=clicked second=clicked clipboard=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-dblclick/artifacts/dblclick.log"
grep -q 'clipboard=0x0' "$run_dir/data-dblclick/artifacts/dblclick.log" && fail=1
expect 'SELFTEST dblclick: checks=ok' "$run_dir/data-dblclick/artifacts/dblclick.log"

step "Selftest: resize the captured selection"
run_selftest resize || fail=1
expect 'SELFTEST resize: handles=4 before=[0-9][0-9]*x[0-9][0-9]* after=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-resize/artifacts/resize.log"
expect 'SELFTEST resize: shrink before=[0-9][0-9]*x[0-9][0-9]* after=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-resize/artifacts/resize.log"
expect 'SELFTEST resize: checks=ok' "$run_dir/data-resize/artifacts/resize.log"

step "Selftest: tiled watermark"
run_selftest watermark || fail=1
expect 'SELFTEST watermark: shown=true' "$run_dir/data-watermark/artifacts/watermark.log"
expect 'rendered=true edited=true opacity=45% off_hidden=true restored=true' "$run_dir/data-watermark/artifacts/watermark.log"
expect 'SELFTEST watermark: clipboard=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-watermark/artifacts/watermark.log"
expect 'SELFTEST watermark: checks=ok' "$run_dir/data-watermark/artifacts/watermark.log"

step "Selftest: desktop pin interactions"
run_selftest pinui || fail=1
expect 'SELFTEST pinui: dblclick clipboard ' "$run_dir/data-pinui/artifacts/pinui.log"
expect 'SELFTEST pinui: bounds unchanged=true' "$run_dir/data-pinui/artifacts/pinui.log"
expect 'SELFTEST pinui: menu copy=clicked' "$run_dir/data-pinui/artifacts/pinui.log"
expect 'SELFTEST pinui: opacity before=100 after=90' "$run_dir/data-pinui/artifacts/pinui.log"
expect 'SELFTEST pinui: wheel bounds=.* scaled=true' "$run_dir/data-pinui/artifacts/pinui.log"
expect 'SELFTEST pinui: after close pins=0 stored=0' "$run_dir/data-pinui/artifacts/pinui.log"

step "Selftest: desktop pin persistence"
pin_data="$run_dir/data-pinpersist"
run_selftest pinpersist-setup "$pin_data" || fail=1
expect 'SELFTEST pinpersist: setup created' "$pin_data/artifacts/pinpersist-setup.log"
run_selftest pinpersist-check "$pin_data" || fail=1
expect 'SELFTEST pinpersist: check restored=[1-9]' "$pin_data/artifacts/pinpersist-check.log"
expect 'SELFTEST pinpersist: check after close stored=0' "$pin_data/artifacts/pinpersist-check.log"

step "Selftest: scrolling capture (auto finish + preview)"
PINSTILL_SELFTEST_SCROLL_SIM=1 run_selftest scroll || fail=1
expect 'SELFTEST scroll: accessibility=' "$run_dir/data-scroll/artifacts/scroll.log"
expect 'SELFTEST scroll: result=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-scroll/artifacts/scroll.log"
expect 'SELFTEST scroll: clipboard=1280x' "$run_dir/data-scroll/artifacts/scroll.log"
expect 'SELFTEST scroll: closed_after_copy=true' "$run_dir/data-scroll/artifacts/scroll.log"
expect 'SELFTEST scroll: frame_visible=true frame_size=524x424' "$run_dir/data-scroll/artifacts/scroll.log"
expect 'SELFTEST scroll: frame_hidden=true' "$run_dir/data-scroll/artifacts/scroll.log"

step "Selftest: scrolling capture through the page"
PINSTILL_SELFTEST_SCROLL_SIM=1 run_selftest scrollflow || fail=1
expect 'SELFTEST scrollflow: result=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-scrollflow/artifacts/scrollflow.log"
expect 'SELFTEST scrollflow: image=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-scrollflow/artifacts/scrollflow.log"
expect 'actions=.*copy' "$run_dir/data-scrollflow/artifacts/scrollflow.log"
expect 'actions=.*save' "$run_dir/data-scrollflow/artifacts/scrollflow.log"
expect 'actions=.*pin' "$run_dir/data-scrollflow/artifacts/scrollflow.log"
expect 'actions=.*close' "$run_dir/data-scrollflow/artifacts/scrollflow.log"
expect 'SELFTEST scrollflow: closed=true' "$run_dir/data-scrollflow/artifacts/scrollflow.log"

step "Selftest: scrolling capture (manual mode auto finish)"
PINSTILL_SELFTEST_SCROLL_SIM=1 run_selftest scrollmanual || fail=1
expect 'SELFTEST scrollmanual: result=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-scrollmanual/artifacts/scrollmanual.log"
expect 'result=.*window=true' "$run_dir/data-scrollmanual/artifacts/scrollmanual.log"
expect 'SELFTEST scrollmanual: frame_visible=true frame_size=524x424' "$run_dir/data-scrollmanual/artifacts/scrollmanual.log"
expect 'SELFTEST scrollmanual: frame_hidden=true' "$run_dir/data-scrollmanual/artifacts/scrollmanual.log"
expect 'SELFTEST scrollmanual: checks=ok' "$run_dir/data-scrollmanual/artifacts/scrollmanual.log"

step "Selftest: whole-display capture"
run_selftest fullscreen || fail=1
expect 'SELFTEST fullscreen: clipboard=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-fullscreen/artifacts/fullscreen.log"
grep -q 'SELFTEST fullscreen: clipboard=0x0' "$run_dir/data-fullscreen/artifacts/fullscreen.log" && fail=1
expect 'history before=[0-9][0-9]* after=[0-9][0-9]*' "$run_dir/data-fullscreen/artifacts/fullscreen.log"
expect 'SELFTEST fullscreen: checks=ok' "$run_dir/data-fullscreen/artifacts/fullscreen.log"

step "Selftest: history actions (copy/pin/remove)"
run_selftest historyactions || fail=1
expect 'historyactions: clipboard=[0-9][0-9]*x[0-9][0-9]*' "$run_dir/data-historyactions/artifacts/historyactions.log"
expect 'pins=1' "$run_dir/data-historyactions/artifacts/historyactions.log"
expect 'remove .*delta=1' "$run_dir/data-historyactions/artifacts/historyactions.log"
expect 'SELFTEST historyactions: checks=ok' "$run_dir/data-historyactions/artifacts/historyactions.log"

step "Selftest: main window views"
run_selftest uicapture || fail=1
expect 'SELFTEST uicapture: views=history,settings' "$run_dir/data-uicapture/artifacts/uicapture.log"
expect 'SELFTEST uicapture: editor_dom=false' "$run_dir/data-uicapture/artifacts/uicapture.log"
expect 'SELFTEST uicapture: preview=true' "$run_dir/data-uicapture/artifacts/uicapture.log"
expect 'SELFTEST uicapture: preview_closed=false' "$run_dir/data-uicapture/artifacts/uicapture.log"
expect 'SELFTEST uicapture: settings ' "$run_dir/data-uicapture/artifacts/uicapture.log"

step "Selftest: pin window"
run_selftest pin || fail=1
expect 'SELFTEST pin: window' "$run_dir/data-pin/artifacts/pin.log"

step "Selftest: global shortcuts"
run_selftest hotkeys || fail=1
expect 'captureKey .* registered=true' "$run_dir/data-hotkeys/artifacts/hotkeys.log"

step "Selftest: settings"
run_selftest settings || fail=1
# Login-item mutation is skipped in selftests; every remaining apply error fails.
expect 'SELFTEST settings: captureKey=CmdOrCtrl+Shift+1' "$run_dir/data-settings/artifacts/settings.log"
expect 'SELFTEST settings: pinKey=CmdOrCtrl+Shift+2' "$run_dir/data-settings/artifacts/settings.log"
expect 'SELFTEST settings: ocrKey=CmdOrCtrl+Shift+O' "$run_dir/data-settings/artifacts/settings.log"

step "Selftest: permission center"
run_selftest perms || fail=1
expect 'SELFTEST perms: categories=7' "$run_dir/data-perms/artifacts/perms.log"
expect 'SELFTEST perms: screen=' "$run_dir/data-perms/artifacts/perms.log"
expect 'SELFTEST perms: actions=.*screen-request' "$run_dir/data-perms/artifacts/perms.log"
expect 'SELFTEST perms: actions=.*accessibility-check' "$run_dir/data-perms/artifacts/perms.log"
expect 'SELFTEST perms: actions=.*login-open' "$run_dir/data-perms/artifacts/perms.log"
expect 'SELFTEST perms: banner=true' "$run_dir/data-perms/artifacts/perms.log"
expect 'SELFTEST perms: checks=ok' "$run_dir/data-perms/artifacts/perms.log"

step "Selftest: text style controls"
run_selftest textstyle || fail=1
expect 'SELFTEST textstyle: props=true width_hidden=true' "$run_dir/data-textstyle/artifacts/textstyle.log"
expect 'bold=true italic=true size=20' "$run_dir/data-textstyle/artifacts/textstyle.log"
expect 'input=true commit=sent input_gone=true' "$run_dir/data-textstyle/artifacts/textstyle.log"
expect 'SELFTEST textstyle: checks=ok' "$run_dir/data-textstyle/artifacts/textstyle.log"

step "Selftest: colour palette panel"
run_selftest palette || fail=1
expect 'SELFTEST palette: open=true' "$run_dir/data-palette/artifacts/palette.log"
expect 'tooltips=true' "$run_dir/data-palette/artifacts/palette.log"
expect 'esc_kept_annotating=true' "$run_dir/data-palette/artifacts/palette.log"
expect 'SELFTEST palette: main_toggles=0' "$run_dir/data-palette/artifacts/palette.log"
expect 'SELFTEST palette: checks=ok' "$run_dir/data-palette/artifacts/palette.log"

printf '\n'
if [ "$fail" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "SOME CHECKS FAILED"
fi
exit "$fail"
