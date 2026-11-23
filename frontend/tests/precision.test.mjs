import test from 'node:test';
import assert from 'node:assert/strict';
import { deferred, page, settle } from './harness.mjs';

const initial = { x: 30, y: 35, width: 100, height: 80 };

function capture(region = initial) {
  return { id: 'initial', url: 'shot://initial', rect: region, width: region.width, height: region.height };
}

test('arrow keys fine-tune the endpoint of a region drag', async () => {
  const requests = [];
  const overlay = page('overlay', { bridge: { prepareSelection: async (region) => {
    requests.push({ ...region });
    return capture(region);
  } } });
  await settle();
  overlay.window.fire('pointerdown', { button: 0, clientX: 30, clientY: 35 });
  overlay.window.fire('pointermove', { clientX: 130, clientY: 115 });
  overlay.window.fire('keydown', { key: 'ArrowRight' });
  overlay.window.fire('keydown', { key: 'ArrowUp', shiftKey: true });
  overlay.window.fire('pointermove', { clientX: 132, clientY: 117 });
  overlay.window.fire('pointerup', { button: 0, clientX: 132, clientY: 117 });
  await settle();
  assert.deepEqual(requests, [{ x: 30, y: 35, width: 103, height: 72 }]);
});

test('keyboard adjustment batches repeats into one capture in the same overlay', async () => {
  const requests = [];
  const overlay = page('overlay', { bridge: {
    prepareSelection: async () => capture(),
    prepareSelectionFast: async (region) => {
      requests.push({ ...region });
      return capture(region);
    },
  } });
  await overlay.run(`prepareSelectionFor(${JSON.stringify(initial)})`);
  overlay.window.fire('keydown', { key: 'ArrowLeft', altKey: true });
  overlay.window.fire('keydown', { key: 'ArrowLeft', altKey: true, repeat: true });
  overlay.window.fire('keydown', { key: 'ArrowDown', altKey: true, shiftKey: true });
  assert.deepEqual(requests, []);
  assert.equal(overlay.document.getElementById('selection').style.left, '28px');
  for (const timer of [...overlay.timers.values()]) await overlay.tick(timer.delay);
  assert.deepEqual(requests, [{ x: 28, y: 35, width: 100, height: 81 }]);
});

test('color picking copies only the sample for the current pointer', async () => {
  const first = deferred();
  const sampled = [], copied = [];
  const overlay = page('overlay', { bridge: {
    sampleOverlayPixels: async (x, y) => {
      sampled.push([x, y]);
      return sampled.length === 1 ? first.promise : { pngBase64: 'cG5n', hex: '#a050ff' };
    },
    copyPixelColor: async (hex) => copied.push(hex),
  } });
  await settle();
  overlay.window.fire('pointermove', { clientX: 20, clientY: 30 });
  overlay.window.fire('keydown', { key: 'z' });
  assert.deepEqual(sampled, [[20, 30]]);
  overlay.window.fire('pointermove', { clientX: 40, clientY: 55 });
  overlay.window.fire('keydown', { key: 'c' });
  first.resolve({ pngBase64: 'cG5n', hex: '#012345' });
  await settle();
  assert.deepEqual(copied, []);
  assert.equal(overlay.document.getElementById('precisionPixels').src, 'data:image/png;base64,cG5n');
  for (const timer of [...overlay.timers.values()]) await overlay.tick(timer.delay);
  assert.deepEqual(sampled, [[20, 30], [40, 55]]);
  overlay.window.fire('keydown', { key: 'c' });
  await settle();
  assert.deepEqual(copied, ['#a050ff']);
});

test('ruler shows movement on both axes from the chosen origin', async () => {
  const overlay = page('overlay');
  await settle();
  overlay.window.fire('pointermove', { clientX: 20, clientY: 30 });
  overlay.window.fire('keydown', { key: 'm' });
  overlay.window.fire('pointermove', { clientX: 80, clientY: 110 });
  const ruler = overlay.document.getElementById('precisionRuler');
  assert.equal(ruler.hidden, false);
  assert.match(overlay.document.getElementById('precisionRulerLabel').textContent, /60 × 80.*100/);
  overlay.window.fire('keydown', { key: 'm' });
  assert.equal(ruler.hidden, true);
});
