import test from 'node:test';
import assert from 'node:assert/strict';
import { page, deferred, settle } from './harness.mjs';

const rect = { x: 10, y: 10, width: 120, height: 80 };
const shot = id => ({ id, url: `shot://${id}`, width: 120, height: 80, rect });
const history = id => ({ ...shot(id), createdAt: '2026-11-13T00:00:00Z', size: 1024 });
const select = p => p.run(`prepareSelectionFor(${JSON.stringify(rect)})`);

// Deferred promises deliberately force the native boundary to complete out of
// order. These are behavior regressions, not assertions about source spelling.
test('settings serialize native mutations and coalesce pending edits', async () => {
  const calls = [];
  const p = page('index', { bridge: { applySettings: s => { const d = deferred(); calls.push({ s, ...d }); return d.promise; } } });
  p.run('preferences.sound = false; scheduleApplySettings()');
  await p.tick(160);
  p.run('preferences.cursor = true; scheduleApplySettings()');
  await p.tick(160);
  assert.equal(calls.length, 1, 'only one native mutation may be in flight');
  calls[0].resolve({}); await settle();
  assert.equal(calls.length, 2);
  assert.equal(calls[1].s.sound, false);
  assert.equal(calls[1].s.includeCursor, true);
  calls[1].resolve({}); await settle();
});

test('frontend-only edits do not reapply an unchanged successful native snapshot', async () => {
  let calls = 0;
  const p = page('index', { bridge: { applySettings: async () => { calls++; return {}; } } });
  await settle();
  p.run('preferences.defaultWidth = 4; scheduleApplySettings()');
  await p.tick(160);
  assert.equal(calls, 1);
});

test('pending edits invalidate obsolete settings feedback and failed settings can retry', async () => {
  const calls = [];
  const p = page('index', { bridge: { applySettings: () => { const d = deferred(); calls.push(d); return d.promise; } } });
  p.run('preferences.sound = false; scheduleApplySettings()');
  calls[0].resolve({ captureKey: 'obsolete conflict' }); await settle();
  assert.doesNotMatch(p.document.getElementById('settingsStatus').textContent, /obsolete/);
  await p.tick(160);
  calls[1].reject(new Error('temporary failure')); await settle();
  p.run('scheduleApplySettings()'); await p.tick(160);
  assert.equal(calls.length, 3, 'a failed snapshot must not be deduplicated');
  calls[2].resolve({}); await settle();
});

test('history refresh preserves the previewed capture identity after insertion', async () => {
  let entries = [history('a'), history('b')];
  const p = page('index', { bridge: { historyList: async () => entries } });
  await settle(); p.run('openHistoryPreview("b", null)');
  entries = [history('new'), ...entries];
  await p.run('refreshHistory()');
  assert.equal(p.document.getElementById('historyPreviewImage').src, 'shot://b');
});

test('late history responses cannot replace a newer list', async () => {
  const calls = [];
  const p = page('index', { bridge: { historyList: () => { const d = deferred(); calls.push(d); return d.promise; } } });
  const latest = p.run('refreshHistory()');
  calls.at(-1).resolve([history('new')]); await latest;
  calls[0].resolve([history('old')]); await settle();
  assert.equal(p.run('historyEntries[0].id'), 'new');
});

test('a transient history error retains the visible list', async () => {
  let fail = false;
  const p = page('index', { bridge: { historyList: async () => { if (fail) throw new Error('temporary'); return [history('a')]; } } });
  await settle(); fail = true; await p.run('refreshHistory()');
  assert.equal(p.run('historyEntries.length'), 1);
});

test('a closed brush stroke survives pointer release and remains undoable', () => {
  const p = page('index');
  const { AnnotationEngine } = p.module('./annotate');
  const canvas = new p.Element('canvas');
  const engine = new AnnotationEngine({ canvas, getStyle: () => ({ color: '#ff0000', width: 2, entry: '' }), getWatermarkOpacity: () => 30 });
  engine.setTool('brush');
  engine.beginDraw({ x: 10, y: 10 });
  engine.moveDraw({ x: 60, y: 10 });
  engine.moveDraw({ x: 60, y: 60 });
  engine.moveDraw({ x: 10, y: 10 });
  engine.endDraw();
  assert.equal(engine.getShapes().length, 1);
  assert.equal(engine.undo(), true);
  engine.beginDraw({ x: 10, y: 10 }); engine.endDraw();
  assert.equal(engine.canUndo(), false, 'a click is still discarded');
});

for (const fails of [false, true]) test(`image fallback releases its blob URL after ${fails ? 'failure' : 'success'}`, async () => {
  const revoked = [];
  const p = page('index', { globals: {
    createImageBitmap: undefined,
    URL: { createObjectURL: () => 'blob:test', revokeObjectURL: url => revoked.push(url) },
    Image: class { async decode() { if (fails) throw new Error('decode failed'); } },
  } });
  const promise = p.module('./util').decodeImageBlob(new Blob(['test']));
  if (fails) await assert.rejects(promise, /decode failed/); else await promise;
  assert.deepEqual(revoked, ['blob:test']);
});

test('late capture event and old request result cannot annotate a new attempt', async () => {
  const calls = [];
  const p = page('overlay', { bridge: { prepareSelection: () => { const d = deferred(); calls.push(d); return d.promise; } } });
  void select(p); p.run('reset()'); void select(p);
  p.emit('onOverlayCapture', shot('old'));
  calls[0].resolve(shot('old')); await settle();
  assert.equal(p.document.getElementById('overlayCanvas').hidden, true);
  calls[1].resolve(shot('new')); await settle();
  assert.equal(p.document.getElementById('overlayCanvas').hidden, false);
  assert.equal(p.run('phase'), 'annotating');
});

test('a decode completing after reset cannot resurrect annotation and is disposed', async () => {
  const image = deferred(); let closed = 0;
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a') }, globals: { createImageBitmap: () => image.promise } });
  void select(p); await settle(); p.run('reset()');
  image.resolve({ close() { closed++; } }); await settle();
  assert.equal(p.run('phase'), 'idle');
  assert.equal(p.document.getElementById('overlayCanvas').hidden, true);
  assert.equal(closed, 1);
});

test('an old preparation rejection cannot reset the current attempt', async () => {
  const calls = [];
  const p = page('overlay', { bridge: { prepareSelection: () => { const d = deferred(); calls.push(d); return d.promise; } } });
  void select(p); p.run('reset()'); void select(p);
  calls[0].reject(new Error('old failure')); await settle();
  assert.equal(p.run('phase'), 'preparing');
  calls[1].resolve(shot('new')); await settle();
  assert.equal(p.run('phase'), 'annotating');
});

test('overlay shown invalidates pending decoding from the previous session', async () => {
  const image = deferred();
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a') }, globals: { createImageBitmap: () => image.promise } });
  void select(p); await settle(); p.emit('onOverlayShown', { width: 1200, height: 800 });
  image.resolve({ close() {} }); await settle();
  assert.equal(p.run('phase'), 'idle');
});

test('overlay tolerates null preferences and uses the shared default tool', () => {
  const p = page('overlay', { storage: { 'pinstill.preferences': 'null' } });
  assert.equal(p.run('currentTool'), p.module('./settings').DEFAULT_PREFERENCES.defaultTool);
});

test('a reused overlay reloads preferences and honors disabled Enter-to-copy', async () => {
  let finishes = 0;
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a'), finishAnnotated: async () => { finishes++; } } });
  p.storage['pinstill.preferences'] = JSON.stringify({ defaultTool: 'brush', defaultWidth: 4, defaultColor: '#123456', enterCopy: false, tooltip: false });
  p.emit('onOverlayShown', { width: 1200, height: 800 });
  void select(p); await settle();
  assert.equal(p.run('engine.getTool()'), 'brush');
  assert.equal(p.document.getElementById('overlayWidth').value, '4');
  assert.equal(p.run('color'), '#123456');
  p.window.fire('keydown', { key: 'Enter' }); await settle();
  assert.equal(finishes, 0);
});

test('the scrolling tool opens a session without requesting auto-scroll, even with a legacy preference', async () => {
  const requests = [];
  const overlay = page('overlay', {
    storage: { 'pinstill.preferences': '{"scrollAuto":true}' },
    bridge: {
      prepareSelection: async () => shot('selected'),
      startScroll: async (...args) => requests.push(JSON.parse(JSON.stringify(args))),
    },
  });
  await select(overlay);
  assert.deepEqual(requests, []);
  await overlay.run('startScrollFromToolbar()');
  assert.deepEqual(requests, [[rect]]);
});

test('scroll handoff blocks competing keyboard finish actions until it fails', async () => {
  const scroll = deferred(); let finishes = 0;
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a'), startScroll: () => scroll.promise, finishAnnotated: async () => { finishes++; } } });
  void select(p); await settle(); void p.run('startScrollFromToolbar()');
  p.window.fire('keydown', { key: 'Enter' }); await settle();
  assert.equal(finishes, 0);
  scroll.reject(new Error('denied')); await settle();
  assert.equal(p.run('phase'), 'annotating');
});

test('Enter-to-copy preference disables the keyboard action', async () => {
  let finishes = 0;
  const p = page('overlay', { storage: { 'pinstill.preferences': '{"enterCopy":false}' }, bridge: { prepareSelection: async () => shot('a'), finishAnnotated: async () => { finishes++; } } });
  void select(p); await settle(); p.window.fire('keydown', { key: 'Enter' }); await settle();
  assert.equal(finishes, 0);
});

test('late failure events cannot interrupt an unrelated annotation', async () => {
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a') } });
  void select(p); await settle();
  p.emit('onCaptureFailed', { message: 'old failure', permission: true });
  assert.equal(p.run('phase'), 'annotating');
  assert.equal(p.document.getElementById('captureError').hidden, true);
});

test('request failure retains permission guidance and retry accepts a new selection', async () => {
  let calls = 0;
  const p = page('overlay', { bridge: {
    prepareSelection: async () => { if (++calls === 1) throw new Error('截图内容为空，可能缺少屏幕录制权限'); return shot('retry'); },
    beginRegion: async () => {},
  } });
  void select(p); await settle();
  assert.equal(p.document.getElementById('captureError').hidden, false);
  assert.equal(p.document.getElementById('captureErrorSettings').hidden, false);
  p.document.getElementById('captureErrorRetry').click(); await settle();
  assert.equal(p.run('phase'), 'idle');
  void select(p); await settle(); assert.equal(p.run('phase'), 'annotating');
});

test('Esc cancels pending preparation and its late result stays hidden', async () => {
  const capture = deferred(); let cancels = 0;
  const p = page('overlay', { bridge: { prepareSelection: () => capture.promise, cancelRegion: async () => { cancels++; } } });
  void select(p); p.window.fire('keydown', { key: 'Escape' }); await settle();
  assert.equal(cancels, 1);
  capture.resolve(shot('old')); await settle();
  assert.equal(p.document.getElementById('overlayCanvas').hidden, true);
});

test('old native settings responses cannot overwrite a new session choice', async () => {
  const calls = []; let finishes = 0;
  const p = page('overlay', { bridge: {
    settings: () => { const d = deferred(); calls.push(d); return d.promise; },
    prepareSelection: async () => shot('a'), finishAnnotated: async () => { finishes++; },
  } });
  p.emit('onOverlayShown', { width: 1200, height: 800 });
  calls[1].resolve({ afterCapture: 'inplace' }); await settle();
  calls[0].resolve({ afterCapture: 'pin' }); await settle();
  assert.equal(p.run('afterCapture'), 'inplace');
  void select(p); await settle(); assert.equal(finishes, 0);
});

test('an old finish rejection cannot change a newer session', async () => {
  const finish = deferred();
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a'), finishAnnotated: () => finish.promise } });
  void select(p); await settle(); p.window.fire('keydown', { key: 'Enter' });
  p.emit('onOverlayShown', { width: 1200, height: 800 });
  void select(p); await settle();
  finish.reject(new Error('old finish failed')); await settle();
  assert.doesNotMatch(p.document.getElementById('overlayStatus').textContent, /old finish/);
});

test('the first saved width choice is not mistaken for a legacy default', () => {
  const p = page('index');
  p.run('preferences.defaultWidth = 4');
  p.module('./settings').savePreferences(p.run('preferences'));
  assert.equal(p.module('./settings').loadPreferences().defaultWidth, 4);
});

test('successful result actions cannot run again during the close delay', async () => {
  let copies = 0;
  const p = page('result', { bridge: { copyScrollResult: async () => { copies++; } } });
  p.emit('onScrollResult', { ...shot('a'), frames: 2 });
  const button = p.document.querySelector('[data-result-action="copy"]');
  button.click(); await settle(); button.click(); await settle();
  assert.equal(copies, 1);
});

test('cancelled result saving remains retryable', async () => {
  let saves = 0;
  const p = page('result', { bridge: { saveScrollResult: async () => { saves++; return ''; } } });
  p.emit('onScrollResult', { ...shot('a'), frames: 2 });
  const button = p.document.querySelector('[data-result-action="save"]');
  button.click(); await settle(); button.click(); await settle();
  assert.equal(saves, 2);
});

test('result close text distinguishes history-disabled long images', () => {
  const p = page('result', { bridge: { closeScrollResult: async () => {} } });
  p.emit('onScrollResult', { ...shot('a'), frames: 2, historyId: '' });
  p.run('closeLocally("预览已关闭")');
  const note = p.document.querySelector('.result-closed');
  assert.match(note.innerHTML, /仅保留在本次预览/);
  assert.doesNotMatch(note.innerHTML, /保存到截图历史/);
});

test('a pending pause cannot reenable controls or overwrite finishing status', async () => {
  const pause = deferred(), finish = deferred();
  const p = page('scroll', { bridge: { setScrollPaused: () => pause.promise, finishScroll: () => finish.promise } });
  p.emit('onScrollInit', { sessionId: 'session-' + p.run('sessionSequence'), auto: true });
  p.emit('onScrollProgress', { sessionId: 'session-0', frames: 2, height: 100, status: 'running', auto: true });
  void p.run('togglePause()'); void p.run('finish()');
  pause.resolve(); await settle();
  assert.equal(p.document.querySelector('[data-scroll-action="pause"]').disabled, true);
  assert.equal(p.document.getElementById('scrollStatus').textContent, '正在生成预览…');
  finish.resolve(); await settle();
});

test('a new scroll session clears old notices and discards old control completions', async () => {
  const mode = deferred();
  const p = page('scroll', { bridge: { setScrollAuto: () => mode.promise } });
  p.emit('onScrollInit', { sessionId: 'session-' + p.run('sessionSequence'), auto: false });
  p.run('showNotice("old session")'); void p.run('toggleMode()');
  p.emit('onScrollInit', { sessionId: 'session-' + p.run('sessionSequence'), auto: false });
  assert.equal(p.document.getElementById('scrollStatus').textContent, '手动滚动中…');
  mode.resolve(); await settle();
  assert.equal(p.document.querySelector('[data-scroll-action="mode"]').getAttribute('aria-pressed'), 'false');
});

test('a local selection crop uses the original pixels rather than baked annotations', async () => {
  const source = { close() {} };
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a') }, globals: { createImageBitmap: async () => source } });
  void select(p); await settle();
  p.run('engine.addShape({ kind:"rect",x:0,y:0,x2:40,y2:40,color:"#ff0000",width:2 }); watermarkEnabled = true; applyWatermark()');
  const canvas = p.document.getElementById('overlayCanvas');
  await p.run(`recaptureSelection({x:20,y:20,width:80,height:40}, ${JSON.stringify(rect)})`);
  const crop = canvas.ctx.commands.filter(([op]) => op === 'drawImage').at(-1)[1];
  assert.ok(crop.ctx.commands.find(([op]) => op === 'drawImage')[1] === source, 'crop must sample the unannotated source');
  assert.equal(p.run('engine.canUndo()'), false, 'resizing resets annotations as the UI promises');
  assert.equal(p.run('engine.getWatermark().text'), 'Pinstill');
});

for (const { edge, start, end, expected } of [
  { edge: 'n', start: [70, 10], end: [110, -20], expected: { x: 10, y: 0, width: 120, height: 90 } },
  { edge: 's', start: [70, 90], end: [30, 115], expected: { x: 10, y: 10, width: 120, height: 105 } },
  { edge: 'w', start: [10, 50], end: [2, 78], expected: { x: 2, y: 10, width: 128, height: 80 } },
  { edge: 'e', start: [130, 50], end: [157, 22], expected: { x: 10, y: 10, width: 147, height: 80 } },
]) {
  test(`dragging the ${edge} edge re-captures only along its axis`, async () => {
    const requests = [];
    const overlay = page('overlay', { bridge: {
      prepareSelection: async () => shot('original'),
      prepareSelectionFast: async (region) => {
        requests.push({ ...region });
        return { ...shot('expanded'), width: region.width, height: region.height, rect: region };
      },
    } });
    await select(overlay);
    const grip = overlay.document.querySelector(`[data-resize="${edge}"]`);
    assert.ok(grip, `missing ${edge} edge grip`);
    const pointer = { pointerId: 1, button: 0 };
    overlay.window.fire('pointerdown', { ...pointer, target: grip, clientX: start[0], clientY: start[1] });
    overlay.window.fire('pointermove', { ...pointer, clientX: end[0], clientY: end[1] });
    assert.equal(overlay.document.getElementById('overlayCanvas').hidden, true);
    assert.deepEqual(requests, []);
    overlay.window.fire('pointerup', { ...pointer, clientX: end[0], clientY: end[1] });
    await settle();
    assert.deepEqual(requests, [expected]);
    assert.equal(overlay.document.getElementById('overlayCanvas').hidden, false);
  });
}

const enlargedRect = { x: 10, y: 10, width: 180, height: 120 };
const enlargedShot = { ...shot('resized'), width: 180, height: 120, rect: enlargedRect };
const savedShape = { kind: 'rect', x: 12, y: 15, x2: 40, y2: 35, color: '#ff0000', width: 2 };

async function resizeFixture() {
  const capture = deferred(), decode = deferred();
  let sourceCloses = 0, replacementCloses = 0, decodes = 0;
  const source = { close() { sourceCloses++; } };
  const replacement = { close() { replacementCloses++; } };
  const p = page('overlay', {
    bridge: { prepareSelection: async () => shot('original'), prepareSelectionFast: () => capture.promise },
    globals: {
      createImageBitmap: () => ++decodes === 1 ? Promise.resolve(source) : decode.promise,
      Image: class { async decode() { throw new Error('fallback decode failed'); } },
    },
  });
  await select(p);
  p.run(`engine.addShape(${JSON.stringify(savedShape)}); watermarkEnabled = true; applyWatermark()`);
  const engine = p.run('engine');
  const canvas = p.document.getElementById('overlayCanvas');
  // Exercise the drag lifecycle: move changes committedRect and hides the
  // canvas before pointerup asks for the replacement capture.
  p.run('startSelectionResize({ pointerId:1, clientX:130, clientY:90, preventDefault(){}, stopPropagation(){} }, "se")');
  p.window.fire('pointermove', { pointerId: 1, clientX: 190, clientY: 130 });
  p.window.fire('pointerup', { pointerId: 1, button: 0 });
  await settle();
  return { p, capture, decode, source, replacement, engine, canvas,
    sourceCloses: () => sourceCloses, replacementCloses: () => replacementCloses, decodes: () => decodes };
}

function assertResizeRestored(f) {
  const { p, engine, canvas, source } = f;
  assert.equal(p.run('phase'), 'annotating', 'resize failure must return to the annotation surface');
  assert.equal(p.run('engine'), engine, 'retain the annotations and their undo history');
  assert.deepEqual(JSON.parse(p.run('JSON.stringify(engine.getShapes())')), [savedShape]);
  assert.equal(p.run('engine.canUndo()'), true);
  assert.equal(p.run('engine.getWatermark().text'), 'Pinstill');
  assert.deepEqual(JSON.parse(p.run('JSON.stringify(committedRect)')), rect);
  assert.equal(canvas.width, 120); assert.equal(canvas.height, 80); assert.equal(canvas.hidden, false);
  assert.equal(p.run('captureImage'), source);
  assert.equal(f.sourceCloses(), 0, 'the source bitmap must remain usable for redraw and export');
  assert.equal(canvas.ctx.commands.filter(([op]) => op === 'drawImage').at(-1)[1], source);
  const selection = p.document.getElementById('selection');
  assert.equal(selection.style.left, '10px'); assert.equal(selection.style.top, '10px');
  assert.equal(selection.style.width, '120px'); assert.equal(selection.style.height, '80px');
  assert.equal(selection.classList.contains('capturing'), false);
  assert.equal(selection.hidden, false);
  for (const id of ['annotationToolbar', 'sizeBadge', 'maskTop']) assert.equal(p.document.getElementById(id).hidden, false, id);
  assert.equal(p.document.getElementById('prepareState').hidden, true);
  assert.equal(p.document.getElementById('captureError').hidden, true, 'resize failures do not replace the old image with initial-capture guidance');
  assert.match(p.document.getElementById('demoToast').textContent, /重新截取失败.*已恢复原选区/);
}

for (const failure of ['request rejection', 'request timeout', 'decode rejection', 'decode timeout', 'render failure']) {
  test(`resize ${failure} restores the previous image and annotations despite late completion`, async () => {
    const f = await resizeFixture();
    if (failure.startsWith('decode') || failure === 'render failure') {
      f.capture.resolve(enlargedShot); await settle();
      assert.equal(f.decodes(), 2);
    }
    assert.equal(f.sourceCloses(), 0, 'keep the old source while the replacement is pending');
    if (failure.endsWith('timeout')) await f.p.tick(4000);
    else if (failure === 'decode rejection') f.decode.reject(new Error('decode failed'));
    else if (failure === 'request rejection') f.capture.reject(new Error('capture failed'));
    else {
      f.canvas.ctx.drawImage = (image, ...args) => {
        if (image === f.replacement) throw new Error('replacement drawing failed');
        f.canvas.ctx.commands.push(['drawImage', image, ...args]);
      };
      f.decode.resolve(f.replacement);
    }
    await settle();
    assertResizeRestored(f);
    f.capture.resolve(enlargedShot); f.decode.resolve(f.replacement);
    f.p.emit('onOverlayCapture', enlargedShot);
    await settle();
    assertResizeRestored(f);
    if (failure === 'decode timeout' || failure === 'render failure') assert.equal(f.replacementCloses(), 1);
    if (failure === 'request timeout') assert.equal(f.decodes(), 1, 'a late native result must not start decoding');
  });
}

test('successful resize releases the previous bitmap only after committing its replacement', async () => {
  const f = await resizeFixture();
  f.capture.resolve(enlargedShot); await settle();
  assert.equal(f.sourceCloses(), 0);
  assert.equal(f.p.run('engine'), f.engine);
  f.decode.resolve(f.replacement); await settle();
  assert.equal(f.p.run('phase'), 'annotating');
  assert.equal(f.p.run('captureImage'), f.replacement);
  assert.equal(f.canvas.width, 180); assert.equal(f.canvas.height, 120);
  assert.equal(f.p.run('engine.canUndo()'), false);
  assert.equal(f.sourceCloses(), 1);
  assert.equal(f.replacementCloses(), 0);
  f.p.run('reset()');
  assert.equal(f.sourceCloses(), 1); assert.equal(f.replacementCloses(), 1);
});

test('a resize failure from an old session cannot restore that session over a new capture', async () => {
  const f = await resizeFixture();
  f.p.emit('onOverlayShown', { width: 1200, height: 800 });
  const next = select(f.p); f.decode.resolve(f.replacement); await next;
  f.capture.reject(new Error('old resize failed')); await settle();
  assert.equal(f.p.run('captureImage'), f.replacement);
  assert.notEqual(f.p.run('engine'), f.engine);
  assert.equal(f.p.run('engine.canUndo()'), false);
  assert.equal(f.sourceCloses(), 1);
  assert.equal(f.p.document.getElementById('demoToast').hidden, true);
});

test('initial capture decode failures still show retry guidance', async () => {
  const p = page('overlay', { bridge: { prepareSelection: async () => shot('a') }, globals: {
    createImageBitmap: async () => { throw new Error('decode failed'); },
    Image: class { async decode() { throw new Error('fallback decode failed'); } },
  } });
  await select(p);
  assert.equal(p.document.getElementById('captureError').hidden, false);
  assert.equal(p.document.getElementById('captureErrorRetry').hidden, false);
});

for (const boundary of ['open settings', 'delay', 'permission result']) {
  test(`scroll permission ${boundary} from an old session cannot update a new session`, async () => {
    const opened = deferred(), granted = deferred(); let checks = 0;
    const p = page('scroll', { bridge: {
      openScrollAccessibilitySettings: () => opened.promise,
      scrollAccessibilityGranted: () => { checks++; return granted.promise; },
    } });
    p.emit('onScrollInit', { sessionId: 'session-' + p.run('sessionSequence'), auto: false, autoDenied: true, accessibilityGranted: false });
    p.document.querySelector('[data-scroll-permission]').click();
    if (boundary !== 'open settings') { opened.resolve(); await settle(); }
    if (boundary === 'permission result') await p.tick(1200);
    p.emit('onScrollInit', { sessionId: 'session-' + p.run('sessionSequence'), auto: false, autoDenied: true, accessibilityGranted: false });
    opened.resolve(); await settle(); await p.tick(1200);
    granted.resolve(true); await settle();
    assert.equal(p.document.getElementById('scrollStatus').textContent, '手动滚动中…');
    assert.equal(p.document.querySelector('[data-scroll-permission]').hidden, false);
    assert.equal(p.run('accessibilityGranted'), false);
    assert.equal(checks, boundary === 'permission result' ? 1 : 0);
  });
}

test('scroll permission refresh still updates the current session', async () => {
  const p = page('scroll', { bridge: {
    openScrollAccessibilitySettings: async () => {}, scrollAccessibilityGranted: async () => true,
  } });
  p.emit('onScrollInit', { sessionId: 'session-' + p.run('sessionSequence'), auto: false, autoDenied: true, accessibilityGranted: false });
  p.document.querySelector('[data-scroll-permission]').click(); await settle(); await p.tick(1200);
  assert.equal(p.run('accessibilityGranted'), true);
  assert.equal(p.document.querySelector('[data-scroll-permission]').hidden, true);
  assert.match(p.document.getElementById('scrollStatus').textContent, /已获得辅助功能权限/);
});
