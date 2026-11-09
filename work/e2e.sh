#!/usr/bin/env bash
# End-to-end verification for Tupen (macOS).
#
# Usage: bash work/e2e.sh [--skip-build]
# Produces selftest artifacts in /tmp/tupen-selftest-*.png and logs in
# /tmp/tupen-selftest-*.log.
set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
step() { printf '\n== %s ==\n' "$1"; }
check() {
  local name="$1"
  shift
  if "$@"; then
    printf 'PASS  %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"
    fail=1
  fi
}

skip_build=0
if [ "${1:-}" = "--skip-build" ]; then
  skip_build=1
fi

# One instance only: a running Tupen would swallow the selftest launches.
pkill -f 'Tupen.app/Contents/MacOS/Tupen' >/dev/null 2>&1 || true

step "Go tests"
GOTOOLCHAIN=auto go test ./... || fail=1

step "Go vet"
GOTOOLCHAIN=auto go vet ./... || fail=1

step "Windows cross-compile"
GOOS=windows GOARCH=amd64 GOTOOLCHAIN=auto go build ./... || fail=1

if [ "$skip_build" -eq 0 ]; then
  step "Frontend build"
  npm run build:web || fail=1

  step "Desktop app build"
  ./node_modules/.bin/mygo build || fail=1
fi

APP="build/darwin-arm64/Tupen.app/Contents/MacOS/Tupen"
if [ ! -x "$APP" ]; then
  echo "missing build output: $APP"
  exit 1
fi

step "Selftest: real quit path"
TUPEN_SELFTEST=quit "$APP" > /tmp/tupen-selftest-quit.log 2>&1 &
qpid=$!
for _ in $(seq 1 30); do
  if ! kill -0 "$qpid" 2>/dev/null; then break; fi
  sleep 0.5
done
if kill -0 "$qpid" 2>/dev/null; then
  echo "quit did not finish (blocked)"
  kill -9 "$qpid" 2>/dev/null
  fail=1
else
  wait "$qpid" 2>/dev/null
  qrc=$?
  if [ "$qrc" -ne 0 ]; then
    echo "quit exited with code $qrc"
    fail=1
  fi
fi
cat /tmp/tupen-selftest-quit.log
grep -q 'quit sequence finished' /tmp/tupen-selftest-quit.log || fail=1

step "Selftest: native capture, history and clipboard"
TUPEN_SELFTEST=capture "$APP" 2>&1 | tee /tmp/tupen-selftest-capture.log
sleep 1.0
grep -q 'SELFTEST capture: ok' /tmp/tupen-selftest-capture.log || fail=1
grep -q 'SELFTEST capture: clipboard ok' /tmp/tupen-selftest-capture.log || fail=1
grep -q 'SELFTEST capture: history entry' /tmp/tupen-selftest-capture.log || fail=1

step "Selftest: overlay rendering"
TUPEN_SELFTEST=overlay "$APP" 2>&1 | tee /tmp/tupen-selftest-overlay.log
sleep 1.0
grep -q 'SELFTEST overlay: ok' /tmp/tupen-selftest-overlay.log || fail=1

step "Selftest: overlay selection mapping"
TUPEN_SELFTEST=drag "$APP" 2>&1 | tee /tmp/tupen-selftest-drag.log
sleep 1.0
grep -q 'SELFTEST drag: delivered' /tmp/tupen-selftest-drag.log || fail=1
grep -q 'same=true' /tmp/tupen-selftest-drag.log || fail=1
grep -q 'editor canvas=' /tmp/tupen-selftest-drag.log || fail=1

step "Selftest: escape cancels the overlay"
TUPEN_SELFTEST=esc "$APP" 2>&1 | tee /tmp/tupen-selftest-esc.log
sleep 1.0
grep -q 'overlay visible=false' /tmp/tupen-selftest-esc.log || fail=1
grep -q 'main visible=true' /tmp/tupen-selftest-esc.log || fail=1

step "Selftest: desktop action flow (drag + toolbar)"
TUPEN_SELFTEST=flow "$APP" 2>&1 | tee /tmp/tupen-selftest-flow.log
sleep 1.0
grep -q 'SELFTEST flow: copy clipboard' /tmp/tupen-selftest-flow.log || fail=1
grep -q 'SELFTEST flow: copy clipboard EMPTY' /tmp/tupen-selftest-flow.log && fail=1
grep -q 'SELFTEST flow: pin pins=1' /tmp/tupen-selftest-flow.log || fail=1
grep -q 'SELFTEST flow: annotate editor canvas=' /tmp/tupen-selftest-flow.log || fail=1
grep -q 'SELFTEST flow: cancel history before=.* after=' /tmp/tupen-selftest-flow.log || fail=1
grep -q 'SELFTEST flow: auto clipboard ' /tmp/tupen-selftest-flow.log || fail=1

step "Selftest: desktop pin interactions"
TUPEN_SELFTEST=pinui "$APP" 2>&1 | tee /tmp/tupen-selftest-pinui.log
sleep 1.0
grep -q 'SELFTEST pinui: dblclick clipboard ' /tmp/tupen-selftest-pinui.log || fail=1
grep -q 'SELFTEST pinui: bounds unchanged=true' /tmp/tupen-selftest-pinui.log || fail=1
grep -q 'SELFTEST pinui: menu copy=clicked' /tmp/tupen-selftest-pinui.log || fail=1
grep -q 'SELFTEST pinui: opacity before=100 after=90' /tmp/tupen-selftest-pinui.log || fail=1
grep -q 'SELFTEST pinui: wheel bounds=.* scaled=true' /tmp/tupen-selftest-pinui.log || fail=1
grep -q 'SELFTEST pinui: after close pins=0 stored=0' /tmp/tupen-selftest-pinui.log || fail=1

step "Selftest: desktop pin persistence"
TUPEN_SELFTEST=pinpersist-setup "$APP" > /tmp/tupen-selftest-pinpersist-setup.log 2>&1 &
sleep 0.3
ppid_setup=$!
for _ in $(seq 1 30); do
  if ! kill -0 "$ppid_setup" 2>/dev/null; then break; fi
  sleep 0.5
done
if kill -0 "$ppid_setup" 2>/dev/null; then
  echo "pinpersist setup did not quit"
  kill -9 "$ppid_setup" 2>/dev/null
  fail=1
fi
wait "$ppid_setup" 2>/dev/null
cat /tmp/tupen-selftest-pinpersist-setup.log
grep -q 'SELFTEST pinpersist: setup created' /tmp/tupen-selftest-pinpersist-setup.log || fail=1
TUPEN_SELFTEST=pinpersist-check "$APP" 2>&1 | tee /tmp/tupen-selftest-pinpersist-check.log
sleep 1.0
grep -q 'SELFTEST pinpersist: check restored=[1-9]' /tmp/tupen-selftest-pinpersist-check.log || fail=1
grep -q 'SELFTEST pinpersist: check after close stored=0' /tmp/tupen-selftest-pinpersist-check.log || fail=1

step "Selftest: editor delivery"
TUPEN_SELFTEST=deliver "$APP" 2>&1 | tee /tmp/tupen-selftest-deliver.log
sleep 1.0
grep -q 'SELFTEST deliver: screenshot' /tmp/tupen-selftest-deliver.log || fail=1
grep -q 'editor page capture' /tmp/tupen-selftest-deliver.log || fail=1

step "Selftest: toolbar actions through the page"
TUPEN_SELFTEST=uiactions "$APP" 2>&1 | tee /tmp/tupen-selftest-uiactions.log
sleep 1.0
grep -q 'clipboard png' /tmp/tupen-selftest-uiactions.log || fail=1
grep -q 'pins=1' /tmp/tupen-selftest-uiactions.log || fail=1
grep -q 'history cards=' /tmp/tupen-selftest-uiactions.log || fail=1

step "Selftest: main window views"
TUPEN_SELFTEST=uicapture "$APP" 2>&1 | tee /tmp/tupen-selftest-uicapture.log
sleep 1.0
grep -q 'rack ' /tmp/tupen-selftest-uicapture.log || fail=1
grep -q 'history ' /tmp/tupen-selftest-uicapture.log || fail=1
grep -q 'settings ' /tmp/tupen-selftest-uicapture.log || fail=1

step "Selftest: pin window"
TUPEN_SELFTEST=pin "$APP" 2>&1 | tee /tmp/tupen-selftest-pin.log
sleep 1.0
grep -q 'SELFTEST pin: window' /tmp/tupen-selftest-pin.log || fail=1

step "Selftest: global shortcuts"
TUPEN_SELFTEST=hotkeys "$APP" 2>&1 | tee /tmp/tupen-selftest-hotkeys.log
sleep 1.0
grep -q 'captureKey .* registered=true' /tmp/tupen-selftest-hotkeys.log || fail=1

step "Selftest: settings"
TUPEN_SELFTEST=settings "$APP" 2>&1 | tee /tmp/tupen-selftest-settings.log
sleep 1.0
# apply errors are expected for launchAtLogin when the app is not in /Applications;
# what matters is that the three shortcuts registered with the new defaults.
grep -q 'SELFTEST settings: captureKey=CmdOrCtrl+Shift+1' /tmp/tupen-selftest-settings.log || fail=1
grep -q 'SELFTEST settings: pinKey=CmdOrCtrl+Shift+2' /tmp/tupen-selftest-settings.log || fail=1
grep -q 'SELFTEST settings: ocrKey=CmdOrCtrl+Shift+O' /tmp/tupen-selftest-settings.log || fail=1

printf '\n'
if [ "$fail" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "SOME CHECKS FAILED"
fi
exit "$fail"
