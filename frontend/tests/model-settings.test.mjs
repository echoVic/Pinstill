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
