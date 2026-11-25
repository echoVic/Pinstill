import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, page, settle } from './harness.mjs';

const region = { x: 20, y: 30, width: 240, height: 160 };
const encodedPNG = 'Y29tcG9zaXRlZA==';
const text = '截图文字\nPinstill OCR';
const model = { mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'vision-model', hasApiKey: true, revision: 'saved-model' };
const localModel = { ...model, mode: 'local', baseUrl: 'http://127.0.0.1:11434/v1', hasApiKey: false };

async function openOverlay(bridge = {}) {
  const overlay = page('overlay', {
    bridge: {
      prepareSelection: async () => ({ id: 'test-preview', url: 'shot://test', width: 480, height: 320, rect: region }),
      modelSettings: async () => ({ mode: 'off', revision: '' }),
      ...bridge,
    },
    storage: { 'pinstill.preferences': JSON.stringify({ ocrLanguage: 'English', translateTo: '日本語' }) },
    globals: { FileReader: class {
      readAsDataURL() {
        this.result = `data:image/png;base64,${encodedPNG}`;
        this.onload();
      }
    } },
  });
  await overlay.run(`prepareSelectionFor(${JSON.stringify(region)})`);
  const canvas = overlay.document.getElementById('overlayCanvas');
  canvas.toBlob = (callback, format) => {
    assert.deepEqual([canvas.width, canvas.height, format], [480, 320, 'image/png']);
    assert.ok(canvas.getContext('2d').commands.some(([method]) => method === 'drawImage'));
    callback(new Blob(['composited'], { type: format }));
  };
  return overlay;
}

function chooseTool(overlay, tool) {
  overlay.document.getElementById('annotationToolbar').fire('click', {
    target: overlay.document.querySelector(`[data-tool="${tool}"]`),
  });
}

test('OCR sends the full-resolution composited image and copies a selection or all lines', async () => {
  const requests = [];
  const clipboard = [];
  const overlay = await openOverlay({
    recognizeSelection: async (mode, language, image, signal) => {
      requests.push([mode, language, image, signal.aborted]);
      assert.ok(overlay.document.getElementById('overlayCanvas').getContext('2d').commands.some(([method]) => method === 'clip'));
      return { text };
    },
    copyRecognizedText: async (value) => { clipboard.push(value); },
  });
  overlay.run('engine.addShape({ kind: "mosaic", x: 10, y: 10, x2: 80, y2: 45, width: 4, color: "#000000" })');
  chooseTool(overlay, 'ocr');
  await settle();
  assert.deepEqual(requests, [['ocr', 'English', encodedPNG, false]]);
  assert.deepEqual(clipboard, []);
  const result = overlay.document.querySelector('#recognitionPanel textarea');
  assert.equal(result.value, text);
  result.selectionStart = 0;
  result.selectionEnd = 4;
  result.fire('select');
  overlay.document.querySelector('[data-recognition-copy-selection]').click();
  await settle();
  overlay.document.querySelector('[data-recognition-copy-all]').click();
  await settle();
  assert.deepEqual(clipboard, ['截图文字', text]);
  overlay.window.fire('keydown', { key: 'Escape', target: result });
  assert.equal(overlay.document.getElementById('recognitionPanel').open, false);
  assert.equal(overlay.run('engine.getShapes()[0].kind'), 'mosaic');
  assert.equal(overlay.run('phase'), 'annotating');
});

test('QR results show every payload and only open a web URL when clicked', async () => {
  const link = 'https://example.test/path?part=1';
  const opened = [];
  const clipboard = [];
  const overlay = await openOverlay({
    recognizeSelection: async () => ({ codes: [{ text: '普通文本' }, { text: link, url: link }, { text: 'file:///example' }] }),
    copyRecognizedText: async (value) => { clipboard.push(value); },
    openRecognizedLink: async (value) => { opened.push(value); },
  });
  chooseTool(overlay, 'qr');
  await settle();
  assert.deepEqual(overlay.document.querySelectorAll('[data-recognition-code]').map((row) => row.textContent), ['普通文本', link, 'file:///example']);
  assert.deepEqual(opened, []);
  const openButtons = overlay.document.querySelectorAll('[data-recognition-open]');
  assert.equal(openButtons.length, 1);
  overlay.document.querySelector('[data-recognition-copy-code]').click();
  await settle();
  assert.deepEqual(clipboard, ['普通文本']);
  openButtons[0].click();
  await settle();
  assert.deepEqual(opened, [link]);
});

test('dismissing a pending recognition aborts it and ignores a late reply', async () => {
  const request = deferred();
  let signal;
  const overlay = await openOverlay({
    recognizeSelection: (_, __, ___, options) => { signal = options; return request.promise; },
  });
  chooseTool(overlay, 'ocr');
  await settle();
  assert.equal(signal?.aborted, false);
  overlay.window.fire('keydown', { key: 'Escape' });
  assert.equal(signal.aborted, true);
  request.resolve({ text: 'stale' });
  await settle();
  assert.equal(overlay.document.getElementById('recognitionPanel').open, false);
  assert.equal(overlay.document.querySelector('#recognitionPanel textarea'), null);
  assert.equal(overlay.run('phase'), 'annotating');
});

test('Enter in recognition results does not export the screenshot', async () => {
  const actions = [];
  const overlay = await openOverlay({
    recognizeSelection: async () => ({ text }),
    finishAnnotated: async (action) => { actions.push(action); return { completed: true }; },
  });
  chooseTool(overlay, 'ocr');
  await settle();
  assert.ok(overlay.document.getElementById('recognitionPanel')?.open);
  overlay.window.fire('keydown', { key: 'Enter', target: overlay.document.querySelector('#recognitionPanel textarea') });
  await settle();
  assert.deepEqual(actions, []);
});

test('screenshot translation uses the configured local model for OCR before translating the result', async () => {
  const calls = [];
  const copied = [];
  const overlay = await openOverlay({
    modelSettings: async () => localModel,
    recognizeSelection: async () => { assert.fail('Vision ran before the configured model'); },
    recognizeWithModel: async (revision, image) => {
      calls.push(['model-ocr', revision, image]);
      return text;
    },
    startTranslation: async (revision, value, target) => {
      calls.push(['translate', revision, value, target]);
      return 'token-one';
    },
    pollTranslation: async () => ({ text: 'スクリーンショットの文字', done: true, error: '' }),
    copyRecognizedText: async (value) => { copied.push(value); },
  });
  chooseTool(overlay, 'translate');
  await settle();
  assert.deepEqual(calls, [['model-ocr', localModel.revision, encodedPNG], ['translate', localModel.revision, text, 'ja']]);
  assert.deepEqual(overlay.document.querySelectorAll('#recognitionPanel textarea').map((field) => field.value), [text, 'スクリーンショットの文字']);
  overlay.document.querySelector('[data-recognition-copy-translation]').click();
  await settle();
  assert.deepEqual(copied, ['スクリーンショットの文字']);
});

test('cloud model OCR waits for explicit approval of the shown destination', async () => {
  const calls = [];
  const overlay = await openOverlay({
    modelSettings: async () => model,
    recognizeSelection: async () => { assert.fail('Vision ran before the configured model'); },
    recognizeWithModel: async (revision, image, signal) => {
      calls.push([revision, image, signal.aborted]);
      return '手写文字';
    },
  });
  chooseTool(overlay, 'ocr');
  await settle();
  assert.deepEqual(calls, []);
  assert.match(overlay.document.querySelector('[data-recognition-model-consent]').textContent, /models\.example\.test/);
  overlay.document.querySelector('[data-recognition-confirm-model-ocr]').click();
  await settle();
  assert.deepEqual(calls, [[model.revision, encodedPNG, false]]);
  assert.equal(overlay.document.querySelector('#recognitionPanel textarea').value, '手写文字');
});

test('dismissing cloud screenshot translation confirmation sends no image or text', async () => {
  const uploads = [];
  const overlay = await openOverlay({
    modelSettings: async () => model,
    recognizeWithModel: async (...args) => { uploads.push(args); return 'result'; },
    translateModel: async (...args) => { uploads.push(args); return 'result'; },
  });
  chooseTool(overlay, 'translate');
  await settle();
  assert.ok(overlay.document.querySelector('[data-recognition-confirm-model-ocr]'));
  overlay.window.fire('keydown', { key: 'Escape' });
  assert.deepEqual(uploads, []);
  assert.equal(overlay.document.getElementById('recognitionPanel').open, false);
  assert.equal(overlay.run('phase'), 'annotating');
});

test('a stalled built-in OCR request times out and ignores a late result', async () => {
  const pending = deferred();
  let nativeSignal;
  const overlay = await openOverlay({
    recognizeSelection: async (mode, language, image, signal) => {
      nativeSignal = signal;
      return pending.promise;
    },
  });
  chooseTool(overlay, 'ocr');
  await settle();
  assert.equal(nativeSignal.aborted, false);
  await overlay.tick(15000);
  assert.equal(nativeSignal.aborted, true);
  assert.match(overlay.document.getElementById('recognitionStatus').textContent, /超时/);
  pending.resolve({ text: 'late OCR result' });
  await settle();
  assert.equal(overlay.document.querySelector('#recognitionPanel textarea'), null);
});

test('model OCR results can be translated as text on request', async () => {
  const requests = [];
  const overlay = await openOverlay({
    modelSettings: async () => localModel,
    recognizeSelection: async () => { assert.fail('Vision ran before the configured model'); },
    recognizeWithModel: async () => '手写文字',
    startTranslation: async (revision, value, target) => {
      requests.push([revision, value, target]);
      return 'token-two';
    },
    pollTranslation: async () => ({ text: '手書きの文字', done: true, error: '' }),
  });
  chooseTool(overlay, 'ocr');
  await settle();
  assert.deepEqual(requests, []);
  overlay.document.querySelector('[data-recognition-translate-model-text]').click();
  await settle();
  assert.deepEqual(requests, [[model.revision, '手写文字', 'ja']]);
  assert.deepEqual(overlay.document.querySelectorAll('#recognitionPanel textarea').map((field) => field.value), ['手写文字', '手書きの文字']);
});

test('cloud translation offers built-in OCR and keeps the image on the Mac when selected', async () => {
  const calls = [];
  const overlay = await openOverlay({
    modelSettings: async () => model,
    recognizeSelection: async (mode, language, image) => {
      calls.push([mode, language, image]);
      return { text };
    },
    recognizeWithModel: async () => { assert.fail('the image was sent to the cloud'); },
    startTranslation: async (revision, value, target) => {
      calls.push(['translate', revision, value, target]);
      return 'token-three';
    },
    pollTranslation: async () => ({ text: 'translated text', done: true, error: '' }),
  });
  chooseTool(overlay, 'translate');
  await settle();
  assert.deepEqual(calls, []);
  overlay.document.querySelector('[data-recognition-local-ocr]').click();
  await settle();
  assert.deepEqual(calls, [['ocr', 'English', encodedPNG], ['translate', model.revision, text, 'ja']]);
  assert.equal(overlay.document.querySelector('textarea[aria-label="译文"]').value, 'translated text');
});

test('switching to built-in OCR cancels a pending model and keeps the new result', async () => {
  const pending = deferred();
  let modelSignal;
  const overlay = await openOverlay({
    modelSettings: async () => localModel,
    recognizeWithModel: async (revision, image, signal) => {
      modelSignal = signal;
      return pending.promise;
    },
    recognizeSelection: async () => ({ text }),
  });
  chooseTool(overlay, 'ocr');
  await settle();
  assert.equal(modelSignal.aborted, false);
  overlay.document.querySelector('[data-recognition-local-ocr]').click();
  await settle();
  assert.equal(modelSignal.aborted, true);
  pending.resolve('stale model recognition');
  await settle();
  assert.equal(overlay.document.querySelector('#recognitionPanel textarea').value, text);
});
