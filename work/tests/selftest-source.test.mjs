import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

// Execute the existing Go mode body with native exit and time replaced. The
// empty in-memory pin stores model a failed restore, without MyGo or disk data.
// This also reproduces the original false green before the result helper exists.
test('pin persistence rejects an empty restore instead of reporting success', (t) => {
  const source = readFileSync(new URL('../../internal/app/selftest.go', import.meta.url), 'utf8');
  const start = source.indexOf('func (a *App) selftestPinPersistCheck()');
  const end = source.indexOf('\n}\n', start) + 3;
  assert.ok(start > 0 && end > start);
  const body = source.slice(start, end).replaceAll('mygo.App.Exit(', 'os.Exit(').replaceAll(/time\.Sleep\([^\n]+\)/g, '');
  const dir = mkdtempSync(join(tmpdir(), 'pinstill-mode-test-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  writeFileSync(join(dir, 'main.go'), `package main
import ("fmt"; "os"; "sync")
type store struct{}
func (*store) List() []int { return nil }
type App struct { mu sync.Mutex; pins map[string]int; pinStore *store }
func (*App) closePin(string) error { return nil }
func selftestFail(format string, args ...any) { fmt.Printf(format, args...); os.Exit(1) }
func selftestRequire(ok bool, format string, args ...any) { if !ok { selftestFail(format, args...) } }
func selftestExit(code int) { os.Exit(code) }
func main() { (&App{}).selftestPinPersistCheck() }
${body}`);
  const result = spawnSync('go', ['run', join(dir, 'main.go')], { encoding: 'utf8', env: { ...process.env, GOTOOLCHAIN: 'auto' } });
  assert.ifError(result.error);
  assert.doesNotMatch(result.stderr, /undefined:|syntax error|cannot use/);
  assert.notEqual(result.status, 0, `empty restore passed: ${result.stdout}${result.stderr}`);
  assert.doesNotMatch(result.stdout, /checks=ok/);
});
