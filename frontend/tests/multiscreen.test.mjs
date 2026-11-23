import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, page, settle } from './harness.mjs';

const rightPane = {
  session: 12,
  displayX: 0, displayY: 0, width: 1000, height: 600, scale: 2,
  desktop: { x: -640, y: -200, width: 1640, height: 800 },
};

test('the overlay waits for the current frozen desktop image to decode before revealing', async () => {
  const ready = [];
  const first = deferred();
  const second = deferred();
  const overlay = page('overlay', { bridge: { overlayReady: async session => ready.push(session) } });
  const backdrop = overlay.document.getElementById('overlayBackdrop');
  assert.ok(backdrop);
  backdrop.decode = () => backdrop.src.endsWith('/0.png') && backdrop.src.includes('/12/') ? first.promise : second.promise;

  overlay.emit('onOverlayShown', { ...rightPane, backdropUrl: 'shot://localhost/d/12/0.png' });
  assert.deepEqual(ready, []);
  overlay.emit('onOverlayShown', { ...rightPane, session: 13, backdropUrl: 'shot://localhost/d/13/0.png' });
  first.resolve();
  await settle();
  assert.deepEqual(ready, []);

  second.resolve();
  await settle();
  assert.equal(backdrop.hidden, false);
  assert.equal(backdrop.src, 'shot://localhost/d/13/0.png');
  assert.deepEqual(ready, [13]);

  overlay.emit('onOverlayHidden', 12);
  assert.equal(backdrop.hidden, false);
  overlay.emit('onOverlayHidden', 13);
  assert.equal(backdrop.hidden, true);

  overlay.emit('onOverlayShown', { ...rightPane, session: 14 });
  await settle();
  assert.equal(backdrop.hidden, true);
  assert.deepEqual(ready, [13, 14]);
});

test('desktop selection retains negative panel coordinates and displays the complete capture', async () => {
  const previews = [];
  const captures = [];
  const overlay = page('overlay', { bridge: {
    previewOverlaySelection: async (session, region) => previews.push({ session, region }),
    prepareSelection: async (region) => {
      captures.push({ ...region });
      return { id: 'desktop', url: 'shot://desktop', width: region.width * 2, height: region.height * 2, rect: region };
    },
  } });
  overlay.emit('onOverlayShown', rightPane);
  const pointer = { pointerId: 1, button: 0 };
  overlay.window.fire('pointerdown', { ...pointer, clientX: 200, clientY: 200 });
  overlay.window.fire('pointermove', { ...pointer, clientX: -140, clientY: -50 });
  await settle();
  overlay.window.fire('pointerup', { ...pointer, clientX: -140, clientY: -50 });
  await settle();

  assert.deepEqual(captures, [{ x: -140, y: -50, width: 340, height: 250 }]);
  assert.equal(previews.at(-1).session, 12);
  assert.equal(previews.at(-1).region.x, -140);
  const selection = overlay.document.getElementById('selection');
  assert.ok(parseInt(selection.style.left, 10) >= 0);
  assert.ok(parseInt(selection.style.top, 10) >= 0);
  assert.equal(overlay.document.getElementById('overlayCanvas').width, 680);
});

test('another pane displays the crossing selection without brightening extra pixels or accepting stale messages', () => {
  const overlay = page('overlay');
  overlay.emit('onOverlayShown', rightPane);
  overlay.emit('onOverlayPreview', { session: 12, rect: { x: -80, y: 90, width: 200, height: 130 } });
  const maskRight = overlay.document.getElementById('maskRight');
  assert.equal(maskRight.style.left, '120px');
  assert.equal(overlay.document.getElementById('selection').hidden, false);
  overlay.emit('onOverlayPreview', { session: 11 });
  assert.equal(overlay.document.getElementById('selection').hidden, false);
  overlay.emit('onOverlayPreview', { session: 12 });
  assert.equal(overlay.document.getElementById('selection').hidden, true);
});

test('entering an idle display pane requests keyboard focus without changing the selection', async () => {
  const focus = [];
  const overlay = page('overlay', { bridge: { previewOverlaySelection: async (...args) => focus.push(args) } });
  overlay.emit('onOverlayShown', rightPane);
  overlay.document.documentElement.fire('pointerenter', { clientX: 100, clientY: 100, buttons: 1 });
  await settle();
  assert.deepEqual(focus, []);
  overlay.document.documentElement.fire('pointerenter', { clientX: 100, clientY: 100 });
  await settle();
  assert.deepEqual(focus, [[12, null]]);

  overlay.emit('onOverlayPreview', { session: 12, rect: { x: -80, y: 90, width: 200, height: 130 } });
  overlay.document.documentElement.fire('pointerenter', { clientX: 100, clientY: 100 });
  await settle();
  assert.deepEqual(focus, [[12, null]]);
});
