import assert from 'node:assert/strict';
import test from 'node:test';
import { page, settle } from './harness.mjs';

const region = { x: 10, y: 10, width: 200, height: 140 };
const arrow = { kind: 'arrow', x: 15, y: 20, x2: 85, y2: 20, width: 2, color: '#e56e48' };
const rectangle = { kind: 'rect', x: 100, y: 60, x2: 175, y2: 108, width: 4, color: '#f3a24e' };
const text = { kind: 'text', x: 30, y: 40, x2: 30, y2: 40, width: 2, color: '#e56e48', text: 'Original', fontSize: 14 };

async function openOverlay(bridge = {}) {
  const overlay = page('overlay', { bridge: { prepareSelection: async () => ({
    id: 'selected', url: 'shot://selected', width: 200, height: 140, rect: region,
  }), ...bridge } });
  await overlay.run(`prepareSelectionFor(${JSON.stringify(region)})`);
  return overlay;
}

function addShape(overlay, shape) {
  overlay.run(`engine.addShape(${JSON.stringify(shape)})`);
}

function shapes(overlay) {
  return JSON.parse(overlay.run('JSON.stringify(engine.getShapes())'));
}

function chooseTool(overlay, name) {
  const tool = overlay.document.querySelector(`[data-tool="${name}"]`);
  assert.ok(tool, `missing ${name} on the annotation toolbar`);
  overlay.document.getElementById('annotationToolbar').fire('click', { target: tool });
}

function pointer(overlay, event, clientX, clientY, pointerId = 1) {
  overlay.window.fire(event, {
    target: overlay.document.getElementById('overlayCanvas'), clientX, clientY, button: 0, pointerId,
  });
}

function clickCanvas(overlay, clientX, clientY) {
  pointer(overlay, 'pointerdown', clientX, clientY);
  pointer(overlay, 'pointerup', clientX, clientY);
}

test('the selection tool moves the clicked arrow and treats a drag as one undo step', async () => {
  const overlay = await openOverlay();
  addShape(overlay, arrow);
  addShape(overlay, rectangle);
  chooseTool(overlay, 'select');
  assert.equal(overlay.document.getElementById('annotationSelection').hidden, true);

  pointer(overlay, 'pointerdown', 48, 20);
  pointer(overlay, 'pointermove', 58, 30);
  pointer(overlay, 'pointermove', 70, 44);
  pointer(overlay, 'pointerup', 70, 44);
  assert.equal(overlay.document.getElementById('annotationSelection').hidden, false);
  const moved = { ...arrow, dx: 22, dy: 24 };
  assert.deepEqual(shapes(overlay), [moved, rectangle]);

  overlay.window.fire('keydown', { key: 'z', metaKey: true });
  assert.deepEqual(shapes(overlay), [arrow, rectangle]);
  assert.equal(overlay.document.querySelector('[data-tool="redo"]').disabled, false);
  overlay.window.fire('keydown', { key: 'Z', metaKey: true, shiftKey: true });
  assert.deepEqual(shapes(overlay), [moved, rectangle]);
});

test('the palette and width slider edit the selected shape as separate undoable changes', async () => {
  const overlay = await openOverlay();
  addShape(overlay, arrow);
  addShape(overlay, rectangle);
  chooseTool(overlay, 'select');
  clickCanvas(overlay, 48, 20);

  overlay.document.querySelector('[data-color-custom]').click();
  const amber = overlay.document.querySelectorAll('button[data-palette-color]')
    .find((swatch) => swatch.dataset.paletteColor === '#f3a24e');
  assert.ok(amber);
  amber.click();
  overlay.document.querySelector('[data-palette-close]').click();
  const width = overlay.document.getElementById('overlayWidth');
  assert.equal(width.value, '2');
  width.value = '5';
  width.fire('input');
  width.value = '6';
  width.fire('input');
  width.fire('change');
  assert.deepEqual(shapes(overlay), [{ ...arrow, color: '#f3a24e', width: 6 }, rectangle]);

  chooseTool(overlay, 'undo');
  assert.deepEqual(shapes(overlay), [{ ...arrow, color: '#f3a24e' }, rectangle]);
  chooseTool(overlay, 'undo');
  assert.deepEqual(shapes(overlay), [arrow, rectangle]);
  overlay.window.fire('keydown', { key: 'Delete' });
  assert.deepEqual(shapes(overlay), [rectangle]);
  assert.equal(overlay.document.querySelector('[data-tool="redo"]').disabled, true);
  chooseTool(overlay, 'undo');
  assert.deepEqual(shapes(overlay), [arrow, rectangle]);
});

test('double-click edits selected text instead of copying and undo restores its contents', async () => {
  const actions = [];
  const overlay = await openOverlay({ finishAnnotated: async (action) => { actions.push(action); return { completed: true }; } });
  addShape(overlay, text);
  chooseTool(overlay, 'select');

  clickCanvas(overlay, 37, 35);
  clickCanvas(overlay, 37, 35);
  const field = overlay.document.getElementById('textInputOverlay');
  assert.ok(field);
  assert.equal(field.value, 'Original');
  field.value = 'Edited';
  field.fire('keydown', { key: 'Enter' });
  await settle();
  assert.deepEqual(actions, []);
  assert.deepEqual(shapes(overlay), [{ ...text, text: 'Edited' }]);

  overlay.document.getElementById('textBold').click();
  assert.equal(shapes(overlay)[0].bold, true);
  chooseTool(overlay, 'undo');
  assert.deepEqual(shapes(overlay), [{ ...text, text: 'Edited' }]);
  chooseTool(overlay, 'undo');
  assert.deepEqual(shapes(overlay), [text]);
});

test('a cancelled selection drag leaves the original object and redo history intact', async () => {
  const overlay = await openOverlay();
  addShape(overlay, arrow);
  addShape(overlay, rectangle);
  chooseTool(overlay, 'select');
  chooseTool(overlay, 'undo');
  pointer(overlay, 'pointerdown', 48, 20);
  pointer(overlay, 'pointermove', 66, 45);
  pointer(overlay, 'pointerup', 66, 45, 2);
  pointer(overlay, 'pointercancel', 66, 45);
  assert.deepEqual(shapes(overlay), [arrow]);
  chooseTool(overlay, 'redo');
  assert.deepEqual(shapes(overlay), [arrow, rectangle]);
});
