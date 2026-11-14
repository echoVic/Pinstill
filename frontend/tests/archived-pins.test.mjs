import test from 'node:test';
import assert from 'node:assert/strict';
import { page, deferred, settle } from './harness.mjs';

const pin = id => ({ id, url: `shot://pin/${id}`, width: 120, height: 80, createdAt: '2026-11-13T00:00:00Z' });

test('history shows archived pins without restoring them automatically', async () => {
  let restores = 0;
  const p = page('index', { bridge: { listArchivedPins: async () => [pin('old')], restoreArchivedPin: async () => { restores++; } } });
  await settle();
  const grid = p.document.getElementById('archivedPinGrid');
  assert.ok(grid, 'history needs an archived-pin list');
  assert.equal(grid.querySelector('img').src, pin('old').url);
  assert.equal(grid.querySelector('button').textContent, '恢复到桌面');
  assert.equal(restores, 0);
});

test('archived pin restoration disables duplicates and refreshes the list on success', async () => {
  const restore = deferred(); let calls = 0, restored = false, lists = 0;
  const p = page('index', { bridge: {
    listArchivedPins: async () => { lists++; return restored ? [] : [pin('old')]; },
    restoreArchivedPin: id => { assert.equal(id, 'old'); calls++; return restore.promise; },
  } });
  await settle();
  const button = p.document.querySelector('#archivedPinGrid button');
  assert.ok(button); button.click(); button.click();
  assert.equal(calls, 1); assert.equal(p.document.querySelector('#archivedPinGrid button').disabled, true);
  restored = true; restore.resolve(); await settle();
  assert.equal(lists, 2); assert.equal(p.document.querySelectorAll('#archivedPinGrid button').length, 0);
  assert.equal(p.document.getElementById('archivedPinEmpty').hidden, false);
});

test('archived restore cap errors preserve the entry and allow retry', async () => {
  let calls = 0;
  const p = page('index', { bridge: { listArchivedPins: async () => [pin('old')],
    restoreArchivedPin: async () => { calls++; throw new Error('最多保留 60 个桌面贴图'); } } });
  await settle();
  const button = p.document.querySelector('#archivedPinGrid button'); assert.ok(button);
  button.click(); await settle();
  assert.equal(button.disabled, false);
  assert.match(p.document.getElementById('archivedPinStatus').textContent, /60/);
  assert.equal(p.document.querySelectorAll('#archivedPinGrid button').length, 1);
  button.click(); await settle(); assert.equal(calls, 2);
});

test('archive refresh ignores late lists and retains entries when a newer load fails', async () => {
  const calls = [];
  const p = page('index', { bridge: { listArchivedPins: () => { const d = deferred(); calls.push(d); return d.promise; } } });
  assert.equal(calls.length, 1);
  const latest = p.run('refreshArchivedPins()'); calls[1].resolve([pin('new')]); await latest;
  calls[0].resolve([pin('old')]); await settle();
  assert.equal(p.document.querySelector('#archivedPinGrid img').src, pin('new').url);
  const failure = p.run('refreshArchivedPins()'); calls[2].reject(new Error('disk offline')); await failure;
  assert.equal(p.document.querySelector('#archivedPinGrid img').src, pin('new').url);
  assert.match(p.document.getElementById('archivedPinStatus').textContent, /disk offline/);
});
