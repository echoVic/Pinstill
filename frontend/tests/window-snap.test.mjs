import test from 'node:test';
import assert from 'node:assert/strict';
import { deferred, page, settle } from './harness.mjs';

const frontWindow = { x: 100, y: 100, width: 400, height: 300, owner: 'Safari', title: 'Docs' };
const lowerWindow = { x: 300, y: 200, width: 500, height: 400, owner: 'Code', title: 'main.go' };
const windows = [frontWindow, lowerWindow];

function overlayWithWindows(extraBridge = {}) {
  return page('overlay', {
    bridge: {
      prepareSelection: async (rect) => ({ id: 'shot-1', url: 'shot://shot-1', width: rect.width, height: rect.height, rect }),
      cancelRegion: async () => {},
      ...extraBridge,
    },
  });
}

test('hover hit-testing follows front-to-back z-order and clears on bare desktop', () => {
  const p = overlayWithWindows();
  p.emit('onOverlayShown', { width: 1200, height: 800, windows });

  const sel = p.document.getElementById('selection');
  const dim = p.document.getElementById('dim');
  const badge = p.document.getElementById('sizeBadge');

  // Overlap region (350, 250) hits frontWindow first.
  p.window.fire('pointermove', { clientX: 350, clientY: 250 });
  assert.equal(sel.hidden, false);
  assert.equal(sel.classList.contains('window-preview'), true);
  assert.equal(sel.style.left, '100px');
  assert.equal(sel.style.top, '100px');
  assert.equal(sel.style.width, '400px');
  assert.equal(sel.style.height, '300px');
  assert.equal(dim.hidden, true);
  assert.equal(badge.hidden, false);
  assert.equal(badge.textContent, 'Safari · 400 × 300');

  // Exposed region of lowerWindow (650, 450) hits lowerWindow.
  p.window.fire('pointermove', { clientX: 650, clientY: 450 });
  assert.equal(sel.style.left, '300px');
  assert.equal(sel.style.top, '200px');
  assert.equal(sel.style.width, '500px');
  assert.equal(sel.style.height, '400px');
  assert.equal(badge.textContent, 'Code · 500 × 400');

  // Bare desktop clears the preview and restores the full-screen dim mask.
  p.window.fire('pointermove', { clientX: 20, clientY: 20 });
  assert.equal(sel.hidden, true);
  assert.equal(sel.classList.contains('window-preview'), false);
  assert.equal(dim.hidden, false);
  assert.equal(badge.hidden, true);
});

test('onOverlayShown with cursor coordinates highlights the topmost window immediately', () => {
  const p = overlayWithWindows();
  p.emit('onOverlayShown', {
    width: 1200,
    height: 800,
    hasCursor: true,
    cursorX: 350,
    cursorY: 250,
    windows,
  });

  const sel = p.document.getElementById('selection');
  const badge = p.document.getElementById('sizeBadge');
  assert.equal(sel.hidden, false);
  assert.equal(sel.classList.contains('window-preview'), true);
  assert.equal(sel.style.left, '100px');
  assert.equal(sel.style.width, '400px');
  assert.equal(badge.textContent, 'Safari · 400 × 300');
});

test('single click on a highlighted window captures its exact rectangle and enters annotating', async () => {
  const captured = [];
  const p = overlayWithWindows({
    prepareSelection: async (rect) => {
      captured.push(rect);
      return { id: 'win-shot', url: 'shot://win-shot', width: rect.width, height: rect.height, rect };
    },
  });
  p.emit('onOverlayShown', { width: 1200, height: 800, windows });

  p.window.fire('pointermove', { clientX: 600, clientY: 300 });
  p.window.fire('pointerdown', { pointerId: 1, button: 0, clientX: 600, clientY: 300 });
  // Small pointer jitter (< MIN_SELECTION) still counts as a click on the window.
  p.window.fire('pointermove', { pointerId: 1, clientX: 601, clientY: 301 });
  p.window.fire('pointerup', { pointerId: 1, button: 0, clientX: 601, clientY: 301 });
  await settle();

  assert.equal(captured.length, 1);
  assert.deepEqual({ ...captured[0] }, { x: 300, y: 200, width: 500, height: 400 });
  assert.equal(p.document.getElementById('annotationToolbar').hidden, true);
  await p.tick(350);
  assert.equal(p.run('phase'), 'annotating');
  assert.equal(p.document.getElementById('selection').classList.contains('window-preview'), false);
});

test('pointerdown uses its own coordinates before the first pointermove', async () => {
  const captured = [];
  const p = overlayWithWindows({
    prepareSelection: async (rect) => {
      captured.push({ ...rect });
      return { id: 'win-shot', url: 'shot://win-shot', width: rect.width, height: rect.height, rect };
    },
  });
  p.emit('onOverlayShown', { width: 1200, height: 800, hasCursor: true, cursorX: 150, cursorY: 150, windows });

  p.window.fire('pointerdown', { pointerId: 1, button: 0, clientX: 650, clientY: 450 });
  p.window.fire('pointerup', { pointerId: 1, button: 0, clientX: 650, clientY: 450 });
  await settle();

  assert.deepEqual(captured, [{ x: 300, y: 200, width: 500, height: 400 }]);
});

test('dragging across a highlighted window switches to manual region capture', async () => {
  const captured = [];
  const p = overlayWithWindows({
    prepareSelection: async (rect) => {
      captured.push(rect);
      return { id: 'drag-shot', url: 'shot://drag-shot', width: rect.width, height: rect.height, rect };
    },
  });
  p.emit('onOverlayShown', { width: 1200, height: 800, windows });

  p.window.fire('pointermove', { clientX: 150, clientY: 150 });
  p.window.fire('pointerdown', { pointerId: 1, button: 0, clientX: 150, clientY: 150 });
  p.window.fire('pointermove', { pointerId: 1, clientX: 290, clientY: 270 });
  assert.equal(p.document.getElementById('selection').classList.contains('window-preview'), false);
  p.window.fire('pointerup', { pointerId: 1, button: 0, clientX: 290, clientY: 270 });
  await settle();

  assert.equal(captured.length, 1);
  assert.deepEqual({ ...captured[0] }, { x: 150, y: 150, width: 140, height: 120 });
  assert.equal(p.run('phase'), 'annotating');
});

test('clicking empty desktop resets to idle while retaining detected windows for hover', () => {
  const p = overlayWithWindows();
  p.emit('onOverlayShown', { width: 1200, height: 800, windows });

  // Click on bare desktop.
  p.window.fire('pointermove', { clientX: 20, clientY: 20 });
  p.window.fire('pointerdown', { pointerId: 1, button: 0, clientX: 20, clientY: 20 });
  p.window.fire('pointerup', { pointerId: 1, button: 0, clientX: 20, clientY: 20 });

  assert.equal(p.run('phase'), 'idle');
  assert.equal(p.run('detectedWindows.length'), 2);

  // Subsequent hover over frontWindow still snaps.
  p.window.fire('pointermove', { clientX: 200, clientY: 200 });
  assert.equal(p.document.getElementById('selection').classList.contains('window-preview'), true);
  assert.equal(p.document.getElementById('sizeBadge').textContent, 'Safari · 400 × 300');
});

test('cancelRegionFlow clears detectedWindows', () => {
  const p = overlayWithWindows();
  p.emit('onOverlayShown', { width: 1200, height: 800, windows });
  assert.equal(p.run('detectedWindows.length'), 2);

  p.run('cancelRegionFlow()');
  assert.equal(p.run('detectedWindows.length'), 0);
});

test('onOverlayShown clears previous canvas and notifies overlayReady after paint commit', async () => {
  const ready = [];
  const frames = [];
  const p = page('overlay', {
    bridge: {
      prepareSelection: async (rect) => ({ id: 'shot-1', url: 'shot://shot-1', width: rect.width, height: rect.height, rect }),
      overlayReady: async (session) => { ready.push(session); },
    },
    globals: {
      requestAnimationFrame: (cb) => { frames.push(cb); return frames.length; },
    },
  });

  await p.run('prepareSelectionFor({ x: 10, y: 10, width: 120, height: 80 })');
  const canvas = p.document.getElementById('overlayCanvas');
  assert.equal(canvas.width, 120);
  assert.equal(canvas.height, 80);

  p.emit('onOverlayShown', { session: 5, width: 1200, height: 800, hasCursor: true, cursorX: 350, cursorY: 250, windows });
  assert.equal(canvas.width, 0);
  assert.equal(canvas.height, 0);
  assert.equal(canvas.hidden, true);
  assert.equal(ready.length, 0, 'overlayReady must wait for frame commit before revealing');

  // Flush the two nested rAF callbacks.
  while (frames.length > 0) frames.shift()();
  await settle();
  assert.deepEqual(ready, [5]);
});

test('stale onOverlayShown session does not invoke overlayReady after a newer session starts', async () => {
  const ready = [];
  const frames = [];
  const p = page('overlay', {
    bridge: { overlayReady: async (session) => { ready.push(session); } },
    globals: { requestAnimationFrame: (cb) => { frames.push(cb); return frames.length; } },
  });

  p.emit('onOverlayShown', { session: 1, width: 1200, height: 800, windows });
  // Run first rAF of session 1, then supersede with session 2 before second rAF commits.
  frames.shift()();
  p.emit('onOverlayShown', { session: 2, width: 1200, height: 800, windows });
  while (frames.length > 0) frames.shift()();
  await settle();

  assert.deepEqual(ready, [2]);
});

test('startScrollFromToolbar clears the overlay surface once scroll starts', async () => {
  const p = page('overlay', {
    bridge: {
      prepareSelection: async (rect) => ({ id: 'shot-1', url: 'shot://shot-1', width: rect.width, height: rect.height, rect }),
      startScroll: async () => {},
    },
  });
  p.emit('onOverlayShown', { width: 1200, height: 800, windows });
  await p.run('prepareSelectionFor({ x: 10, y: 10, width: 120, height: 80 })');
  const canvas = p.document.getElementById('overlayCanvas');
  assert.equal(canvas.width, 120);

  await p.run('startScrollFromToolbar()');
  assert.equal(p.run('phase'), 'idle');
  assert.equal(p.run('detectedWindows.length'), 0);
  assert.equal(canvas.width, 0);
  assert.equal(canvas.height, 0);
  assert.equal(canvas.hidden, true);
});
test('a second click during window capture copies without ever showing the toolbar', async () => {
  const pending = deferred();
  const captures = [];
  const finishes = [];
  const overlay = page('overlay', { bridge: {
    prepareSelection: async rect => { captures.push({ ...rect }); return pending.promise; },
    finishAnnotated: async (action, data) => { finishes.push({ action, data }); return { completed: true }; },
  }, globals: { FileReader: class {
    readAsDataURL() { this.result = 'data:image/png;base64,YQ=='; this.onload(); }
  } } });
  const rect = { x: 40, y: 60, width: 300, height: 220 };
  overlay.emit('onOverlayShown', { session: 15, width: 1200, height: 800, windows: [rect] });
  const toolbar = overlay.document.getElementById('annotationToolbar');
  const pointer = { pointerId: 1, button: 0, clientX: 75, clientY: 100 };
  overlay.window.fire('pointerdown', pointer);
  overlay.window.fire('pointerup', pointer);
  overlay.window.fire('pointerdown', pointer);
  overlay.window.fire('pointerup', pointer);
  pending.resolve({ id: 'popup', url: 'shot://popup', width: 300, height: 220, rect });
  await settle();

  assert.deepEqual(captures, [rect]);
  assert.deepEqual(finishes, [{ action: 'copy', data: 'YQ==' }]);
  assert.equal(toolbar.hidden, true);
  assert.equal(overlay.document.getElementById('overlayCanvas').width, 0);
});
