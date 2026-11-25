import assert from 'node:assert/strict';
import test from 'node:test';
import { page, settle } from './harness.mjs';

const original = { x: 30, y: 35, width: 120, height: 90 };
const annotation = { kind: 'rect', x: 10, y: 12, x2: 30, y2: 35, color: '#123456', width: 2 };
const preferencesKey = 'pinstill.preferences';

function payload(rect = original) {
  return { id: 'selected', url: 'shot://selected', rect, width: rect.width, height: rect.height };
}

async function openOverlay(bridge = {}, storage = {}, region = original, globals = {}) {
  const requests = [];
  const overlay = page('overlay', { storage, globals, bridge: {
    prepareSelection: async () => payload(region),
    prepareSelectionFast: async (rect) => { requests.push({ ...rect }); return payload(rect); },
    ...bridge,
  } });
  await overlay.run(`prepareSelectionFor(${JSON.stringify(region)})`);
  return { overlay, requests, canvas: overlay.document.getElementById('overlayCanvas') };
}

function pointer(overlay, type, clientX, clientY, options = {}) {
  overlay.window.fire(type, { target: overlay.document.getElementById('overlayCanvas'), pointerId: 1, button: 0, clientX, clientY, ...options });
}

function chooseTool(overlay, tool) {
  overlay.document.getElementById('annotationToolbar').fire('click', { target: overlay.document.querySelector(`[data-tool="${tool}"]`) });
}

function selectionRect(overlay) {
  return JSON.parse(overlay.run('JSON.stringify(committedRect)'));
}

test('a fresh or reused capture starts with no tool selected', async () => {
  const { overlay, canvas } = await openOverlay();
  const verify = () => {
    assert.equal(overlay.run('engine.getTool()'), 'none');
    assert.equal(overlay.document.querySelectorAll('#overlayTools .active').length, 0);
    assert.equal(canvas.style.cursor, 'move');
  };
  verify();
  chooseTool(overlay, 'arrow');
  overlay.emit('onOverlayShown', { width: 1200, height: 800 });
  await overlay.run(`prepareSelectionFor(${JSON.stringify(original)})`);
  verify();
});

test('upgrading replaces the old built-in defaults once without changing other preferences', async () => {
  const storage = { [preferencesKey]: JSON.stringify({ defaultTool: 'arrow', defaultColor: '#e56e48', tooltip: false }) };
  const { overlay } = await openOverlay({}, storage);
  const { loadPreferences, savePreferences } = overlay.module('./settings');
  const preferences = loadPreferences();
  assert.equal(preferences.defaultTool, 'none');
  assert.equal(preferences.defaultColor, '#ff0000');
  assert.equal(preferences.tooltip, false);
  assert.equal(JSON.parse(storage[preferencesKey]).defaultColor, '#ff0000');

  savePreferences({ ...preferences, defaultTool: 'arrow', defaultColor: '#e56e48' });
  assert.equal(loadPreferences().defaultTool, 'arrow');
  assert.equal(loadPreferences().defaultColor, '#e56e48');
});

test('a new shape and text annotation both use the red default', async () => {
  const { overlay } = await openOverlay();
  const redSwatch = overlay.document.querySelectorAll('[data-color]').find(({ dataset }) => dataset.color === '#ff0000');
  assert.equal(redSwatch?.getAttribute('aria-pressed'), 'true');
  chooseTool(overlay, 'rect');
  pointer(overlay, 'pointerdown', 45, 45);
  pointer(overlay, 'pointermove', 90, 65);
  pointer(overlay, 'pointerup', 90, 65);
  assert.equal(overlay.run('engine.getShapes()[0]?.color'), '#ff0000');
  chooseTool(overlay, 'text');
  pointer(overlay, 'pointerdown', 75, 55);
  const editor = overlay.document.getElementById('textInputOverlay');
  assert.ok(editor);
  editor.value = 'Red text';
  editor.fire('keydown', { key: 'Enter' });
  assert.equal(overlay.run('engine.getShapes()[1]?.color'), '#ff0000');
});

test('dragging inside an unmarked selection moves it without resizing and re-captures once', async () => {
  const { overlay, requests, canvas } = await openOverlay();
  const selection = overlay.document.getElementById('selection');
  const mask = overlay.document.getElementById('maskTop');
  pointer(overlay, 'pointerdown', 75, 65);
  pointer(overlay, 'pointermove', 76, 65);
  assert.equal(canvas.hidden, false);
  assert.equal(selection.style.left, '30px');
  pointer(overlay, 'pointermove', 110, 85, { pointerId: 2 });
  assert.equal(selection.style.left, '30px');
  pointer(overlay, 'pointermove', 110, 85);
  assert.equal(selection.style.left, '65px');
  assert.equal(selection.style.top, '55px');
  assert.equal(selection.style.width, '120px');
  assert.equal(selection.style.height, '90px');
  assert.equal(canvas.hidden, true);
  assert.equal(mask.hidden, false);
  assert.deepEqual(requests, []);
  pointer(overlay, 'pointerup', 110, 85);
  await settle();
  assert.deepEqual(requests, [{ ...original, x: 65, y: 55 }]);
  assert.deepEqual(selectionRect(overlay), requests[0]);
  assert.equal(canvas.hidden, false);
  assert.equal(overlay.run('engine.getShapes().length'), 0);
});

test('a selected drawing tool draws normally and Option-drag moves within the display bounds', async () => {
  const { overlay, requests } = await openOverlay();
  chooseTool(overlay, 'rect');
  pointer(overlay, 'pointerdown', 50, 55);
  pointer(overlay, 'pointermove', 85, 75);
  pointer(overlay, 'pointerup', 85, 75);
  assert.equal(overlay.run('engine.getShapes().length'), 1);
  assert.deepEqual(requests, []);

  pointer(overlay, 'pointerdown', 50, 55, { altKey: true });
  pointer(overlay, 'pointermove', 2000, -80, { altKey: true });
  pointer(overlay, 'pointerup', 2000, -80, { altKey: true });
  await settle();
  assert.deepEqual(requests, [{ ...original, x: 1080, y: 0 }]);
  assert.equal(overlay.run('engine.getTool()'), 'rect');
});

test('cancelling a move restores the original selection and retains its annotations', async () => {
  const { overlay, requests, canvas } = await openOverlay();
  overlay.run(`engine.addShape(${JSON.stringify(annotation)})`);
  pointer(overlay, 'pointerdown', 60, 55);
  pointer(overlay, 'pointermove', 110, 85);
  assert.equal(canvas.hidden, true);
  pointer(overlay, 'pointercancel', 110, 85);
  assert.equal(canvas.hidden, false);
  assert.equal(overlay.document.getElementById('selection').style.left, '30px');
  assert.deepEqual(selectionRect(overlay), original);
  assert.equal(overlay.run('engine.canUndo()'), true);
  assert.deepEqual(requests, []);
});

test('a click followed by a nearby drag does not copy the selection', async () => {
  const actions = [];
  const { overlay, requests } = await openOverlay({ finishAnnotated: async (action) => { actions.push(action); return { completed: true }; } });
  pointer(overlay, 'pointerdown', 60, 55);
  pointer(overlay, 'pointerup', 60, 55);
  pointer(overlay, 'pointerdown', 61, 55);
  pointer(overlay, 'pointermove', 112, 70);
  overlay.window.fire('keydown', { key: 'Enter' });
  pointer(overlay, 'pointerup', 112, 70);
  await settle();
  assert.deepEqual(actions, []);
  assert.deepEqual(requests, [{ ...original, x: 81, y: 50 }]);
});

test('two clicks without a drag still copy an unmarked selection', async () => {
  const actions = [];
  const { overlay, requests } = await openOverlay({ finishAnnotated: async (action) => { actions.push(action); return { completed: true }; } });
  for (const index of [0, 1]) {
    pointer(overlay, 'pointerdown', 60 + index, 55);
    pointer(overlay, 'pointermove', 61 + index, 55);
    pointer(overlay, 'pointerup', 61 + index, 55);
  }
  await settle();
  assert.deepEqual(actions, ['copy']);
  assert.deepEqual(requests, []);
});

test('a click does not move the size badge when the toolbar sits above the selection', async () => {
  const { overlay } = await openOverlay({}, {}, { ...original, y: 700 });
  const badge = overlay.document.getElementById('sizeBadge');
  const top = badge.style.top;
  pointer(overlay, 'pointerdown', 70, 740);
  pointer(overlay, 'pointerup', 70, 740);
  assert.equal(badge.style.top, top);
});

test('moving after a failed automatic copy leaves the new area open for markup', async () => {
  const actions = [];
  const { overlay, requests } = await openOverlay({
    settings: async () => ({ afterCapture: 'clipboard' }),
    finishAnnotated: async (action) => { actions.push(action); throw new Error('clipboard unavailable'); },
  }, {}, original, { FileReader: class {
    readAsDataURL() {
      this.result = 'data:image/png;base64,cGl4ZWxz';
      this.onload();
    }
  } });
  assert.deepEqual(actions, ['copy']);
  await overlay.run(`recaptureSelection({ ...committedRect, x: 80, y: 65 }, committedRect)`);
  assert.equal(requests.length, 1);
  assert.deepEqual(actions, ['copy']);
  assert.equal(overlay.run('phase'), 'annotating');
});

test('a failed capture after moving restores the original frame and toolbar', async () => {
  const { overlay, canvas } = await openOverlay({ prepareSelectionFast: async () => { throw new Error('capture failed'); } });
  overlay.run(`engine.addShape(${JSON.stringify(annotation)})`);
  const previousEngine = overlay.run('engine');
  pointer(overlay, 'pointerdown', 60, 55);
  pointer(overlay, 'pointermove', 110, 85);
  pointer(overlay, 'pointerup', 110, 85);
  await settle();
  assert.equal(overlay.run('phase'), 'annotating');
  assert.equal(canvas.hidden, false);
  assert.equal(overlay.run('engine'), previousEngine);
  assert.deepEqual(selectionRect(overlay), original);
  assert.match(overlay.document.getElementById('demoToast').textContent, /重新截取失败.*已恢复原选区/);
});
