import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, page, settle } from './harness.mjs';

const capture = { id: 'quick-one', thumbnail: 'data:image/png;base64,cG5n', width: 640, height: 480, completion: 'copy' };

async function quickPage(overrides = {}) {
  const viewport = page('quick', { bridge: {
    quickCapture: async () => capture,
    showQuickCapture: async (id) => assert.equal(id, capture.id),
    quickAction: async () => ({ completed: true }),
    dismissQuickCapture: async () => {},
    ...overrides,
  } });
  await settle();
  return viewport;
}

test('quick actions forward the exact capture and dismiss it after success', async () => {
  const calls = [];
  const viewport = await quickPage({
    quickAction: async (...args) => { calls.push(args); return { completed: true }; },
    dismissQuickCapture: async (id) => calls.push(['dismiss', id]),
  });
  assert.equal(viewport.document.getElementById('quickThumbnail').src, capture.thumbnail);
  viewport.document.querySelector('[data-quick-action="hang"]').click();
  await settle();
  assert.deepEqual(calls, [[capture.id, 'hang'], ['dismiss', capture.id]]);
});

test('quick access waits while hovered and while a native action is pending', async () => {
  const pendingSave = deferred();
  const dismissed = [];
  const viewport = await quickPage({
    quickAction: () => pendingSave.promise,
    dismissQuickCapture: async (id) => dismissed.push(id),
  });
  const card = viewport.document.getElementById('quickCard');
  const lifetime = [...viewport.timers.values()][0]?.delay;
  assert.ok(lifetime > 0);
  card.fire('pointerenter');
  await viewport.tick(lifetime);
  assert.deepEqual(dismissed, []);

  const save = viewport.document.querySelector('[data-quick-action="save"]');
  save.click();
  card.fire('pointerleave');
  await viewport.tick(lifetime);
  assert.deepEqual(dismissed, []);
  assert.equal(save.disabled, true);

  pendingSave.resolve({ completed: false, cancelled: true });
  await settle();
  assert.equal(save.disabled, false);
  assert.deepEqual(dismissed, []);
  await viewport.tick(lifetime);
  assert.deepEqual(dismissed, [capture.id]);
});

test('quick access exposes a recoverable action error and allows retry', async () => {
  let attempts = 0;
  const dismissed = [];
  const viewport = await quickPage({
    quickAction: async () => {
      attempts++;
      if (attempts === 1) throw new Error('图片暂时不可用');
      return { completed: true };
    },
    dismissQuickCapture: async (id) => dismissed.push(id),
  });
  const pin = viewport.document.querySelector('[data-quick-action="pin"]');
  pin.click();
  await settle();
  assert.match(viewport.document.getElementById('quickStatus').textContent, /图片暂时不可用/);
  assert.equal(pin.disabled, false);
  assert.deepEqual(dismissed, []);
  pin.click();
  await settle();
  assert.deepEqual(dismissed, [capture.id]);
});
