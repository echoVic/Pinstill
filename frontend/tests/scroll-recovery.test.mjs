import test from 'node:test';
import assert from 'node:assert/strict';
import { page, settle } from './harness.mjs';

test('an unaligned frame gives recovery guidance and allows keeping the stitched part', async () => {
  const finished = [];
  const control = page('scroll', { bridge: { finishScroll: async (...args) => finished.push(args) } });
  control.emit('onScrollInit', { sessionId: 'scroll-1', auto: true });
  const progress = { sessionId: 'scroll-1', frames: 4, height: 720 };
  control.emit('onScrollProgress', { ...progress, status: 'unaligned', auto: false });
  assert.match(control.document.getElementById('scrollStatus').textContent, /往回滚/);
  assert.match(control.document.getElementById('scrollProgress').textContent, /4 帧 · 720 px/);
  assert.equal(control.document.querySelector('[data-scroll-action="mode"]').getAttribute('aria-pressed'), 'false');
  assert.equal(control.document.querySelector('[data-scroll-action="finish"]').disabled, false);

  control.emit('onScrollProgress', { ...progress, frames: 5, height: 900, status: 'running', auto: false });
  assert.match(control.document.getElementById('scrollProgress').textContent, /5 帧 · 900 px/);
  control.run('finish(false)');
  await settle();
  assert.deepEqual(finished, [['scroll-1', 'finish']]);
});
