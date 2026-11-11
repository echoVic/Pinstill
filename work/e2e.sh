#!/usr/bin/env bash
# End-to-end verification for Pinstill (macOS).
#
# Usage: bash work/e2e.sh [--skip-build]
# Produces selftest artifacts in /tmp/pinstill-selftest-*.png and logs in
# /tmp/pinstill-selftest-*.log.
set -uo pipefail
cd "$(dirname "$0")/.."

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

# expect greps a selftest log line and names the failing check.
expect() {
  if grep -q "$1" "$2"; then
    return 0
  fi
  printf 'FAIL  missing "%s" in %s\n' "$1" "$2"
  fail=1
}

skip_build=0
if [ "${1:-}" = "--skip-build" ]; then
  skip_build=1
fi

# One instance only: a running Pinstill would swallow the selftest launches.
pkill -f 'Pinstill.app/Contents/MacOS/Pinstill' >/dev/null 2>&1 || true

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

APP="build/darwin-arm64/Pinstill.app/Contents/MacOS/Pinstill"
if [ ! -x "$APP" ]; then
  echo "missing build output: $APP"
  exit 1
fi

step "Selftest: real quit path"
PINSTILL_SELFTEST=quit "$APP" > /tmp/pinstill-selftest-quit.log 2>&1 &
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
cat /tmp/pinstill-selftest-quit.log
expect 'quit sequence finished' /tmp/pinstill-selftest-quit.log

step "Selftest: native capture, history and clipboard"
PINSTILL_SELFTEST=capture "$APP" 2>&1 | tee /tmp/pinstill-selftest-capture.log
sleep 1.0
expect 'SELFTEST capture: ok' /tmp/pinstill-selftest-capture.log
expect 'SELFTEST capture: clipboard ok' /tmp/pinstill-selftest-capture.log
expect 'SELFTEST capture: history entry' /tmp/pinstill-selftest-capture.log

step "Selftest: overlay rendering"
PINSTILL_SELFTEST=overlay "$APP" 2>&1 | tee /tmp/pinstill-selftest-overlay.log
sleep 1.0
expect 'SELFTEST overlay: ok' /tmp/pinstill-selftest-overlay.log

step "Selftest: overlay selection mapping"
PINSTILL_SELFTEST=drag "$APP" 2>&1 | tee /tmp/pinstill-selftest-drag.log
sleep 1.0
expect 'SELFTEST drag: delivered' /tmp/pinstill-selftest-drag.log
expect 'same=true' /tmp/pinstill-selftest-drag.log

step "Selftest: escape cancels the overlay"
PINSTILL_SELFTEST=esc "$APP" 2>&1 | tee /tmp/pinstill-selftest-esc.log
sleep 1.0
expect 'overlay visible=false' /tmp/pinstill-selftest-esc.log
expect 'main visible=true' /tmp/pinstill-selftest-esc.log

step "Selftest: desktop action flow (drag + toolbar)"
PINSTILL_SELFTEST=flow "$APP" 2>&1 | tee /tmp/pinstill-selftest-flow.log
sleep 1.0
expect 'SELFTEST flow: toolbar_drag=60,40' /tmp/pinstill-selftest-flow.log
expect 'SELFTEST flow: copy clipboard' /tmp/pinstill-selftest-flow.log
grep -q 'SELFTEST flow: copy clipboard EMPTY' /tmp/pinstill-selftest-flow.log && fail=1
expect 'SELFTEST flow: pin pins=1' /tmp/pinstill-selftest-flow.log
expect 'SELFTEST flow: cancel history before=.* after=' /tmp/pinstill-selftest-flow.log
expect 'SELFTEST flow: auto clipboard ' /tmp/pinstill-selftest-flow.log

step "Selftest: double-click copies the capture"
PINSTILL_SELFTEST=dblclick "$APP" 2>&1 | tee /tmp/pinstill-selftest-dblclick.log
sleep 1.0
expect 'SELFTEST dblclick: first=clicked second=clicked clipboard=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-dblclick.log
grep -q 'clipboard=0x0' /tmp/pinstill-selftest-dblclick.log && fail=1
expect 'SELFTEST dblclick: checks=ok' /tmp/pinstill-selftest-dblclick.log

step "Selftest: desktop pin interactions"
PINSTILL_SELFTEST=pinui "$APP" 2>&1 | tee /tmp/pinstill-selftest-pinui.log
sleep 1.0
expect 'SELFTEST pinui: dblclick clipboard ' /tmp/pinstill-selftest-pinui.log
expect 'SELFTEST pinui: bounds unchanged=true' /tmp/pinstill-selftest-pinui.log
expect 'SELFTEST pinui: menu copy=clicked' /tmp/pinstill-selftest-pinui.log
expect 'SELFTEST pinui: opacity before=100 after=90' /tmp/pinstill-selftest-pinui.log
expect 'SELFTEST pinui: wheel bounds=.* scaled=true' /tmp/pinstill-selftest-pinui.log
expect 'SELFTEST pinui: after close pins=0 stored=0' /tmp/pinstill-selftest-pinui.log

step "Selftest: desktop pin persistence"
PINSTILL_SELFTEST=pinpersist-setup "$APP" > /tmp/pinstill-selftest-pinpersist-setup.log 2>&1 &
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
cat /tmp/pinstill-selftest-pinpersist-setup.log
expect 'SELFTEST pinpersist: setup created' /tmp/pinstill-selftest-pinpersist-setup.log
PINSTILL_SELFTEST=pinpersist-check "$APP" 2>&1 | tee /tmp/pinstill-selftest-pinpersist-check.log
sleep 1.0
expect 'SELFTEST pinpersist: check restored=[1-9]' /tmp/pinstill-selftest-pinpersist-check.log
expect 'SELFTEST pinpersist: check after close stored=0' /tmp/pinstill-selftest-pinpersist-check.log

step "Selftest: scrolling capture (auto finish + preview)"
PINSTILL_SELFTEST=scroll PINSTILL_SELFTEST_SCROLL_SIM=1 "$APP" 2>&1 | tee /tmp/pinstill-selftest-scroll.log
sleep 1.0
expect 'SELFTEST scroll: accessibility=' /tmp/pinstill-selftest-scroll.log
expect 'SELFTEST scroll: result=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-scroll.log
expect 'SELFTEST scroll: clipboard=1280x' /tmp/pinstill-selftest-scroll.log
expect 'SELFTEST scroll: closed_after_copy=true' /tmp/pinstill-selftest-scroll.log

step "Selftest: scrolling capture through the page"
PINSTILL_SELFTEST=scrollflow PINSTILL_SELFTEST_SCROLL_SIM=1 "$APP" 2>&1 | tee /tmp/pinstill-selftest-scrollflow.log
sleep 1.0
expect 'SELFTEST scrollflow: result=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-scrollflow.log
expect 'SELFTEST scrollflow: image=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-scrollflow.log
expect 'actions=.*copy' /tmp/pinstill-selftest-scrollflow.log
expect 'actions=.*save' /tmp/pinstill-selftest-scrollflow.log
expect 'actions=.*pin' /tmp/pinstill-selftest-scrollflow.log
expect 'actions=.*close' /tmp/pinstill-selftest-scrollflow.log
expect 'SELFTEST scrollflow: closed=true' /tmp/pinstill-selftest-scrollflow.log

step "Selftest: scrolling capture (manual mode auto finish)"
PINSTILL_SELFTEST=scrollmanual PINSTILL_SELFTEST_SCROLL_SIM=1 "$APP" 2>&1 | tee /tmp/pinstill-selftest-scrollmanual.log
sleep 1.0
expect 'SELFTEST scrollmanual: result=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-scrollmanual.log
expect 'result=.*window=true' /tmp/pinstill-selftest-scrollmanual.log
expect 'SELFTEST scrollmanual: checks=ok' /tmp/pinstill-selftest-scrollmanual.log

step "Selftest: whole-display capture"
PINSTILL_SELFTEST=fullscreen "$APP" 2>&1 | tee /tmp/pinstill-selftest-fullscreen.log
sleep 1.0
expect 'SELFTEST fullscreen: clipboard=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-fullscreen.log
grep -q 'SELFTEST fullscreen: clipboard=0x0' /tmp/pinstill-selftest-fullscreen.log && fail=1
expect 'history before=[0-9][0-9]* after=[0-9][0-9]*' /tmp/pinstill-selftest-fullscreen.log
expect 'SELFTEST fullscreen: checks=ok' /tmp/pinstill-selftest-fullscreen.log

step "Selftest: history actions (copy/pin/remove)"
PINSTILL_SELFTEST=historyactions "$APP" 2>&1 | tee /tmp/pinstill-selftest-historyactions.log
sleep 1.0
expect 'historyactions: clipboard=[0-9][0-9]*x[0-9][0-9]*' /tmp/pinstill-selftest-historyactions.log
expect 'pins=1' /tmp/pinstill-selftest-historyactions.log
expect 'remove .*delta=1' /tmp/pinstill-selftest-historyactions.log
expect 'SELFTEST historyactions: checks=ok' /tmp/pinstill-selftest-historyactions.log

step "Selftest: main window views"
PINSTILL_SELFTEST=uicapture "$APP" 2>&1 | tee /tmp/pinstill-selftest-uicapture.log
sleep 1.0
expect 'SELFTEST uicapture: views=history,settings' /tmp/pinstill-selftest-uicapture.log
expect 'SELFTEST uicapture: editor_dom=false' /tmp/pinstill-selftest-uicapture.log
expect 'SELFTEST uicapture: preview=true' /tmp/pinstill-selftest-uicapture.log
expect 'SELFTEST uicapture: preview_closed=false' /tmp/pinstill-selftest-uicapture.log
expect 'SELFTEST uicapture: settings ' /tmp/pinstill-selftest-uicapture.log

step "Selftest: pin window"
PINSTILL_SELFTEST=pin "$APP" 2>&1 | tee /tmp/pinstill-selftest-pin.log
sleep 1.0
expect 'SELFTEST pin: window' /tmp/pinstill-selftest-pin.log

step "Selftest: global shortcuts"
PINSTILL_SELFTEST=hotkeys "$APP" 2>&1 | tee /tmp/pinstill-selftest-hotkeys.log
sleep 1.0
expect 'captureKey .* registered=true' /tmp/pinstill-selftest-hotkeys.log

step "Selftest: settings"
PINSTILL_SELFTEST=settings "$APP" 2>&1 | tee /tmp/pinstill-selftest-settings.log
sleep 1.0
# apply errors are expected for launchAtLogin when the app is not in /Applications;
# what matters is that the three shortcuts registered with the new defaults.
expect 'SELFTEST settings: captureKey=CmdOrCtrl+Shift+1' /tmp/pinstill-selftest-settings.log
expect 'SELFTEST settings: pinKey=CmdOrCtrl+Shift+2' /tmp/pinstill-selftest-settings.log
expect 'SELFTEST settings: ocrKey=CmdOrCtrl+Shift+O' /tmp/pinstill-selftest-settings.log

step "Selftest: permission center"
PINSTILL_SELFTEST=perms "$APP" 2>&1 | tee /tmp/pinstill-selftest-perms.log
sleep 1.0
expect 'SELFTEST perms: categories=7' /tmp/pinstill-selftest-perms.log
expect 'SELFTEST perms: screen=' /tmp/pinstill-selftest-perms.log
expect 'SELFTEST perms: actions=.*screen-request' /tmp/pinstill-selftest-perms.log
expect 'SELFTEST perms: actions=.*accessibility-check' /tmp/pinstill-selftest-perms.log
expect 'SELFTEST perms: actions=.*login-open' /tmp/pinstill-selftest-perms.log
expect 'SELFTEST perms: banner=true' /tmp/pinstill-selftest-perms.log
expect 'SELFTEST perms: checks=ok' /tmp/pinstill-selftest-perms.log

step "Selftest: text style controls"
PINSTILL_SELFTEST=textstyle "$APP" 2>&1 | tee /tmp/pinstill-selftest-textstyle.log
sleep 1.0
expect 'SELFTEST textstyle: props=true width_hidden=true' /tmp/pinstill-selftest-textstyle.log
expect 'bold=true italic=true size=20' /tmp/pinstill-selftest-textstyle.log
expect 'input=true commit=sent input_gone=true' /tmp/pinstill-selftest-textstyle.log
expect 'SELFTEST textstyle: checks=ok' /tmp/pinstill-selftest-textstyle.log

step "Selftest: colour palette panel"
PINSTILL_SELFTEST=palette "$APP" 2>&1 | tee /tmp/pinstill-selftest-palette.log
sleep 1.0
expect 'SELFTEST palette: open=true' /tmp/pinstill-selftest-palette.log
expect 'esc_kept_annotating=true' /tmp/pinstill-selftest-palette.log
expect 'SELFTEST palette: main_toggles=0' /tmp/pinstill-selftest-palette.log
expect 'SELFTEST palette: checks=ok' /tmp/pinstill-selftest-palette.log

printf '\n'
if [ "$fail" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "SOME CHECKS FAILED"
fi
exit "$fail"
