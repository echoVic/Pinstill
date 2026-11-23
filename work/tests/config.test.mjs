import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import test from 'node:test';
import ts from 'typescript';

const repo = resolve(dirname(fileURLToPath(import.meta.url)), '../..');

// Load the real configuration with its public defineConfig consumer. Never run
// the MyGo CLI: importing mygo-cli only returns the object, with no signing.
function config(t, env = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'pinstill-config-test-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  symlinkSync(join(repo, 'node_modules'), join(dir, 'node_modules'), 'dir');
  const source = readFileSync(join(repo, 'mygo.config.ts'), 'utf8');
  writeFileSync(join(dir, 'config.mjs'), ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText);
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', `import config from ${JSON.stringify(pathToFileURL(join(dir, 'config.mjs')).href)}; console.log(JSON.stringify(config));`], {
    encoding: 'utf8', env: { ...process.env, MACOS_SIGNING_IDENTITY: '', MACOS_NOTARY_PROFILE: '', ...env },
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(result.stdout);
}

test('ordinary builds need neither a developer certificate nor a notary profile', (t) => {
  const c = config(t);
  assert.equal(c.macos.signingIdentity, '-');
  assert.equal(c.macos.notarize, undefined);
});

test('explicit release identity and profile enable notarization', (t) => {
  const c = config(t, { MACOS_SIGNING_IDENTITY: 'Developer ID Application: Synthetic Test', MACOS_NOTARY_PROFILE: 'synthetic-profile' });
  assert.equal(c.macos.signingIdentity, 'Developer ID Application: Synthetic Test');
  assert.deepEqual(c.macos.notarize, { keychainProfile: 'synthetic-profile' });
});

test('ad-hoc builds ignore an inherited notary profile', (t) => {
  const c = config(t, { MACOS_SIGNING_IDENTITY: '-', MACOS_NOTARY_PROFILE: 'synthetic-profile' });
  assert.equal(c.macos.notarize, undefined);
});

test('signing alone does not opt into notarization', (t) => {
  const c = config(t, { MACOS_SIGNING_IDENTITY: 'Developer ID Application: Synthetic Test' });
  assert.equal(c.macos.notarize, undefined);
});

test('packaged builds use Pinstill GitHub Releases and pin only a public update key', (context) => {
  const { updates } = config(context);
  assert.equal(updates?.github, 'echoVic/Pinstill');
  const publicKey = Buffer.from(updates.publicKey, 'base64');
  assert.equal(publicKey.length, 32);
  assert.equal(publicKey.toString('base64'), updates.publicKey);
  assert.equal(updates.privateKey, undefined);
});
