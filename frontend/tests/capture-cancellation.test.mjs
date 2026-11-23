import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, page, settle } from './harness.mjs';

const region = { x: 20, y: 30, width: 200, height: 100 };
const start = (overlay, fast = false) => overlay.run(`prepareSelectionFor(${JSON.stringify(region)}, ${fast})`);

test('Esc aborts the in-flight preparation call and closes the overlay session', async () => {
  const pending = deferred();
  let signal;
  let cancellations = 0;
  const overlay = page('overlay', { bridge: {
    prepareSelection: (_, options) => { signal = options; return pending.promise; },
    cancelRegion: async () => { cancellations++; },
  } });
  const preparing = start(overlay);
  assert.equal(signal?.aborted, false);
  overlay.window.fire('keydown', { key: 'Escape' });
  assert.equal(signal.aborted, true);
  assert.equal(cancellations, 1);
  pending.resolve({ id: 'late', url: 'shot://late', ...region, rect: region });
  await preparing;
  assert.equal(overlay.document.getElementById('overlayCanvas').hidden, true);
});

test('resize watchdog aborts a stalled preview fetch and calls the rollback handler', async () => {
  const pending = deferred();
  let nativeSignal;
  let fetchSignal;
  const overlay = page('overlay', { bridge: {
    prepareSelectionFast: async (_, signal) => {
      nativeSignal = signal;
      return { id: 'new', url: 'shot://new', ...region, rect: region };
    },
  }, globals: { fetch: (_, options) => { fetchSignal = options?.signal; return pending.promise; } } });
  const failure = [];
  overlay.context.noteFailure = error => failure.push(error.message);
  const preparing = overlay.run(`prepareSelectionFor(${JSON.stringify(region)}, true, noteFailure)`);
  await settle();
  assert.ok(nativeSignal);
  assert.equal(fetchSignal, nativeSignal);
  await overlay.tick(4000);
  await preparing;
  assert.equal(fetchSignal.aborted, true);
  assert.deepEqual(failure, ['重新截取超时']);
  pending.resolve({ ok: true, blob: async () => new Blob(['late']) });
  await settle();
  assert.equal(overlay.document.getElementById('overlayCanvas').hidden, true);
});

test('replacing a pending preparation aborts only the previous request', async () => {
  const pending = deferred();
  const signals = [];
  const overlay = page('overlay', { bridge: {
    prepareSelection: (_, signal) => {
      signals.push(signal);
      if (signals.length === 1) return pending.promise;
      return Promise.resolve({ id: 'new', url: 'shot://new', ...region, rect: region });
    },
  } });
  const previous = start(overlay);
  await start(overlay);
  assert.deepEqual(signals.map(signal => signal.aborted), [true, false]);
  pending.reject(new Error('previous operation aborted'));
  await previous;
  assert.equal(overlay.document.getElementById('overlayCanvas').hidden, false);
});
