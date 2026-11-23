import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, page, settle } from './harness.mjs';

const entries = ['first', 'second', 'third'].map((id) => ({ id, name: id, thumbUrl: `shot://localhost/l/${id}.thumb.png` }));

test('hanging an annotated capture sends its pixels through the dedicated action', async () => {
  const actions = [];
  const overlay = page('overlay', { bridge: {
    prepareSelection: async (rect) => ({ id: 'capture', url: 'shot://capture', width: rect.width, height: rect.height, rect }),
    finishAnnotated: async (...action) => { actions.push(action); return { completed: true }; },
  } });
  await overlay.run('prepareSelectionFor({ x: 10, y: 10, width: 120, height: 80 })');
  const hang = overlay.document.querySelector('[data-action="hang"]');
  assert.ok(hang);
  assert.notEqual(hang, overlay.document.querySelector('[data-action="pin"]'));
  await overlay.run('runAction("hang")');
  assert.deepEqual(actions, [['hang', Buffer.from('pixels').toString('base64')]]);
});

test('clothesline keeps newer order and moves the selected picture by identity', async () => {
  const initial = deferred();
  const moved = [];
  const line = page('line', { bridge: {
    listClothesline: () => initial.promise,
    moveClotheslineImage: async (...args) => moved.push(args),
  } });
  line.emit('onLineChanged', { revision: 2, entries });
  initial.resolve({ revision: 1, entries: [] });
  await settle();
  line.document.querySelector('[data-line-id="second"]').click();
  line.document.getElementById('lineMoveLeft').click();
  await settle();
  assert.deepEqual(moved, [['second', 'first']]);
  line.emit('onLineChanged', { revision: 3, entries: [entries[1], entries[0], entries[2]] });
  assert.equal(line.document.querySelectorAll('[data-line-id]')[0].dataset.lineId, 'second');
  assert.equal(line.document.getElementById('lineMoveLeft').disabled, true);
});

test('rejected remove keeps the photo selected and the available controls usable', async () => {
  const line = page('line', { bridge: {
    listClothesline: async () => ({ revision: 1, entries }),
    removeClotheslineImage: async () => { throw new Error('磁盘写入失败'); },
  } });
  await settle();
  line.document.querySelector('[data-line-id="second"]').click();
  const remove = line.document.getElementById('lineRemove');
  remove.click();
  await settle();
  assert.equal(line.document.querySelector('[data-line-id="second"]').getAttribute('aria-pressed'), 'true');
  assert.equal(remove.disabled, false);
  assert.match(line.document.getElementById('lineStatus').textContent, /磁盘写入失败/);
});
