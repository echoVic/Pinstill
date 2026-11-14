import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import ts from 'typescript';
import { page, deferred, settle } from './harness.mjs';

// Execute the production bridge with only its native/runtime boundaries mocked.
function resultBridge(hasNative) {
  const calls = [];
  const Capture = new Proxy({}, { get: (_, method) => (...args) => {
    calls.push([method, ...args]);
    return Promise.resolve(method === 'saveScrollResult' ? '/tmp/result.png' : method === 'pinScrollResult' ? 'pin' : undefined);
  } });
  const exports = {};
  const code = ts.transpileModule(fs.readFileSync(new URL('../src/bridge.ts', import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  vm.runInNewContext(code, { exports, Error, require: name => {
    if (name === 'mygo-runtime') return { isMyGo: () => hasNative };
    if (name === './mygo') return { Capture, History: {}, events: {} };
    if (name === './util') return {};
    throw new Error(`Unexpected module ${name}`);
  } });
  return { bridge: exports, calls };
}

const methods = ['copyScrollResult', 'saveScrollResult', 'pinScrollResult', 'closeScrollResult'];
for (const method of methods) {
  test(`${method} forwards the result token and rejects missing tokens before native calls`, async () => {
    const { bridge, calls } = resultBridge(true);
    await bridge[method]('result-a');
    assert.deepEqual(calls, [[method, 'result-a']]);
    for (const id of ['', '  ', undefined]) await assert.rejects(() => bridge[method](id), /缺少长图结果 ID/);
    assert.equal(calls.length, 1);
  });
  test(`${method} keeps demo calls compatible with nonempty result tokens`, async () => {
    const { bridge, calls } = resultBridge(false);
    await bridge[method]('demo-result');
    assert.equal(calls.length, 0);
    await assert.rejects(() => bridge[method](''), /缺少长图结果 ID/);
  });
}

const result = id => ({ id, url: `shot://${id}`, width: 120, height: 240, frames: 2, capped: false });
for (const action of ['copy', 'save', 'pin']) {
  test(`result ${action} carries the original ID and its late success cannot close a newer preview`, async () => {
    const calls = [], closes = [];
    const p = page('result', { bridge: {
      [`${action}ScrollResult`]: id => { const d = deferred(); calls.push({ id, ...d }); return d.promise; },
      closeScrollResult: async id => closes.push(id),
    } });
    p.emit('onScrollResult', result('result-a'));
    const old = p.run(`runAction("${action}", null)`);
    assert.equal(calls[0].id, 'result-a');
    p.emit('onScrollResult', result('result-b'));
    calls[0].resolve('saved-or-pinned'); await old; await p.tick(320);
    assert.equal(p.document.getElementById('resultImage').src, 'shot://result-b');
    assert.equal(p.document.querySelectorAll('.result-closed').length, 0);
    assert.equal(p.document.getElementById('resultStatus').textContent, '');
    assert.equal(p.document.querySelector(`[data-result-action="${action}"]`).disabled, false);
    assert.equal(closes.length, 0);
    const latest = p.run(`runAction("${action}", null)`);
    assert.equal(calls[1].id, 'result-b');
    calls[1].resolve('saved-or-pinned'); await latest; await p.tick(320);
    assert.deepEqual(closes, ['result-b']);
  });
}

for (const outcome of ['rejection', 'cancelled save', 'close delay']) {
  test(`old result ${outcome} cannot change the newer preview`, async () => {
    const request = deferred(); let closes = 0;
    const p = page('result', { bridge: { saveScrollResult: () => request.promise, closeScrollResult: async () => { closes++; } } });
    p.emit('onScrollResult', result('result-a'));
    const old = p.run('runAction("save", null)');
    if (outcome === 'close delay') { request.resolve('/tmp/result.png'); await old; }
    p.emit('onScrollResult', result('result-b'));
    if (outcome === 'rejection') request.reject(new Error('old failure'));
    else request.resolve('');
    await old; await p.tick(320);
    assert.equal(p.document.getElementById('resultStatus').textContent, '');
    assert.equal(p.document.querySelector('[data-result-action="save"]').disabled, false);
    assert.equal(p.document.querySelectorAll('.result-closed').length, 0);
    assert.equal(closes, 0);
  });
}

test('native IDless result reports a visible error and cannot issue an action', async () => {
  let copies = 0;
  const p = page('result', { bridge: { copyScrollResult: async () => { copies++; } } });
  const invalid = result(''); delete invalid.id;
  p.emit('onScrollResult', invalid);
  assert.match(p.document.getElementById('resultStatus').textContent, /缺少长图结果 ID/);
  assert.equal(p.document.getElementById('resultLoading').hidden, true, 'invalid events cannot leave an unresolved spinner');
  await p.run('runAction("copy", null)'); await p.tick(2500);
  assert.equal(copies, 0);
  assert.match(p.document.getElementById('resultStatus').textContent, /缺少长图结果 ID/);
});

test('native result close snapshots the current ID before closing', () => {
  const closes = [];
  const p = page('result', { bridge: { closeScrollResult: async id => closes.push(id) } });
  p.emit('onScrollResult', result('result-a')); p.run('dismiss()');
  assert.deepEqual(closes, ['result-a']);
});

test('result demo creates a token and remains usable without native calls', async () => {
  const p = page('result', { bridge: { hasNative: false } });
  assert.ok(p.run('payload.id'));
  await p.run('runAction("copy", null)'); await p.tick(700);
  assert.equal(p.document.querySelectorAll('.result-closed').length, 1);
});
