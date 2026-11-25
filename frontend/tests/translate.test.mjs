import assert from 'node:assert/strict';
import test from 'node:test';
import { deferred, page, settle } from './harness.mjs';

test('translate window streams translation, auto-scrolls, copies with feedback, and auto-clears status', async () => {
  const started = [];
  const cancelled = [];
  const clipboard = [];
  const polls = [
    { text: '第一段', done: false, error: '' },
    { text: '第一段\n第二段', done: true, error: '' },
  ];

  const win = page('translate', {
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      startTranslation: async (revision, text, target, source, style) => {
        started.push([revision, text, target, source, style]);
        return 'tok-1';
      },
      pollTranslation: async (token) => {
        assert.equal(token, 'tok-1');
        return polls.shift();
      },
      cancelTranslation: async (token) => { cancelled.push(token); },
      copyRecognizedText: async (text) => { clipboard.push(text); },
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const sourceLang = win.document.getElementById('source-lang');
  const result = win.document.getElementById('result');
  const resultField = win.document.getElementById('result-field');
  const translateBtn = win.document.getElementById('translate');
  const copyResult = win.document.getElementById('copy-result');
  const copyBoth = win.document.getElementById('copy-both');
  const status = win.document.getElementById('status');

  assert.equal(copyResult.disabled, true);
  assert.equal(copyBoth.disabled, true);

  sourceLang.value = 'auto';
  source.value = 'Hello\nWorld';
  source.fire('input');
  assert.equal(copyBoth.disabled, true);

  result.clientHeight = 100;
  result.scrollHeight = 180;
  result.scrollTop = 70;

  translateBtn.click();
  await settle();

  assert.deepEqual(started, [['rev-1', 'Hello\nWorld', 'zh-Hans', 'auto', 'standard']]);
  assert.equal(result.value, '第一段');
  assert.equal(result.scrollTop, 180);
  assert.equal(translateBtn.disabled, false);
  assert.equal(translateBtn.textContent, '停止');
  assert.equal(translateBtn.classList.contains('is-stop'), true);
  assert.equal(status.classList.contains('is-loading'), true);
  assert.equal(resultField.classList.contains('is-streaming'), true);

  await win.tick(120);
  assert.equal(result.value, '第一段\n第二段');
  assert.equal(status.textContent, '');
  assert.equal(status.classList.contains('error'), false);
  assert.equal(status.classList.contains('is-loading'), false);
  assert.equal(resultField.classList.contains('is-streaming'), false);
  assert.equal(translateBtn.textContent, '翻译');
  assert.equal(translateBtn.classList.contains('is-stop'), false);
  assert.equal(copyResult.disabled, false);
  assert.equal(copyBoth.disabled, false);
  assert.deepEqual(cancelled, ['tok-1']);

  copyResult.click();
  await settle();
  assert.equal(copyResult.classList.contains('is-success'), true);
  assert.equal(status.textContent, '已复制译文');
  await win.tick(2000);
  assert.equal(status.textContent, '');

  copyBoth.click();
  await settle();
  assert.equal(copyBoth.classList.contains('is-success'), true);
  assert.deepEqual(clipboard, [
    '第一段\n第二段',
    '原文：\nHello\nWorld\n\n译文：\n第一段\n第二段',
  ]);
});

test('translate window can stop in-flight translations via stop button, Escape, clear, and hidden state', async () => {
  const cancelled = [];
  let tokenSeq = 0;
  let pendingPoll = null;
  const win = page('translate', {
    bridge: {
      modelSettings: async () => ({ mode: 'local', baseUrl: 'http://127.0.0.1:11434/v1', model: 'm', hasApiKey: false, revision: 'rev-1' }),
      startTranslation: async () => `tok-${++tokenSeq}`,
      pollTranslation: () => {
        pendingPoll = deferred();
        return pendingPoll.promise;
      },
      cancelTranslation: async (token) => { cancelled.push(token); },
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const result = win.document.getElementById('result');
  const translateBtn = win.document.getElementById('translate');
  const clearBtn = win.document.getElementById('clear');
  const status = win.document.getElementById('status');

  source.value = 'One';
  translateBtn.click();
  await settle();
  assert.equal(translateBtn.textContent, '停止');
  translateBtn.click();
  pendingPoll.resolve({ text: '一', done: true, error: '' });
  await settle();
  assert.deepEqual(cancelled, ['tok-1']);
  assert.equal(translateBtn.textContent, '翻译');
  assert.equal(status.textContent, '已停止翻译');
  assert.equal(result.value, '');

  translateBtn.click();
  await settle();
  win.document.fire('keydown', { key: 'Escape' });
  pendingPoll.resolve({ text: '二', done: true, error: '' });
  await settle();
  assert.deepEqual(cancelled, ['tok-1', 'tok-2']);
  assert.equal(status.textContent, '已停止翻译');

  translateBtn.click();
  await settle();
  clearBtn.click();
  pendingPoll.resolve({ text: '三', done: true, error: '' });
  await settle();
  assert.deepEqual(cancelled, ['tok-1', 'tok-2', 'tok-3']);
  assert.equal(source.value, '');
  assert.equal(result.value, '');
  assert.equal(status.textContent, '');

  source.value = 'Four';
  translateBtn.click();
  await settle();
  win.document.hidden = true;
  win.document.fire('visibilitychange');
  pendingPoll.resolve({ text: '四', done: true, error: '' });
  await settle();
  assert.deepEqual(cancelled, ['tok-1', 'tok-2', 'tok-3', 'tok-4']);
  assert.equal(translateBtn.textContent, '翻译');
});

test('empty copy guards block clipboard writes when no translation exists', async () => {
  const clipboard = [];
  const win = page('translate', {
    bridge: {
      modelSettings: async () => ({ mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' }),
      copyRecognizedText: async (text) => { clipboard.push(text); },
    },
  });
  await settle();

  const copyResult = win.document.getElementById('copy-result');
  const copyBoth = win.document.getElementById('copy-both');
  const status = win.document.getElementById('status');

  assert.equal(copyResult.disabled, true);
  assert.equal(copyBoth.disabled, true);
  copyResult.fire('click');
  await settle();
  assert.equal(status.textContent, '还没有译文');
  assert.equal(status.classList.contains('error'), true);
  copyBoth.fire('click');
  await settle();
  assert.deepEqual(clipboard, []);
});

test('swapping or changing target language with an existing result re-translates automatically', async () => {
  const started = [];
  const replies = new Map([
    ['zh-Hans', '你好'],
    ['en', 'Hello'],
    ['ja', 'こんにちは'],
  ]);
  const win = page('translate', {
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      startTranslation: async (_rev, text, target, source, style) => {
        started.push({ text, target, source, style });
        return target;
      },
      pollTranslation: async (token) => ({ text: replies.get(token) ?? '', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const sourceLang = win.document.getElementById('source-lang');
  const target = win.document.getElementById('target');
  const result = win.document.getElementById('result');

  sourceLang.value = 'auto';
  source.value = 'Hello';
  win.document.getElementById('translate').click();
  await settle();
  assert.equal(result.value, '你好');

  win.document.getElementById('swap').click();
  await settle();
  assert.equal(sourceLang.value, 'zh-Hans');
  assert.equal(target.value, 'en');
  assert.equal(source.value, '你好');
  assert.equal(result.value, 'Hello');

  target.value = 'ja';
  target.fire('change');
  await settle();
  assert.equal(result.value, 'こんにちは');
  assert.deepEqual(started, [
    { text: 'Hello', target: 'zh-Hans', source: 'auto', style: 'standard' },
    { text: '你好', target: 'en', source: 'zh-Hans', style: 'standard' },
    { text: '你好', target: 'ja', source: 'zh-Hans', style: 'standard' },
  ]);
});

test('translate window surfaces modelSettings errors and resets to ready state', async () => {
  const win = page('translate', {
    bridge: {
      modelSettings: async () => { throw new Error('没有可识别的截图选区'); },
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const translateBtn = win.document.getElementById('translate');
  const status = win.document.getElementById('status');

  source.value = 'Test text';
  translateBtn.click();
  await settle();

  assert.equal(status.textContent, '没有可识别的截图选区');
  assert.equal(status.classList.contains('error'), true);
  assert.equal(translateBtn.disabled, false);
  assert.equal(translateBtn.textContent, '翻译');
});

test('unconfigured model shows settings shortcut and main window navigates on focus', async () => {
  let openedMain = 0;
  const storage = {};
  const win = page('translate', {
    storage,
    bridge: {
      modelSettings: async () => ({ mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' }),
      showMainWindow: async () => { openedMain += 1; },
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const translateBtn = win.document.getElementById('translate');
  const openSettings = win.document.getElementById('open-settings');
  const status = win.document.getElementById('status');

  assert.equal(openSettings.hidden, true);
  source.value = 'Test text';
  translateBtn.click();
  await settle();

  assert.equal(status.textContent, '先在偏好设置 → 识别与翻译中配置模型服务');
  assert.equal(status.classList.contains('error'), true);
  assert.equal(openSettings.hidden, false);

  openSettings.click();
  assert.equal(openedMain, 1);
  assert.equal(storage['pinstill.openSettingsCategory'], 'recognition');

  const main = page('index', { storage });
  main.window.fire('focus');
  await settle();
  const recognitionTab = main.document.querySelector('[data-settings-cat="recognition"]');
  assert.ok(recognitionTab);
  assert.equal(recognitionTab.getAttribute('aria-current'), 'page');
  assert.equal(storage['pinstill.openSettingsCategory'], '');
});

test('translation style is only selectable when an LLM service is configured', async () => {
  let currentSettings = { mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' };
  const started = [];
  const storage = { 'pinstill.translateStyle': 'polish' };
  const win = page('translate', {
    storage,
    bridge: {
      modelSettings: async () => currentSettings,
      startTranslation: async (_rev, text, target, source, style) => {
        started.push({ text, target, source, style });
        return 'tok-style';
      },
      pollTranslation: async () => ({ text: '技术译文', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const styleSelect = win.document.getElementById('style');
  const source = win.document.getElementById('source');
  const result = win.document.getElementById('result');

  assert.equal(styleSelect.disabled, true);
  assert.equal(styleSelect.value, 'standard');

  // Programmatic changes while model is unavailable are ignored.
  styleSelect.value = 'technical';
  styleSelect.fire('change');
  assert.equal(styleSelect.value, 'standard');
  assert.equal(storage['pinstill.translateStyle'], 'polish');

  // Enabling an LLM model unlocks the selector and restores the saved style.
  currentSettings = { mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-2' };
  win.window.fire('focus');
  await settle();
  assert.equal(styleSelect.disabled, false);
  assert.equal(styleSelect.value, 'polish');

  source.value = 'API rate limit';
  win.document.getElementById('translate').click();
  await settle();
  assert.equal(result.value, '技术译文');

  // Switching style with an existing result persists it and re-translates.
  styleSelect.value = 'technical';
  styleSelect.fire('change');
  await settle();
  assert.equal(storage['pinstill.translateStyle'], 'technical');
  assert.deepEqual(started, [
    { text: 'API rate limit', target: 'zh-Hans', source: '', style: 'polish' },
    { text: 'API rate limit', target: 'zh-Hans', source: '', style: 'technical' },
  ]);

  // Disabling the model service locks the selector back to standard.
  currentSettings = { mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' };
  win.window.fire('focus');
  await settle();
  assert.equal(styleSelect.disabled, true);
  assert.equal(styleSelect.value, 'standard');
});

test('mergeBrokenLines and merge-lines button clean soft wraps while keeping structure', async () => {
  const win = page('translate', {
    bridge: {
      modelSettings: async () => ({ mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' }),
    },
  });
  await settle();

  const { mergeBrokenLines } = win.module('./translate-text');
  assert.equal(
    mergeBrokenLines('This is an implemen-\ntation across\nlines.\n\n中文第一行\n中文第二行'),
    'This is an implementation across lines.\n\n中文第一行中文第二行',
  );
  assert.equal(
    mergeBrokenLines('# Title\nFirst item line\ncontinued\n- Bullet one\nmore bullet\n```ts\nconst a = 1;\nconst b = 2;\n```'),
    '# Title\nFirst item line continued\n- Bullet one more bullet\n```ts\nconst a = 1;\nconst b = 2;\n```',
  );

  const source = win.document.getElementById('source');
  const mergeBtn = win.document.getElementById('merge-lines');
  const status = win.document.getElementById('status');

  source.value = 'Single line';
  source.fire('input');
  assert.equal(mergeBtn.disabled, true);

  source.value = 'Line one\nline two';
  source.fire('input');
  assert.equal(mergeBtn.disabled, false);

  mergeBtn.click();
  assert.equal(source.value, 'Line one line two');
  assert.equal(mergeBtn.disabled, true);
  assert.equal(mergeBtn.classList.contains('is-success'), true);
  assert.equal(status.textContent, '已合并断行');
  await win.tick(2000);
  assert.equal(status.textContent, '');
});

test('pin toggle and model preset selector work in the translate titlebar', async () => {
  const pinCalls = [];
  const selectedPresets = [];
  const storage = { 'pinstill.translatePinned': '1' };
  const presets = [
    { id: 'p1', name: 'DeepSeek', mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'ds', hasApiKey: true, revision: 'r1' },
    { id: 'p2', name: '本地 Ollama', mode: 'local', baseUrl: 'http://127.0.0.1:11434/v1', model: 'qwen', hasApiKey: false, revision: 'r2' },
  ];

  const win = page('translate', {
    storage,
    bridge: {
      modelSettings: async () => ({ ...presets[0], activeId: 'p1', presets }),
      setTranslateAlwaysOnTop: async (pinned) => {
        pinCalls.push(pinned);
        return pinned;
      },
      selectModelPreset: async (id) => {
        selectedPresets.push(id);
        const found = presets.find((p) => p.id === id);
        return { ...found, activeId: found.id, presets };
      },
    },
  });
  await settle();

  const pinBtn = win.document.getElementById('pin-window');
  const presetSelect = win.document.getElementById('model-preset');
  const status = win.document.getElementById('status');

  assert.deepEqual(pinCalls, [true]);
  assert.equal(pinBtn.classList.contains('is-pinned'), true);
  assert.equal(pinBtn.getAttribute('aria-pressed'), 'true');
  assert.equal(presetSelect.hidden, false);
  assert.equal(presetSelect.value, 'p1');

  pinBtn.click();
  await settle();
  assert.deepEqual(pinCalls, [true, false]);
  assert.equal(pinBtn.classList.contains('is-pinned'), false);
  assert.equal(storage['pinstill.translatePinned'], '0');
  assert.equal(status.textContent, '已取消置顶');

  presetSelect.value = 'p2';
  presetSelect.fire('change');
  await settle();
  assert.deepEqual(selectedPresets, ['p2']);
  assert.equal(status.textContent, '已切换模型：本地 Ollama');
});

test('pasting or dropping an image runs local OCR and translates automatically', async () => {
  const ocrCalls = [];
  const started = [];
  let ocrReply = 'Recognized text';

  const win = page('translate', {
    storage: { 'pinstill.preferences': JSON.stringify({ ocrLanguage: 'English', translateTo: 'zh-Hans' }) },
    globals: {
      FileReader: class {
        readAsDataURL() {
          this.result = 'data:image/png;base64,ZmFrZS1wbmc=';
          this.onload();
        }
      },
    },
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      recognizeTranslateImage: async (language, pngBase64) => {
        ocrCalls.push([language, pngBase64]);
        return ocrReply;
      },
      startTranslation: async (_rev, text, target, source, style) => {
        started.push([text, target, source, style]);
        return 'tok-ocr';
      },
      pollTranslation: async () => ({ text: '识别到的文本', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const sourceField = win.document.getElementById('source-field');
  const result = win.document.getElementById('result');
  const status = win.document.getElementById('status');
  const pngFile = new Blob(['png'], { type: 'image/png' });

  // Paste PNG image -> OCR -> auto-translate.
  win.document.fire('paste', {
    clipboardData: { items: [{ kind: 'file', type: 'image/png', getAsFile: () => pngFile }] },
  });
  await settle();
  assert.deepEqual(ocrCalls, [['English', 'ZmFrZS1wbmc=']]);
  assert.equal(source.value, 'Recognized text');
  assert.equal(result.value, '识别到的文本');
  assert.deepEqual(started, [['Recognized text', 'zh-Hans', '', 'standard']]);

  // Dragenter sets dragover state; drop clears it and runs OCR -> empty text shows error without translating.
  ocrReply = '   ';
  win.document.fire('dragenter', { dataTransfer: { types: ['Files'] } });
  assert.equal(sourceField.classList.contains('is-dragover'), true);
  win.document.fire('drop', { dataTransfer: { files: [pngFile], types: ['Files'] } });
  await settle();
  assert.equal(sourceField.classList.contains('is-dragover'), false);
  assert.equal(status.textContent, '图片中没有识别到文字');
  assert.equal(status.classList.contains('error'), true);
  assert.equal(started.length, 1);
});

test('character counts, auto-merge paste/OCR, and dictionary hint work together', async () => {
  const started = [];
  const storage = { 'pinstill.preferences': JSON.stringify({ ocrLanguage: 'English', translateTo: 'zh-Hans' }) };
  const statusLog = [];
  let pendingPoll = null;
  const win = page('translate', {
    storage,
    globals: {
      FileReader: class {
        readAsDataURL() {
          this.result = 'data:image/png;base64,ZmFrZS1wbmc=';
          this.onload();
        }
      },
    },
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      recognizeTranslateImage: async () => 'First OCR\nline',
      startTranslation: async (_rev, text, target, source, style, glossary) => {
        started.push({ text, target, source, style, glossary });
        statusLog.push(win.document.getElementById('status').textContent);
        return 'tok-dict';
      },
      pollTranslation: () => {
        pendingPoll = deferred();
        return pendingPoll.promise;
      },
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const sourceCount = win.document.getElementById('source-count');
  const result = win.document.getElementById('result');
  const resultCount = win.document.getElementById('result-count');
  const autoMergeBtn = win.document.getElementById('auto-merge');
  const status = win.document.getElementById('status');

  assert.equal(sourceCount.textContent, '');
  assert.equal(resultCount.textContent, '');

  // Enable auto-merge and paste wrapped text into source.
  autoMergeBtn.click();
  assert.equal(autoMergeBtn.classList.contains('is-active'), true);
  assert.equal(storage['pinstill.translateAutoMerge'], '1');

  win.document.fire('paste', {
    target: source,
    clipboardData: { getData: (type) => (type === 'text/plain' ? 'Broken\nline text' : '') },
  });
  assert.equal(source.value, 'Broken line text');
  assert.equal(sourceCount.textContent, '16 字符');
  assert.equal(status.textContent, '已自动合并断行');

  // Paste image while auto-merge is active -> OCR lines merged before translating.
  const pngFile = new Blob(['png'], { type: 'image/png' });
  win.document.fire('paste', {
    clipboardData: { items: [{ kind: 'file', type: 'image/png', getAsFile: () => pngFile }] },
  });
  await settle();
  assert.equal(source.value, 'First OCR line');
  pendingPoll.resolve({ text: '第一行 OCR', done: true, error: '' });
  await settle();
  assert.equal(resultCount.textContent, '7 字符');

  // Dictionary hint triggers for single word in standard mode.
  source.value = 'ephemeral';
  source.fire('input');
  assert.equal(sourceCount.textContent, '9 字符');
  win.document.getElementById('translate').click();
  await settle();
  assert.equal(statusLog.at(-1), '正在查询词典（简体中文）…');
  pendingPoll.resolve({ text: '短暂的', done: true, error: '' });
  await settle();
});

test('glossary settings manage terms and translate window forwards glossary to startTranslation', async () => {
  const storage = {};
  const main = page('index', { storage });
  main.document.querySelector('[data-view="settings"]').click();
  main.document.querySelector('[data-settings-cat="recognition"]').click();
  await settle();

  const glossaryForm = main.document.getElementById('glossaryForm');
  const srcInput = main.document.getElementById('glossarySource');
  const tgtInput = main.document.getElementById('glossaryTarget');
  assert.ok(glossaryForm);

  srcInput.value = 'Pinstill';
  tgtInput.value = '';
  glossaryForm.fire('submit');

  srcInput.value = 'rate limit';
  tgtInput.value = '速率限制';
  glossaryForm.fire('submit');

  assert.equal(main.document.querySelectorAll('.glossary-item').length, 2);

  // Remove one term via delete button.
  main.document.querySelectorAll('.glossary-item button')[1].click();
  assert.equal(main.document.querySelectorAll('.glossary-item').length, 1);

  // Translate window loads persisted glossary and passes it to startTranslation.
  const started = [];
  const win = page('translate', {
    storage,
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      startTranslation: async (_rev, _text, _target, _source, _style, glossary) => {
        started.push(glossary);
        return 'tok-g';
      },
      pollTranslation: async () => ({ text: 'Pinstill 截图工具', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  win.document.getElementById('source').value = 'Pinstill screenshot tool';
  win.document.getElementById('translate').click();
  await settle();
  assert.deepEqual(JSON.parse(JSON.stringify(started)), [[{ source: 'Pinstill', target: '' }]]);
});

test('translation history records entries, supports starring/filtering/restoring/clearing, and respects record toggle', async () => {
  const storage = {};
  const replies = new Map([
    ['First sentence', '第一句'],
    ['Second sentence', '第二句'],
    ['Third sentence', '第三句'],
  ]);
  const win = page('translate', {
    storage,
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      startTranslation: async (_rev, text) => text,
      pollTranslation: async (token) => ({ text: replies.get(token) ?? '', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const result = win.document.getElementById('result');
  const translateBtn = win.document.getElementById('translate');
  const starBtn = win.document.getElementById('star-result');
  const historyToggle = win.document.getElementById('history-toggle');
  const historyDrawer = win.document.getElementById('history-drawer');

  // Translate first sentence and star it.
  source.value = 'First sentence';
  translateBtn.click();
  await settle();
  assert.equal(starBtn.disabled, false);
  starBtn.click();
  assert.equal(starBtn.classList.contains('is-starred'), true);

  // Translate second sentence (left unstarred).
  source.value = 'Second sentence';
  source.fire('input');
  translateBtn.click();
  await settle();

  // Open history drawer and inspect both items.
  historyToggle.click();
  assert.equal(historyDrawer.hidden, false);
  assert.equal(win.document.querySelectorAll('.history-item').length, 2);

  // Switch to Starred tab and verify only first item shows.
  win.document.getElementById('history-tab-starred').click();
  assert.equal(win.document.querySelectorAll('.history-item').length, 1);

  // Search within All tab.
  win.document.getElementById('history-tab-all').click();
  const search = win.document.getElementById('history-search');
  search.value = '第二句';
  search.fire('input');
  assert.equal(win.document.querySelectorAll('.history-item').length, 1);

  // Clear unstarred removes Second sentence while keeping starred First sentence.
  search.value = '';
  search.fire('input');
  win.document.getElementById('history-clear').click();
  const items = win.document.querySelectorAll('.history-item');
  assert.equal(items.length, 1);

  // Clicking the remaining item restores it and closes drawer.
  items[0].click();
  assert.equal(source.value, 'First sentence');
  assert.equal(result.value, '第一句');
  assert.equal(starBtn.classList.contains('is-starred'), true);
  assert.equal(historyDrawer.hidden, true);

  // Pausing recording prevents adding new unstarred items.
  historyToggle.click();
  win.document.getElementById('history-record-toggle').click();
  assert.equal(storage['pinstill.translateHistoryEnabled'], '0');
  win.document.getElementById('history-close').click();
  source.value = 'Third sentence';
  source.fire('input');
  translateBtn.click();
  await settle();
  historyToggle.click();
  assert.equal(win.document.querySelectorAll('.history-item').length, 1);
});

test('speechLocaleFor and offline speech controls speak, toggle stop, switch panes, and cancel on Escape/hide', async () => {
  const spoken = [];
  let cancelled = 0;
  class MockUtterance {
    constructor(text) {
      this.text = text;
      this.lang = '';
      this.voice = null;
      this.onend = null;
      this.onerror = null;
    }
  }
  const win = page('translate', {
    globals: {
      SpeechSynthesisUtterance: MockUtterance,
      speechSynthesis: {
        getVoices: () => [
          { lang: 'en_US', localService: true, name: 'Albert' },
          { lang: 'en-US', localService: true, name: 'Eddy (英语（美国）)' },
          { lang: 'en-US', localService: true, name: 'Samantha' },
          { lang: 'zh_CN', localService: true, name: 'Eddy (中文（中国大陆）)' },
          { lang: 'zh-CN', localService: true, name: 'Grandma (中文（中国大陆）)' },
          { lang: 'zh-CN', localService: true, name: 'Tingting' },
          { lang: 'zh-CN', localService: true, name: 'Tingting (Enhanced)' },
        ],
        speak: (utterance) => { spoken.push(utterance); },
        cancel: () => { cancelled += 1; },
      },
    },
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'm', hasApiKey: true, revision: 'rev-1' }),
      startTranslation: async () => 'tok-tts',
      pollTranslation: async () => ({ text: '你好世界', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const { speechLocaleFor } = win.module('./translate-speech');
  assert.equal(speechLocaleFor('zh-Hans', 'x'), 'zh-CN');
  assert.equal(speechLocaleFor('ja', 'x'), 'ja-JP');
  assert.equal(speechLocaleFor('auto', 'Hello world'), 'en-US');
  assert.equal(speechLocaleFor('auto', '你好世界'), 'zh-CN');
  assert.equal(speechLocaleFor('auto', 'こんにちは'), 'ja-JP');
  assert.equal(speechLocaleFor('auto', '안녕하세요'), 'ko-KR');
  assert.equal(speechLocaleFor('auto', 'Привет'), 'ru-RU');

  const source = win.document.getElementById('source');
  const speakSource = win.document.getElementById('speak-source');
  const speakResult = win.document.getElementById('speak-result');

  assert.equal(speakSource.disabled, true);
  assert.equal(speakResult.disabled, true);

  source.value = 'Hello world';
  source.fire('input');
  assert.equal(speakSource.disabled, false);

  // Speak source -> button switches to "停止".
  speakSource.click();
  assert.equal(spoken.length, 1);
  assert.equal(spoken[0].text, 'Hello world');
  assert.equal(spoken[0].lang, 'en-US');
  assert.equal(spoken[0].voice?.name, 'Samantha');
  assert.equal(speakSource.textContent, '停止');
  assert.equal(speakSource.classList.contains('is-speaking'), true);

  // Clicking again stops playback.
  speakSource.click();
  assert.equal(speakSource.textContent, '朗读');
  assert.equal(speakSource.classList.contains('is-speaking'), false);
  const cancelAfterFirstStop = cancelled;
  assert.ok(cancelAfterFirstStop >= 2);

  // Translate, then speak result and finish via onend.
  win.document.getElementById('translate').click();
  await settle();
  assert.equal(speakResult.disabled, false);
  speakResult.click();
  assert.equal(spoken.length, 2);
  assert.equal(spoken[1].text, '你好世界');
  assert.equal(spoken[1].lang, 'zh-CN');
  assert.equal(spoken[1].voice?.name, 'Tingting (Enhanced)');
  assert.equal(speakResult.textContent, '停止');

  spoken[1].onend();
  assert.equal(speakResult.textContent, '朗读');

  // Start speaking source again and stop via Escape, then test visibilitychange.
  speakSource.click();
  assert.equal(speakSource.textContent, '停止');
  win.document.fire('keydown', { key: 'Escape' });
  assert.equal(speakSource.textContent, '朗读');

  speakResult.click();
  assert.equal(speakResult.textContent, '停止');
  win.document.hidden = true;
  win.document.fire('visibilitychange');
  assert.equal(speakResult.textContent, '朗读');
});

test('remote TTS provider (DeepSeek / dedicated TTS) synthesizes, plays audio, and supports stop/Escape', async () => {
  const synthesized = [];
  const instances = [];
  class MockAudio {
    constructor(src) {
      this.src = src;
      this.paused = false;
      this.onended = null;
      this.onerror = null;
      instances.push(this);
    }
    play() {
      return Promise.resolve();
    }
    pause() {
      this.paused = true;
    }
  }

  const win = page('translate', {
    globals: { Audio: MockAudio },
    bridge: {
      modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://api.deepseek.com/v1', model: 'deepseek-flash', hasApiKey: true, revision: 'rev-1' }),
      ttsSettings: async () => ({ provider: 'deepseek', baseUrl: 'https://api.deepseek.com/v1', model: '', voice: 'stella', hasApiKey: true, revision: 'tts-rev-1' }),
      synthesizeSpeech: async (revision, text) => {
        synthesized.push([revision, text]);
        return { mimeType: 'audio/wav', dataBase64: 'UklGRg==' };
      },
      startTranslation: async () => 'tok-remote-tts',
      pollTranslation: async () => ({ text: '你好，这是远端语音测试', done: true, error: '' }),
      cancelTranslation: async () => {},
    },
  });
  await settle();

  const source = win.document.getElementById('source');
  const speakResult = win.document.getElementById('speak-result');
  const status = win.document.getElementById('status');

  source.value = 'Remote TTS test';
  win.document.getElementById('translate').click();
  await settle();

  assert.equal(speakResult.disabled, false);
  speakResult.click();
  await settle();

  assert.deepEqual(synthesized, [['tts-rev-1', '你好，这是远端语音测试']]);
  assert.equal(instances.length, 1);
  assert.equal(instances[0].src, 'data:audio/wav;base64,UklGRg==');
  assert.equal(speakResult.textContent, '停止');
  assert.equal(status.textContent, '');

  // Pressing Escape stops audio playback cleanly.
  win.document.fire('keydown', { key: 'Escape' });
  assert.equal(instances[0].paused, true);
  assert.equal(speakResult.textContent, '朗读');
});

test('editing the source invalidates the previous translation and cancels stale updates', async () => {
  const late = deferred();
  const cancelled = [];
  const win = page('translate', { bridge: {
    modelSettings: async () => ({ mode: 'cloud', model: 'm', revision: 'rev-1' }),
    startTranslation: async (_rev, text) => text,
    pollTranslation: async (token) => token === 'Old source' ? { text: '旧译文', done: true } : late.promise,
    cancelTranslation: async (token) => { cancelled.push(token); },
  } });
  await settle();
  const source = win.document.getElementById('source');
  const result = win.document.getElementById('result');
  const button = win.document.getElementById('translate');
  source.value = 'Old source';
  button.click();
  await settle();
  assert.equal(result.value, '旧译文');
  source.value = 'New source';
  source.fire('input');
  assert.equal(result.value, '');
  assert.equal(win.document.getElementById('star-result').disabled, true);
  assert.equal(win.document.getElementById('copy-both').disabled, true);
  button.click();
  await settle();
  source.value = 'Changed again';
  source.fire('input');
  late.resolve({ text: '迟到的译文', done: true });
  await settle();
  assert.equal(result.value, '');
  assert.ok(cancelled.includes('New source'));
});

test('translation history keeps the same spelling in distinct source languages', async () => {
  const storage = {};
  const win = page('translate', { storage, bridge: {
    modelSettings: async () => ({ mode: 'cloud', model: 'm', revision: 'rev-1' }),
    startTranslation: async (_rev, _text, _target, source) => source,
    pollTranslation: async (token) => ({ text: token === 'en' ? '礼物' : '毒药', done: true }),
    cancelTranslation: async () => {},
  } });
  await settle();
  const sourceLang = win.document.getElementById('source-lang');
  win.document.getElementById('source').value = 'Gift';
  sourceLang.value = 'en';
  win.document.getElementById('translate').click();
  await settle();
  win.document.getElementById('star-result').click();
  sourceLang.value = 'de';
  sourceLang.fire('change');
  win.document.getElementById('translate').click();
  await settle();
  const history = JSON.parse(storage['pinstill.translateHistory']);
  assert.deepEqual(history.map(({ sourceLang, result, starred }) => [sourceLang, result, starred]),
    [['de', '毒药', false], ['en', '礼物', true]]);
});
