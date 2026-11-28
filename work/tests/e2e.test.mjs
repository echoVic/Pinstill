import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const logs = JSON.parse(readFileSync(new URL('./fixtures/selftest-logs.json', import.meta.url), 'utf8'));

// Execute the actual harness in a disposable checkout. All native/build/process
// commands are doubles; even the shell's kill builtin is replaced by BASH_ENV.
function fixture(t, options = {}) {
  const root = mkdtempSync(join(tmpdir(), 'pinstill-harness-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const checkout = join(root, 'checkout with spaces');
  const bin = join(root, 'bin');
  for (const dir of [bin, join(checkout, 'work'), join(checkout, 'node_modules/.bin'), join(root, 'tmp'), join(root, 'personal-data'), join(root, 'logs')]) mkdirSync(dir, { recursive: true });
  // Redirect the old hardcoded logs too, so RED runs cannot touch shared /tmp files.
  let script = readFileSync(join(repo, 'work/e2e.sh'), 'utf8').replaceAll('/tmp/pinstill-selftest-', `${root}/legacy-selftest-`);
  // Most failure tests need only the real runner and preflight. Keep the two
  // full traversals below for all case wiring; avoid replaying every fake app for
  // each injected failure. Slice only at the stable case-list boundary.
  if (options.modes) {
    const boundary = script.indexOf('\nstep "Selftest: real quit path"');
    assert.ok(boundary > 0);
    script = script.slice(0, boundary) + '\npin_data="$run_dir/data-pinpersist"\n' +
      options.modes.map(mode => `run_selftest ${mode}${mode.startsWith('pinpersist-') ? ' "$pin_data"' : ''} || fail=1`).join('\n') +
      '\n[ "$fail" -eq 0 ] || echo "SOME CHECKS FAILED"\nexit "$fail"\n';
  }
  writeFileSync(join(checkout, 'work/e2e.sh'), script);
  writeFileSync(join(root, 'personal-data/sentinel'), 'keep');
  for (const [mode, log] of Object.entries(logs)) writeFileSync(join(root, `logs/${mode}.log`), `${log}\n`);
  const executable = (path, text) => { writeFileSync(path, `#!/bin/bash\n${text}\n`); chmodSync(path, 0o755); };
  writeFileSync(join(root, 'bash-env'), `
sleep() { if [ "\${TEST_TIMEOUT_MODES:-}" != "" ] && [[ ",$TEST_TIMEOUT_MODES," == *",\${mode:-},"* ]]; then SECONDS=$((SECONDS + 120)); fi; }
kill() {
  if [ "$1" = -0 ]; then
    if [[ ",\${TEST_TIMEOUT_MODES:-}," == *",\${mode:-},"* ]] && [ -n "\${mode:-}" ]; then return 0; fi
    return 1
  fi
  printf '%s|%s|%s\\n' "\${mode:-}" "$*" "\${pid:-}" >> "$TEST_ROOT/signals"
  return 0
}
uname() { case "$1" in -s) echo "\${TEST_OS:-Darwin}";; -m) echo "\${TEST_ARCH:-arm64}";; esac; }
pgrep() { return "\${TEST_PGREP_STATUS:-1}"; }
pkill() { echo pkill >> "$TEST_ROOT/forbidden"; return 1; }
go() { printf 'go %s\\n' "$*" >> "$TEST_ROOT/commands"; [ "\${TEST_FAIL_COMMAND:-}" != "go $*" ]; }
npm() { printf 'npm %s\\n' "$*" >> "$TEST_ROOT/commands"; [ "\${TEST_FAIL_COMMAND:-}" != "npm $*" ]; }
`);
  for (const cmd of ['go', 'npm', 'mygo']) executable(join(bin, cmd), `printf '%s\\n' '${cmd} '"$*" >> "$TEST_ROOT/commands"
if [ "\${TEST_FAIL_COMMAND:-}" = '${cmd} '"$*" ] || [ "\${TEST_FAIL_COMMAND:-}" = '${cmd}' ]; then exit 17; fi`);
  copyFileSync(join(bin, 'mygo'), join(checkout, 'node_modules/.bin/mygo'));
  executable(join(bin, 'sleep'), 'exit 0');
  executable(join(bin, 'pkill'), 'echo pkill >> "$TEST_ROOT/forbidden"; exit 1');
  executable(join(bin, 'pgrep'), 'exit "${TEST_PGREP_STATUS:-1}"');
  executable(join(bin, 'uname'), 'case "$1" in -s) echo "${TEST_OS:-Darwin}";; -m) echo "${TEST_ARCH:-arm64}";; *) exit 2;; esac');
  executable(join(bin, 'mktemp'), `if [ "\${TEST_FAIL_MKTEMP:-}" = 1 ]; then exit 17; fi
exec /usr/bin/mktemp "$@"`);
  executable(join(bin, 'tee'), `"\${TEST_REAL_TEE}" "$@"
if [ "\${TEST_FAIL_TEE:-}" = 1 ]; then exit 17; fi`);
  const appDir = join(checkout, `build/darwin-${options.arch === 'x86_64' ? 'amd64' : 'arm64'}/Pinstill.app/Contents/MacOS`);
  mkdirSync(appDir, { recursive: true });
  executable(join(appDir, 'Pinstill'), `mode="$PINSTILL_SELFTEST"
data="\${PINSTILL_DATA_DIR:-}"
printf '%s|%s|%s\\n' "$mode" "$data" "\${PINSTILL_SELFTEST_SCROLL_SIM:-}" >> "$TEST_ROOT/launches"
# No writes are allowed outside this fixture, even when testing broken code.
case "$data" in "$TEST_ROOT"/*) ;; *) echo "unsafe data path" >&2; exit 90;; esac
if [ ! -d "$data" ]; then echo 'missing data directory' >&2; exit 91; fi
if [ "$mode" = pinpersist-check ]; then
  [ -f "$data/visited-pinpersist-setup" ] || { echo 'missing persisted pin' >&2; exit 92; }
else
  for entry in "$data"/*; do
    [[ "$entry" == */artifacts || "$entry" == */.pinstill-selftest ]] || [ ! -e "$entry" ] || printf '%s|%s\\n' "$mode" "$entry" >> "$TEST_ROOT/dirty-launches"
  done
fi
: > "$data/visited-$mode"
if [ -f "$data/.pinstill-selftest" ]; then
  printf '%s\\n' "$mode" >> "$TEST_ROOT/marked-launches"
fi
if [ "$mode" = capture ]; then
  if [ -d "$data/artifacts" ]; then printf 'synthetic' > "$data/artifacts/pinstill-selftest-capture.png"; fi
fi
if [ "\${TEST_MISSING_LOG:-}" != "$mode" ]; then
  while IFS= read -r line; do
    if [ "\${TEST_MISSING_RESULT:-}" = "$mode" ] && [[ "$line" == *checks=ok* ]]; then continue; fi
    printf '%s\\n' "$line"
  done < "$TEST_ROOT/logs/$mode.log"
  if [ "\${TEST_MISSING_RESULT:-}" != "$mode" ]; then printf 'SELFTEST %s: checks=ok\\n' "$mode"; fi
fi
if [ "\${TEST_FALSE_GREEN:-}" = "$mode" ]; then printf 'SELFTEST %s: checks=FAILED\\n' "$mode"; fi
if [ "\${TEST_FAIL_MODE:-}" = "$mode" ]; then exit 23; fi`);
  const env = {
    ...process.env, PATH: `${bin}:/usr/bin:/bin`, BASH_ENV: join(root, 'bash-env'),
    HOME: root, TMPDIR: join(root, 'tmp'), TEST_ROOT: root, TEST_ARCH: options.arch ?? 'arm64',
    TEST_REAL_TEE: '/usr/bin/tee', PINSTILL_DATA_DIR: join(root, 'personal-data'),
    ...options.env,
  };
  function run(args = []) {
    // Fake processes exit promptly; timeout signals are recorded, never sent.
    const result = spawnSync('/bin/bash', [join(checkout, 'work/e2e.sh'), ...args], { cwd: root, env, encoding: 'utf8' });
    assert.ifError(result.error);
    assert.equal(readFileSync(join(root, 'personal-data/sentinel'), 'utf8'), 'keep');
    return { ...result, output: result.stdout + result.stderr };
  }
  const lines = (name) => existsSync(join(root, name)) ? readFileSync(join(root, name), 'utf8').trim().split('\n') : [];
  return { root, checkout, run, lines };
}

test('every case gets fresh data, only the persistence pair shares data, and separate runs never reuse it', (t) => {
  const f = fixture(t);
  for (let i = 0; i < 2; i++) assert.equal(f.run(['--skip-build']).status, 0);
  const launches = f.lines('launches').map(line => line.split('|'));
  const cases = Object.keys(logs);
  assert.equal(launches.length, 2 * cases.length);
  const unique = new Set();
  for (let offset = 0; offset < launches.length; offset += cases.length) {
    const run = launches.slice(offset, offset + cases.length);
    assert.deepEqual(run.map(([mode]) => mode), cases);
    const setup = run.find(([mode]) => mode === 'pinpersist-setup')[1];
    for (const [mode, dir, sim] of run) {
      assert.ok(dir.startsWith(join(f.root, 'tmp') + '/'), `${mode} must override inherited personal data`);
      if (mode === 'pinpersist-check') assert.equal(dir, setup);
      else { assert.ok(!unique.has(dir), `${mode} must have a fresh store`); unique.add(dir); }
      if (['scroll', 'scrollflow', 'scrollmanual'].includes(mode)) assert.equal(sim, '1');
    }
  }
  assert.deepEqual(f.lines('dirty-launches'), []);
  assert.deepEqual(f.lines('forbidden'), []);
  assert.deepEqual(readdirSync(join(f.root, 'personal-data')), ['sentinel']);
});

for (const command of ['go test ./...', 'go vet ./...', 'go build ./...', 'npm run build:web', 'mygo']) {
  test(`${command} failure prevents every app launch, even with an old executable present`, (t) => {
    const f = fixture(t, { env: { TEST_FAIL_COMMAND: command } });
    assert.notEqual(f.run().status, 0);
    assert.deepEqual(f.lines('launches'), []);
    const commands = f.lines('commands');
    assert.ok(commands.at(-1).startsWith(command), `must stop at ${command}: ${commands}`);
  });
}

for (const mode of ['quit', 'capture', 'pinpersist-setup', 'pinpersist-check']) {
  test(`${mode} nonzero exit fails the run despite success log markers`, (t) => {
    const modes = mode.startsWith('pinpersist-') ? ['pinpersist-setup', 'pinpersist-check'] : [mode];
    const f = fixture(t, { modes, env: { TEST_FAIL_MODE: mode } });
    const result = f.run(['--skip-build']);
    assert.notEqual(result.status, 0);
    assert.match(result.output, /SOME CHECKS FAILED/);
    assert.equal(f.lines('launches').length, modes.length, 'remaining independent checks still run');
  });
}

test('missing log assertions and tee errors fail the run', (t) => {
  for (const env of [{ TEST_MISSING_LOG: 'capture' }, { TEST_FAIL_TEE: '1' }]) {
    const f = fixture(t, { modes: ['capture'], env });
    assert.notEqual(f.run(['--skip-build']).status, 0);
  }
});

for (const env of [{ TEST_PGREP_STATUS: '0' }, { TEST_PGREP_STATUS: '2' }, { TEST_FAIL_MKTEMP: '1' }, { TEST_OS: 'Linux' }]) {
  test(`unsafe preflight ${JSON.stringify(env)} refuses to launch without killing processes`, (t) => {
    const f = fixture(t, { modes: ['capture'], env });
    assert.notEqual(f.run(['--skip-build']).status, 0);
    assert.deepEqual(f.lines('launches'), []);
    assert.deepEqual(f.lines('forbidden'), []);
  });
}

test('Intel macOS uses the amd64 bundle and selftest builds skip release packaging', (t) => {
  const f = fixture(t, { arch: 'x86_64', modes: ['capture'] });
  const result = f.run();
  assert.equal(result.status, 0, result.output);
  const build = f.lines('commands').find(line => line.startsWith('mygo build'));
  assert.match(build, /-platform darwin\/amd64/);
  assert.match(build, /-skip-notarize/);
  assert.match(build, /-skip-dmg/);
});

test('invalid arguments fail before builds and launches', (t) => {
  const f = fixture(t);
  assert.notEqual(f.run(['--skip-buid']).status, 0);
  assert.deepEqual(f.lines('commands'), []);
  assert.deepEqual(f.lines('launches'), []);
});

for (const env of [{ TEST_MISSING_RESULT: 'capture' }, { TEST_FALSE_GREEN: 'capture' }]) {
  test(`missing/failed final result fails even with old success logs: ${JSON.stringify(env)}`, (t) => {
    const f = fixture(t, { modes: ['capture'], env });
    assert.notEqual(f.run(['--skip-build']).status, 0);
  });
}

test('every case is bounded, including quit and both persistence runs; only owned PIDs are signaled', (t) => {
  const modes = Object.keys(logs);
  const f = fixture(t, { modes, env: { TEST_TIMEOUT_MODES: modes.join(',') } });
  const result = f.run(['--skip-build']);
  assert.notEqual(result.status, 0);
  const signals = f.lines('signals').map(line => line.split('|'));
  assert.deepEqual(signals.map(([mode]) => mode), modes);
  for (const [mode, args, pid] of signals) {
    assert.match(pid, /^[1-9][0-9]*$/, mode);
    assert.equal(args, `-9 ${pid}`, 'timeout must signal exactly the launched child');
  }
  assert.deepEqual(f.lines('forbidden'), []);
});

test('fresh directories carry the isolation marker and retain artifacts plus logs', (t) => {
  const modes = ['capture', 'pinpersist-setup', 'pinpersist-check'];
  const f = fixture(t, { modes });
  const result = f.run(['--skip-build']);
  assert.equal(result.status, 0, result.output);
  assert.deepEqual(f.lines('marked-launches'), modes);
  for (const line of f.lines('launches')) {
    const [mode, dir] = line.split('|');
    assert.ok(existsSync(join(dir, 'artifacts', `${mode}.log`)), `${mode} log must stay with its data`);
    if (mode === 'capture') assert.equal(readFileSync(join(dir, 'artifacts/pinstill-selftest-capture.png'), 'utf8'), 'synthetic');
  }
});
