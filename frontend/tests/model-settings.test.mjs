import assert from 'node:assert/strict';
import test from 'node:test';
import { page, settle } from './harness.mjs';

test('model preferences save credentials only through the native form and clear the input', async () => {
  const submissions = [];
  const app = page('index', { bridge: {
    modelSettings: async () => ({ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'old-model', hasApiKey: true, revision: 'old' }),
    saveModelSettings: async (input) => {
      submissions.push(JSON.parse(JSON.stringify(input)));
      return { mode: input.mode, baseUrl: input.baseUrl, model: input.model, hasApiKey: true, revision: 'new' };
    },
  } });
  app.document.querySelector('[data-view="settings"]').click();
  app.document.querySelector('[data-settings-cat="recognition"]').click();
  await settle();
  const form = app.document.getElementById('modelSettingsForm');
  assert.ok(form);
  const keyField = app.document.getElementById('modelApiKey');
  assert.equal(keyField.type, 'password');
  assert.equal(keyField.value, '');
  assert.equal(app.document.getElementById('modelBaseUrl').value, 'https://models.example.test/v1');
  app.document.getElementById('modelName').value = 'new-model';
  keyField.value = 'stub-authentication-value';
  form.fire('submit');
  await settle();
  assert.deepEqual(submissions, [{ mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'new-model', apiKey: 'stub-authentication-value' }]);
  assert.equal(keyField.value, '');
  assert.ok(!JSON.stringify(app.storage).includes('stub-authentication-value'));
});

test('model preferences support creating, switching, testing, and deleting presets', async () => {
  const tested = [];
  const deleted = [];
  let state = {
    mode: 'cloud',
    baseUrl: 'https://models.example.test/v1',
    model: 'cloud-model',
    hasApiKey: true,
    revision: 'rev-1',
    activeId: 'p1',
    presets: [
      { id: 'p1', name: 'DeepSeek', mode: 'cloud', baseUrl: 'https://models.example.test/v1', model: 'cloud-model', hasApiKey: true, revision: 'rev-1' },
    ],
  };

  const app = page('index', {
    bridge: {
      modelSettings: async () => state,
      saveModelPreset: async (input) => {
        const next = {
          id: input.id || 'p2',
          name: input.name || input.model,
          mode: input.mode,
          baseUrl: input.baseUrl,
          model: input.model,
          hasApiKey: Boolean(input.apiKey),
          revision: 'rev-2',
        };
        const presets = [...state.presets.filter((p) => p.id !== next.id), next];
        state = { ...next, activeId: next.id, presets };
        return state;
      },
      selectModelPreset: async (id) => {
        const found = state.presets.find((p) => p.id === id);
        state = found ? { ...found, activeId: found.id, presets: state.presets } : { mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '', activeId: '', presets: state.presets };
        return state;
      },
      deleteModelPreset: async (id) => {
        deleted.push(id);
        const presets = state.presets.filter((p) => p.id !== id);
        const next = presets[0];
        state = next ? { ...next, activeId: next.id, presets } : { mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' };
        return state;
      },
      testModelConnection: async (input) => {
        tested.push(input);
        if (input.model === 'bad-model') throw new Error('模型或接口不存在');
        return '连接成功（42 ms）';
      },
    },
  });

  app.document.querySelector('[data-view="settings"]').click();
  app.document.querySelector('[data-settings-cat="recognition"]').click();
  await settle();

  const presetSelect = app.document.getElementById('modelPresetSelect');
  const addBtn = app.document.getElementById('modelPresetAdd');
  const deleteBtn = app.document.getElementById('modelPresetDelete');
  const testBtn = app.document.getElementById('modelTestConnection');
  const form = app.document.getElementById('modelSettingsForm');
  const status = app.document.querySelector('.model-settings-status');

  assert.equal(presetSelect.value, 'p1');
  assert.equal(app.document.getElementById('modelPresetName').value, 'DeepSeek');

  // Test connection success & failure.
  testBtn.click();
  await settle();
  assert.equal(tested.length, 1);
  assert.equal(status.textContent, '连接成功（42 ms）');
  assert.equal(status.classList.contains('success'), true);

  app.document.getElementById('modelName').value = 'bad-model';
  testBtn.click();
  await settle();
  assert.equal(status.textContent, '模型或接口不存在');
  assert.equal(status.classList.contains('error'), true);

  // Add a second local preset.
  addBtn.click();
  app.document.getElementById('modelPresetName').value = '本地 Ollama';
  app.document.getElementById('modelMode').value = 'local';
  app.document.getElementById('modelMode').fire('change');
  app.document.getElementById('modelBaseUrl').value = 'http://127.0.0.1:11434';
  app.document.getElementById('modelName').value = 'qwen2.5';
  form.fire('submit');
  await settle();
  assert.equal(presetSelect.value, 'p2');

  // Switch back to p1, then delete p1.
  presetSelect.value = 'p1';
  presetSelect.fire('change');
  await settle();
  assert.equal(app.document.getElementById('modelName').value, 'cloud-model');

  deleteBtn.click();
  await settle();
  assert.deepEqual(deleted, ['p1']);
  assert.equal(presetSelect.value, 'p2');
});

test('tts preferences support deepseek, openai-compatible models, and test preview', async () => {
  const savedCalls = [];
  const testCalls = [];
  const played = [];
  class MockAudio {
    constructor(src) {
      this.src = src;
      this.onended = null;
      this.onerror = null;
    }
    play() {
      played.push(this.src);
      return Promise.resolve();
    }
    pause() {}
  }

  const app = page('index', {
    globals: { Audio: MockAudio },
    bridge: {
      modelSettings: async () => ({ mode: 'off', baseUrl: '', model: '', hasApiKey: false, revision: '' }),
      ttsSettings: async () => ({ provider: 'system', baseUrl: '', model: '', voice: '', hasApiKey: false, revision: '' }),
      saveTTSSettings: async (input) => {
        savedCalls.push(JSON.parse(JSON.stringify(input)));
        return {
          provider: input.provider,
          baseUrl: input.baseUrl,
          model: input.model,
          voice: input.voice || (input.provider === 'deepseek' ? 'stella' : 'alloy'),
          hasApiKey: true,
          revision: 'tts-rev-1',
        };
      },
      testTTS: async (input) => {
        testCalls.push(JSON.parse(JSON.stringify(input)));
        return { mimeType: 'audio/wav', dataBase64: 'UklGRg==' };
      },
    },
  });

  app.document.querySelector('[data-view="settings"]').click();
  app.document.querySelector('[data-settings-cat="recognition"]').click();
  await settle();

  const provider = app.document.getElementById('ttsProvider');
  const base = app.document.getElementById('ttsBaseUrl');
  const model = app.document.getElementById('ttsModel');
  const voice = app.document.getElementById('ttsVoice');
  const key = app.document.getElementById('ttsApiKey');
  const testBtn = app.document.getElementById('ttsTestButton');
  const form = app.document.getElementById('ttsSettingsForm');
  const status = app.document.getElementById('ttsSettingsStatus');

  assert.equal(provider.value, 'system');
  assert.equal(testBtn.disabled, true);

  // Switch to DeepSeek TTS: auto-populates DeepSeek URL and hides model input.
  provider.value = 'deepseek';
  provider.fire('change');
  assert.equal(base.value, 'https://api.deepseek.com/v1');
  assert.equal(testBtn.disabled, false);

  voice.value = 'tide';
  testBtn.click();
  await settle();
  assert.equal(testCalls.length, 1);
  assert.equal(played.length, 1);
  assert.equal(status.textContent, '试听成功');
  assert.equal(status.classList.contains('success'), true);

  // Save OpenAI-compatible TTS model.
  provider.value = 'openai-cloud';
  provider.fire('change');
  base.value = 'https://api.openai.com/v1';
  model.value = 'gpt-4o-mini-tts';
  voice.value = 'coral';
  key.value = 'stub-tts-key';
  form.fire('submit');
  await settle();

  assert.deepEqual(savedCalls, [{
    provider: 'openai-cloud',
    baseUrl: 'https://api.openai.com/v1',
    model: 'gpt-4o-mini-tts',
    voice: 'coral',
    apiKey: 'stub-tts-key',
  }]);
  assert.equal(key.value, '');
  assert.equal(status.textContent, '语音配置已保存在系统钥匙串');
});
