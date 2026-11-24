import test from 'node:test';
import assert from 'node:assert/strict';
import { page, deferred, settle } from './harness.mjs';

const rect = { x: 10, y: 10, width: 120, height: 80 };
const shot = { id: 'capture', url: 'shot://capture', width: 120, height: 80, rect };
const shape = { kind: 'rect', x: 12, y: 15, x2: 40, y2: 35, color: '#ff0000', width: 2 };
async function annotatedPage(finishAnnotated) {
  let closes = 0;
  const p = page('overlay', { bridge: { prepareSelection: async () => shot, finishAnnotated },
    globals: { createImageBitmap: async () => ({ close() { closes++; } }) } });
  await p.run(`prepareSelectionFor(${JSON.stringify(rect)})`);
  p.run(`engine.addShape(${JSON.stringify(shape)})`);
  return { p, engine: p.run('engine'), closes: () => closes };
}

test('cancelled annotation saving keeps the image and annotations editable and retryable', async () => {
  let calls = 0;
  const { p, engine, closes } = await annotatedPage(async () => { calls++; return { completed: false, cancelled: true }; });
  await p.run('runAction("save")');
  assert.equal(p.run('phase'), 'annotating');
  assert.equal(p.run('engine'), engine); assert.equal(p.run('engine.canUndo()'), true);
  assert.equal(p.document.getElementById('overlayCanvas').hidden, false);
  assert.equal(p.document.querySelector('[data-action="save"]').disabled, false);
  assert.equal(closes(), 0);
  await p.run('runAction("save")'); assert.equal(calls, 2);
});

test('successful annotation completion releases the image and clears the surface', async () => {
  const { p, closes } = await annotatedPage(async () => ({ completed: true, cancelled: false }));
  await p.run('runAction("copy")');
  assert.equal(p.run('phase'), 'idle'); assert.ok(p.run('engine') === null);
  assert.equal(p.document.getElementById('overlayCanvas').hidden, true);
  assert.equal(closes(), 1);
});

test('explicit annotation cancellation clears the session', async () => {
  const { p, closes } = await annotatedPage(async () => ({ completed: false, cancelled: true }));
  await p.run('runAction("cancel")');
  assert.ok(p.run('engine') === null); assert.equal(closes(), 1);
});

test('failed annotation finishing leaves annotations editable with an error', async () => {
  const { p, engine, closes } = await annotatedPage(async () => { throw new Error('disk full'); });
  await p.run('runAction("save")');
  assert.equal(p.run('phase'), 'annotating'); assert.equal(p.run('engine'), engine);
  assert.equal(closes(), 0); assert.match(p.document.getElementById('overlayStatus').textContent, /disk full/);
});

test('an obsolete successful annotation action cannot clear a newer capture', async () => {
  const finished = deferred();
  const { p } = await annotatedPage(() => finished.promise);
  const old = p.run('runAction("copy")');
  p.emit('onOverlayShown', { width: 1200, height: 800 });
  await p.run(`prepareSelectionFor(${JSON.stringify(rect)})`);
  const engine = p.run('engine');
  finished.resolve({ completed: true, cancelled: false }); await old;
  assert.equal(p.run('engine'), engine); assert.equal(p.run('phase'), 'annotating');
});

test('failed automatic finishing returns to an editable capture instead of discarding it', async () => {
  const p = page('overlay', { bridge: {
    settings: async () => ({ afterCapture: 'pin' }), prepareSelection: async () => shot,
    finishAnnotated: async () => { throw new Error('pin failed'); },
  }, globals: { FileReader: class {
    readAsDataURL() { this.result = 'data:image/png;base64,aW1hZ2U='; this.onload(); }
  } } });
  await p.run(`prepareSelectionFor(${JSON.stringify(rect)})`);
  assert.equal(p.run('phase'), 'annotating'); assert.ok(p.run('engine'));
  assert.equal(p.document.getElementById('overlayCanvas').hidden, false);
  assert.match(p.document.getElementById('overlayStatus').textContent, /pin failed/);
});

const init = sessionId => ({ sessionId, rect, auto: false, autoDenied: false, accessibilityGranted: false });
const progress = (sessionId, frames = 2) => ({ sessionId, frames, height: frames * 100, status: 'running', auto: false });

test('scroll progress from another or missing session cannot overwrite the current capture', () => {
  const p = page('scroll');
  p.emit('onScrollInit', init('old')); p.emit('onScrollInit', init('new'));
  p.emit('onScrollProgress', progress('new', 3));
  const before = p.document.getElementById('scrollProgress').textContent;
  p.emit('onScrollProgress', { ...progress('old', 9), status: 'paused', auto: true });
  p.emit('onScrollProgress', progress('', 7));
  p.emit('onScrollProgress', { frames: 8, height: 900, status: 'paused' });
  assert.equal(p.document.getElementById('scrollProgress').textContent, before);
  assert.equal(p.document.querySelector('[data-scroll-action="mode"]').getAttribute('aria-pressed'), 'false');
  assert.equal(p.document.querySelector('[data-scroll-action="pause"]').textContent, '暂停');
});

test('scroll ignores progress before init and duplicate or retired session init events', () => {
  const p = page('scroll');
  p.emit('onScrollProgress', progress('unannounced'));
  assert.equal(p.run('frames'), 0);
  p.emit('onScrollInit', init('old')); p.emit('onScrollInit', init('new'));
  p.emit('onScrollProgress', progress('new', 3));
  p.emit('onScrollInit', init('new')); p.emit('onScrollInit', init('old')); p.emit('onScrollInit', init(''));
  assert.equal(p.run('frames'), 3);
  p.emit('onScrollProgress', progress('new', 4)); assert.equal(p.run('frames'), 4);
});

test('a never-seen older numeric scroll init cannot replace a newer session', () => {
  const p = page('scroll');
  p.emit('onScrollInit', init('scroll-2'));
  p.emit('onScrollProgress', progress('scroll-2', 3));
  p.emit('onScrollInit', init('scroll-1'));
  assert.equal(p.run('sessionId'), 'scroll-2'); assert.equal(p.run('frames'), 3);
  p.emit('onScrollInit', init('scroll-9007199254740993'));
  p.emit('onScrollInit', init('scroll-9007199254740992'));
  assert.equal(p.run('sessionId'), 'scroll-9007199254740993');
});

test('scroll browser demo creates a fresh session and advances only that session', async () => {
  const p = page('scroll', { bridge: { hasNative: false } });
  const first = p.run('typeof sessionId === "string" ? sessionId : ""');
  assert.ok(first, 'demo needs the same session identity as native progress');
  await p.tick(700); assert.equal(p.run('frames'), 1);
  p.run('startDemo()');
  assert.notEqual(p.run('sessionId'), first);
  p.emit('onScrollProgress', progress(first, 9)); assert.equal(p.run('frames'), 0);
  await p.tick(700); assert.equal(p.run('frames'), 1);
});

test('scroll commands carry the session that issued them and do nothing before init', async () => {
  const calls = [];
  const p = page('scroll', { bridge: {
    setScrollPaused: async (...args) => calls.push(['pause', ...args]),
    setScrollAuto: async (...args) => calls.push(['auto', ...args]),
    finishScroll: async (...args) => calls.push(['finish', ...args]),
  } });
  await p.run('togglePause(); toggleMode(); finish(true)');
  assert.equal(calls.length, 0);
  p.emit('onScrollInit', init('active'));
  await p.run('togglePause()'); await p.run('toggleMode()'); await p.run('finish(true)');
  assert.deepEqual(calls, [['pause', 'active', true], ['auto', 'active', true], ['finish', 'active', 'cancel']]);
});

for (const budget of ['canvas dimensions', 'decoded image dimensions', 'encoded image bytes']) {
  test(`resize rejects excessive ${budget} before canvas allocation and retains annotations`, async () => {
    let decodes = 0, oldCloses = 0, newCloses = 0;
    const next = { ...shot, rect: { ...rect, width: 180, height: 120 } };
    if (budget === 'canvas dimensions') { next.width = 8192; next.height = 4097; }
    const p = page('overlay', { bridge: { prepareSelection: async () => shot, prepareSelectionFast: async () => next }, globals: {
      createImageBitmap: async () => ++decodes === 1
        ? { width: 120, height: 80, close() { oldCloses++; } }
        : { width: budget === 'decoded image dimensions' ? 8192 : 120,
          height: budget === 'decoded image dimensions' ? 4097 : 80, close() { newCloses++; } },
    } });
    await p.run(`prepareSelectionFor(${JSON.stringify(rect)})`);
    p.run(`engine.addShape(${JSON.stringify(shape)})`);
    const engine = p.run('engine');
    const canvas = p.document.getElementById('overlayCanvas');
    const allocations = [];
    let width = canvas.width, height = canvas.height;
    Object.defineProperties(canvas, {
      width: { get: () => width, set: v => { width = v; allocations.push(width * height); } },
      height: { get: () => height, set: v => { height = v; allocations.push(width * height); } },
    });
    if (budget === 'encoded image bytes') p.context.fetch = async () => ({ ok: true, blob: async () => ({ size: (64 << 20) + 1 }) });
    await p.run(`recaptureSelection(${JSON.stringify(next.rect)}, ${JSON.stringify(rect)})`);
    assert.ok(p.run('engine') === engine, 'retain original annotations');
    assert.equal(p.run('engine.canUndo()'), true); assert.equal(oldCloses, 0);
    assert.equal(canvas.width, 120); assert.equal(canvas.height, 80); assert.equal(canvas.hidden, false);
    assert.ok(allocations.every(pixels => pixels <= (32 << 20)), 'never allocate an oversized canvas');
    assert.equal(decodes, budget === 'decoded image dimensions' ? 2 : 1);
    assert.equal(newCloses, budget === 'decoded image dimensions' ? 1 : 0);
    assert.match(p.document.getElementById('demoToast').textContent, /重新截取失败.*已恢复原选区/);
  });
}

function loadedPin(bridge = {}, { width = 240, height = 160, pixelsWidth = width, pixelsHeight = height } = {}) {
  const p = page('pin', { bridge });
  p.window.screen = { availWidth: 1200, availHeight: 800 };
  p.emit('onPinInit', { id: 'pin', url: 'shot://pin', width, height, opacity: 100, border: true });
  const image = p.document.getElementById('pinImage');
  image.naturalWidth = pixelsWidth; image.naturalHeight = pixelsHeight; image.onload();
  return p;
}

test('tall pins fit within the screen below the usual minimum width and preserve image aspect', async () => {
  const calls = [];
  const p = loadedPin({ resizePin: async (...args) => calls.push(args), updatePinState: async () => {} },
    { width: 96, height: 720, pixelsWidth: 100, pixelsHeight: 1000 });
  await p.run('resetSize()');
  assert.deepEqual(calls.at(-1), ['pin', 72, 720]);
  assert.equal(p.run('aspect'), 0.1);
  assert.deepEqual(JSON.parse(p.run('JSON.stringify(fitToScreen(500))')), { width: 72, height: 720 });
});

test('pin grip resize uses image aspect even when initial window dimensions were clamped', async () => {
  const calls = [];
  const p = loadedPin({ resizePin: async (...args) => calls.push(args), updatePinState: async () => {} },
    { width: 96, height: 720, pixelsWidth: 100, pixelsHeight: 1000 });
  p.document.getElementById('pinResize').fire('pointerdown', { button: 0, clientX: 96, pointerId: 1 });
  p.window.fire('pointermove', { clientX: 196, pointerId: 1 });
  p.window.fire('pointerup'); await settle();
  assert.deepEqual(calls.at(-1), ['pin', 72, 720]);
});

test('pin resize serializes native requests and applies the latest size after the prior request settles', async () => {
  const calls = [];
  const p = loadedPin({
    resizePin: (...args) => {
      const request = deferred();
      calls.push({ args, ...request });
      return request.promise;
    },
    updatePinState: async () => {},
  });
  const handle = p.document.getElementById('pinResize');
  handle.fire('pointerdown', { button: 0, clientX: 240, pointerId: 1 });
  p.window.fire('pointermove', { clientX: 300, pointerId: 1 });
  await p.tick(70);
  assert.equal(calls.length, 1);

  p.window.fire('pointermove', { clientX: 360, pointerId: 1 });
  await p.tick(70);
  p.window.fire('pointermove', { clientX: 390, pointerId: 1 });
  p.window.fire('pointerup');
  await settle();
  assert.equal(calls.length, 1, 'a newer resize must wait for the in-flight native request');

  calls[0].resolve();
  await settle();
  assert.equal(calls.length, 2);
  assert.deepEqual(calls[1].args, ['pin', 390, 260]);
  calls[1].resolve();
  await settle();
});

test('pin pointer release does not repeat an unchanged in-flight resize', async () => {
  const request = deferred(); let calls = 0;
  const p = loadedPin({ resizePin: () => { calls++; return request.promise; }, updatePinState: async () => {} });
  p.document.getElementById('pinResize').fire('pointerdown', { button: 0, clientX: 240, pointerId: 1 });
  p.window.fire('pointermove', { clientX: 300, pointerId: 1 }); await p.tick(70);
  p.window.fire('pointerup'); await settle();
  assert.equal(calls, 1);
  request.resolve(); await settle(); assert.equal(calls, 1);
});

test('a rejected native resize restores the last persisted dimensions', async () => {
  const calls = [];
  const p = loadedPin({ resizePin: (...args) => { const d = deferred(); calls.push({ args, ...d }); return d.promise; } });
  p.run('applyResize(300, 200)'); await p.tick(70);
  p.run('applyResize(360, 240)'); await p.tick(70);
  calls[0].resolve(); await settle();
  assert.deepEqual(calls.map(request => request.args), [['pin', 300, 200], ['pin', 360, 240]]);
  assert.equal(p.run('acceptedWidth'), 300); assert.equal(p.run('acceptedHeight'), 200);
  calls[1].reject(new Error('resize rejected')); await settle();
  assert.equal(p.run('currentWidth'), 300); assert.equal(p.run('currentHeight'), 200);
  assert.match(p.document.getElementById('pinToast').textContent, /resize rejected/);
});

test('pin opacity is serialized and failures restore the last accepted value', async () => {
  const calls = [];
  const p = loadedPin({ setPinOpacity: (...args) => { const d = deferred(); calls.push({ args, ...d }); return d.promise; } });
  const first = p.run('adjustOpacity(-10)'); await settle();
  const second = p.run('adjustOpacity(-10)'); await settle();
  assert.equal(calls.length, 1, 'opacity mutations cannot overlap');
  calls[0].resolve(); await first; await settle();
  assert.deepEqual(calls[1].args, ['pin', 80]);
  assert.equal(p.run('acceptedOpacity'), 90);
  calls[1].reject(new Error('opacity rejected')); await second; await settle();
  assert.equal(p.run('opacity'), 90);
  assert.match(p.document.getElementById('pinToast').textContent, /opacity rejected/);
});

test('an older failed opacity request cannot roll back a newer successful choice', async () => {
  const calls = [];
  const p = loadedPin({ setPinOpacity: () => { const d = deferred(); calls.push(d); return d.promise; }, updatePinState: async () => {} });
  const first = p.run('adjustOpacity(-10)'); await settle();
  const second = p.run('adjustOpacity(-10)');
  calls[0].reject(new Error('older failure')); await first; await settle();
  assert.equal(p.run('opacity'), 80);
  calls[1].resolve(); await second;
  assert.equal(p.run('opacity'), 80);
});

test('returning to an earlier opacity value still invalidates that earlier failure', async () => {
  const calls = [];
  const p = loadedPin({ setPinOpacity: () => { const d = deferred(); calls.push(d); return d.promise; }, updatePinState: async () => {} });
  const first = p.run('adjustOpacity(-10)'); await settle();
  const second = p.run('adjustOpacity(-10)');
  const third = p.run('adjustOpacity(10)');
  calls[0].reject(new Error('older failure')); await first; await settle();
  assert.equal(p.run('opacity'), 90, 'the newer request owns feedback even when its value repeats');
  calls[1].resolve(); await second; await settle();
  calls[2].resolve(); await third;
  assert.equal(p.run('opacity'), 90);
});

test('pin resize and opacity wait for the same native mutation queue', async () => {
  const resized = deferred(), opacity = deferred(), calls = [];
  const p = loadedPin({ resizePin: (...args) => { calls.push(['resize', ...args]); return resized.promise; },
    setPinOpacity: (...args) => { calls.push(['opacity', ...args]); return opacity.promise; } });
  p.run('applyResize(300, 200)'); await p.tick(70);
  const changing = p.run('adjustOpacity(-10)'); await settle();
  assert.deepEqual(calls, [['resize', 'pin', 300, 200]]);
  resized.resolve(); await settle();
  assert.deepEqual(calls[1], ['opacity', 'pin', 90]);
  opacity.resolve(); await changing;
});

test('pin fit never rounds extremely thin images down to a zero dimension', () => {
  const p = loadedPin({}, { width: 1000, height: 1, pixelsWidth: 10000, pixelsHeight: 1 });
  const size = JSON.parse(p.run('JSON.stringify(originalSize())'));
  assert.equal(size.width, 1080); assert.equal(size.height, 1);
});

test('failed pin reset keeps the error visible instead of reporting success', async () => {
  const p = loadedPin({ resizePin: async () => { throw new Error('window unavailable'); } });
  await p.run('resetSize()');
  assert.match(p.document.getElementById('pinToast').textContent, /调整大小失败.*window unavailable/);
  assert.doesNotMatch(p.document.getElementById('pinToast').textContent, /已恢复/);
});

test('failed pin state persistence is visible', async () => {
  const p = loadedPin({ setPinOpacity: async () => { throw new Error('store unavailable'); } });
  await p.run('adjustOpacity(-10)');
  assert.equal(p.document.getElementById('pinToast').hidden, false);
  assert.match(p.document.getElementById('pinToast').textContent, /store unavailable/);
});

test('failed pin deletion leaves the window open with a retryable error', async () => {
  let fallbacks = 0, calls = 0;
  const p = loadedPin({ closePin: async () => { calls++; throw new Error('delete failed'); } });
  p.window.close = () => fallbacks++;
  await p.run('closeWindow()');
  assert.equal(fallbacks, 0, 'do not bypass backend persistence failure by closing directly');
  assert.equal(p.run('closing'), false);
  assert.match(p.document.getElementById('pinToast').textContent, /delete failed/);
  await p.run('closeWindow()'); assert.equal(calls, 2);
});

test('history partial settings failures have readable labels and remain retryable', async () => {
  let calls = 0;
  const p = page('index', { bridge: { applySettings: async () => ++calls === 1
    ? { keepHistory: 'clear failed', retentionDays: 'prune failed', historyLimitMB: 'limit failed' } : {} } });
  await settle();
  const status = p.document.getElementById('settingsStatus');
  assert.match(status.textContent, /保存历史.*clear failed/);
  assert.match(status.textContent, /保留.*prune failed/);
  assert.match(status.textContent, /容量.*limit failed/);
  p.run('scheduleApplySettings()'); await p.tick(160);
  assert.equal(calls, 2); assert.doesNotMatch(status.textContent, /failed/);
});

test('annotation export failure preserves editing and shows a recoverable error', async () => {
  let calls = 0;
  const { p, engine, closes } = await annotatedPage(async () => { calls++; return { completed: true, cancelled: false }; });
  p.document.getElementById('overlayCanvas').toDataURL = () => { throw new Error('encoding failed'); };
  await p.run('runAction("save")');
  assert.ok(p.run('engine') === engine); assert.equal(p.run('phase'), 'annotating');
  assert.equal(closes(), 0); assert.equal(calls, 0);
  assert.match(p.document.getElementById('overlayStatus').textContent, /encoding failed/);
});

test('oversized encoded annotation is rejected before the bridge and stays editable', async () => {
  let calls = 0;
  const { p, engine, closes } = await annotatedPage(async () => { calls++; return { completed: true, cancelled: false }; });
  // A repeat string provides length metadata without allocating an image/canvas.
  engine.toPNGBase64 = () => 'A'.repeat(Math.ceil((64 << 20) / 3) * 4 + 4);
  await p.run('runAction("save")');
  assert.equal(calls, 0); assert.ok(p.run('engine') === engine);
  assert.equal(p.run('phase'), 'annotating'); assert.equal(closes(), 0);
  assert.match(p.document.getElementById('overlayStatus').textContent, /64 MiB/);
});

function watermarkPage(storage = {}) {
  return page('overlay', { storage, bridge: { prepareSelection: async () => shot } });
}
function rememberWatermark(p, text, opacity) {
  const textInput = p.document.getElementById('watermarkText'); textInput.value = text; textInput.fire('input');
  const opacityInput = p.document.getElementById('watermarkOpacity'); opacityInput.value = String(opacity); opacityInput.fire('input');
}
async function shownWatermark(p) {
  p.emit('onOverlayShown', { width: 1200, height: 800 });
  await p.run(`prepareSelectionFor(${JSON.stringify(rect)})`);
  p.run('toggleWatermarkTool()');
  return p.run('engine.getWatermark()');
}

for (const changed of [{ watermarkText: 'New default' }, { watermarkOpacity: 55 }]) {
  test(`general watermark ${Object.keys(changed)[0]} changes supersede the remembered toolbar values`, async () => {
    const storage = { 'pinstill.preferences': JSON.stringify({ watermarkText: 'General', watermarkOpacity: 35 }) };
    const p = watermarkPage(storage);
    rememberWatermark(p, 'Toolbar', 70);
    const defaults = { watermarkText: 'General', watermarkOpacity: 35, ...changed };
    storage['pinstill.preferences'] = JSON.stringify(defaults);
    const layer = await shownWatermark(p);
    assert.equal(layer.text, defaults.watermarkText); assert.equal(layer.opacity, defaults.watermarkOpacity);
    const reopened = watermarkPage(storage);
    const next = await shownWatermark(reopened);
    assert.equal(next.text, defaults.watermarkText); assert.equal(next.opacity, defaults.watermarkOpacity);
  });
}

test('watermark toolbar overrides survive unrelated preferences but follow later general changes', async () => {
  const storage = { 'pinstill.preferences': JSON.stringify({ watermarkText: 'General', watermarkOpacity: 35 }) };
  const p = watermarkPage(storage); rememberWatermark(p, 'Toolbar', 70);
  storage['pinstill.preferences'] = JSON.stringify({ watermarkText: 'General', watermarkOpacity: 35, sound: false });
  let layer = await shownWatermark(p); assert.equal(layer.text, 'Toolbar'); assert.equal(layer.opacity, 70);
  storage['pinstill.preferences'] = JSON.stringify({ watermarkText: 'General 2', watermarkOpacity: 45 });
  await shownWatermark(p); rememberWatermark(p, 'New toolbar', 60);
  layer = await shownWatermark(p); assert.equal(layer.text, 'New toolbar'); assert.equal(layer.opacity, 60);
  storage['pinstill.preferences'] = JSON.stringify({ watermarkText: 'General 3', watermarkOpacity: 50 });
  layer = await shownWatermark(p); assert.equal(layer.text, 'General 3'); assert.equal(layer.opacity, 50);
});

test('legacy watermark storage gets a baseline so later general changes take effect', async () => {
  const storage = { 'pinstill.preferences': JSON.stringify({ watermarkText: 'General', watermarkOpacity: 35 }),
    'pinstill.watermark': JSON.stringify({ text: 'Legacy', opacity: 65 }) };
  const p = watermarkPage(storage);
  let layer = await shownWatermark(p); assert.equal(layer.text, 'Legacy');
  storage['pinstill.preferences'] = JSON.stringify({ watermarkText: 'Updated', watermarkOpacity: 40 });
  layer = await shownWatermark(p); assert.equal(layer.text, 'Updated'); assert.equal(layer.opacity, 40);
});

test('OCR shortcut is visibly inactive and cannot start shortcut recording', () => {
  const p = page('index'); p.run('settingsCategory = "shortcuts"; renderSettings()');
  const button = p.document.getElementById('setting-ocrKey');
  assert.equal(button.disabled, true);
  assert.match(button.textContent, /未启用|待接入/);
  button.click(); assert.equal(p.run('recordingShortcut'), null);
});

test('inactive OCR shortcut does not reserve its key against active actions', () => {
  const p = page('index'); p.run('preferences.ocrKey = "Ctrl+Alt+O"; recordingShortcut = "captureKey"');
  p.document.fire('keydown', { key: 'o', code: 'KeyO', ctrlKey: true, altKey: true, metaKey: false, shiftKey: false });
  assert.equal(p.run('preferences.captureKey'), 'Ctrl+Alt+O');
});

test('OCR language can be selected on macOS', () => {
  const p = page('index'); p.run('settingsCategory = "recognition"; renderSettings()');
  const language = p.document.getElementById('setting-ocrLanguage');
  assert.equal(language.disabled, false);
  assert.equal(language.value, '中英文混合');
});

test('inactive OCR keeps the remembered key locally but sends no native registration', () => {
  const p = page('index');
  const settings = p.module('./settings');
  const prefs = { ...settings.DEFAULT_PREFERENCES, ocrKey: 'Ctrl+Alt+O' };
  assert.equal(settings.toNativeSettings(prefs).ocrKey, '');
  assert.equal(prefs.ocrKey, 'Ctrl+Alt+O');
});
