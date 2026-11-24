import fs from 'node:fs';
import vm from 'node:vm';
import path from 'node:path';
import ts from 'typescript';

export function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
export async function settle() {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

// Only DOM/canvas/bridge boundaries are doubled. The complete production TS
// modules execute in a fresh VM; no native app, clipboard, or capture is used.
export function page(name, { bridge = {}, globals = {}, storage = {} } = {}) {
  const listeners = new Map();
  const timers = new Map();
  let timerId = 0;
  class Element {
    constructor(tag = 'div') {
      this.tagName = tag.toUpperCase(); this.children = []; this.dataset = {};
      this.attributes = {}; this.style = { setProperty(k, v) { this[k] = v; } };
      this.hidden = false; this.disabled = false; this.textContent = ''; this.value = '';
      this.events = new Map(); this.className = ''; this.width = 100; this.height = 100;
      this.offsetWidth = 100; this.offsetHeight = 30;
      this.classList = {
        contains: c => this.className.split(/\s+/).includes(c),
        add: (...cs) => { this.className = [...new Set([...this.className.split(/\s+/).filter(Boolean), ...cs])].join(' '); },
        remove: (...cs) => { this.className = this.className.split(/\s+/).filter(c => !cs.includes(c)).join(' '); },
        toggle: (c, force) => { const add = force ?? !this.classList.contains(c); this.classList[add ? 'add' : 'remove'](c); return add; },
      };
    }
    setAttribute(k, v) { this.attributes[k] = String(v); if (k === 'id') this.id = v; if (k === 'class') this.className = v; if (k === 'hidden') this.hidden = true; if (k.startsWith('data-')) this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = v; }
    getAttribute(k) { return k === 'src' ? this.src ?? null : this.attributes[k] ?? null; }
    hasAttribute(k) { return k in this.attributes; }
    removeAttribute(k) { delete this.attributes[k]; }
    append(...children) { for (const c of children) { c.parent = this; this.children.push(c); } }
    replaceChildren(...children) { this.children.forEach(c => c.parent = null); this.children = []; this.append(...children); }
    after(child) { this.parent?.append(child); }
    remove() { if (this.parent) this.parent.children = this.parent.children.filter(c => c !== this); this.parent = null; }
    get isConnected() { return !!this.parent; }
    contains(target) { return this === target || this.children.some(c => c.contains(target)); }
    matches(selector) {
      const id = selector.match(/#([\w-]+)/)?.[1];
      const tag = selector.match(/^[\w-]+/)?.[0];
      const classes = [...selector.matchAll(/\.([\w-]+)/g)].map(m => m[1]);
      const attrs = [...selector.matchAll(/\[([\w-]+)(?:=["']?([^\]"']+)["']?)?\]/g)];
      return (!id || this.id === id) && (!tag || this.tagName.toLowerCase() === tag) && classes.every(c => this.classList.contains(c)) && attrs.every(([, k, v]) => k.startsWith('data-') ? (v === undefined ? this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] !== undefined : this.dataset[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] === v) : this.hasAttribute(k) && (v === undefined || this.getAttribute(k) === v));
    }
    querySelectorAll(selector) {
      if (selector.includes(',')) return [...new Set(selector.split(',').flatMap(s => this.querySelectorAll(s.trim())))];
      const parts = selector.trim().split(/\s+/);
      const descendants = this.children.flatMap(c => [c, ...c.querySelectorAll('*')]);
      return descendants.filter(c => c.matches(parts.at(-1)) && (parts.length === 1 || !!c.parent?.closest(parts.slice(0, -1).join(' '))));
    }
    querySelector(s) { return this.querySelectorAll(s)[0] ?? null; }
    closest(s) { return s.split(',').some(v => this.matches(v.trim())) ? this : this.parent?.closest(s) ?? null; }
    addEventListener(type, cb) { const list = this.events.get(type) ?? []; list.push(cb); this.events.set(type, list); }
    fire(type, event = {}) { const e = { target: this, preventDefault() {}, stopPropagation() {}, stopImmediatePropagation() {}, ...event }; for (const cb of this.events.get(type) ?? []) cb(e); }
    focus() {} select() {} setSelectionRange() {} setPointerCapture() {} click() { if (!this.disabled) this.fire('click'); }
    showModal() { this.open = true; }
    close() { this.open = false; }
    getBoundingClientRect() { return { left: 0, top: 0, width: this.width, height: this.height, bottom: this.height, right: this.width }; }
    getContext() {
      if (this.ctx) return this.ctx;
      const commands = [];
      this.ctx = new Proxy({ commands, measureText: () => ({ width: 30 }), getImageData: () => ({ data: [0, 0, 0, 255] }), createLinearGradient: () => ({ addColorStop() {} }) }, {
        get: (target, key) => key in target ? target[key] : (...args) => { commands.push([key, ...args]); },
      });
      return this.ctx;
    }
    toDataURL() { return 'data:image/png;base64,cGl4ZWxz'; }
  }
  const document = new Element('document');
  document.documentElement = new Element('html'); document.body = new Element('body'); document.head = new Element('head');
  document.append(document.documentElement); document.documentElement.append(document.head, document.body);
  document.createElement = tag => new Element(tag);
  document.getElementById = id => document.querySelector(`#${id}`);
  const html = fs.readFileSync(new URL(`../${name}.html`, import.meta.url), 'utf8').replace(/<style[\s\S]*?<\/style>|<script[\s\S]*?<\/script>/g, '');
  const body = html.match(/<body[^>]*>([\s\S]*?)<\/body>/)[1];
  const stack = [document.body];
  const voids = new Set(['input', 'img', 'meta', 'link', 'br', 'hr']);
  for (const match of body.matchAll(/<(\/?)([\w-]+)([^>]*)>/g)) {
    const [, closing, tag, attrs] = match;
    if (closing) { if (stack.length > 1) stack.pop(); continue; }
    const el = new Element(tag);
    for (const a of attrs.matchAll(/([\w-]+)(?:="([^"]*)")?/g)) el.setAttribute(a[1], a[2] ?? '');
    stack.at(-1).append(el);
    if (!voids.has(tag) && !attrs.endsWith('/')) stack.push(el);
  }
  const window = new Element('window');
  Object.assign(window, { innerWidth: 1200, innerHeight: 800, devicePixelRatio: 1, location: { search: '' },
    setTimeout: (fn, delay) => { timers.set(++timerId, { fn, delay }); return timerId; },
    clearTimeout: id => timers.delete(id), requestAnimationFrame: cb => cb(),
    setInterval: (fn, delay) => { timers.set(++timerId, { fn, delay, interval: true }); return timerId; },
    clearInterval: id => timers.delete(id),
  });
  const bridgeDefaults = { hasNative: true, historyList: async () => [], listArchivedPins: async () => [], applySettings: async () => ({}), permissionGranted: async () => true,
    settings: async () => ({ afterCapture: 'inplace' }), overlayInfo: async () => ({ width: 1200, height: 800 }), overlayReady: async () => {},
  };
  const bridgeMock = new Proxy({ ...bridgeDefaults, ...bridge }, { get: (target, key) => key in target ? target[key] : key.startsWith('on') ? cb => { listeners.set(key, cb); return () => {}; } : () => { throw new Error(`Unexpected bridge call ${key}`); } });
  const context = vm.createContext({ console, URL, URLSearchParams, Blob, AbortController, Error, performance, document, window,
    Element, HTMLElement: Element, HTMLButtonElement: Element, HTMLInputElement: Element, Node: Element,
    navigator: { platform: 'MacIntel', userAgent: 'test' },
    localStorage: { getItem: key => storage[key] ?? null, setItem: (key, value) => storage[key] = value },
    requestAnimationFrame: window.requestAnimationFrame, setTimeout: window.setTimeout,
    fetch: async () => ({ ok: true, blob: async () => new Blob(['image']) }),
    createImageBitmap: async () => ({ close() {} }), ...globals,
  });
  const cache = new Map();
  function requireModule(spec, parent = path.resolve('frontend/src/main.ts')) {
    if (spec === './bridge') return bridgeMock;
    if (spec === './icons') return { refreshPinstillIcons() {} };
    if (spec === 'mygo-runtime') return { isMyGo: () => true, currentWindow: {} };
    if (spec === './mygo') return { events: { pinInit: { on: cb => listeners.set('onPinInit', cb) } } };
    const file = path.resolve(path.dirname(parent), `${spec}.ts`);
    if (cache.has(file)) return cache.get(file);
    const exports = {}; cache.set(file, exports);
    const code = ts.transpileModule(fs.readFileSync(file, 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
    vm.runInContext(`(function(require,exports){${code}\n})`, context)(s => requireModule(s, file), exports);
    return exports;
  }
  context.require = requireModule; context.exports = {};
  const entry = name === 'index' ? 'main' : name;
  const code = ts.transpileModule(fs.readFileSync(new URL(`../src/${entry}.ts`, import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  vm.runInContext(code, context, { filename: `${entry}.ts` });
  return { document, window, timers, storage, context, Element, module: requireModule,
    run: code => vm.runInContext(code, context),
    emit: (key, value) => listeners.get(key)?.(value),
    tick: async delay => { const ready = [...timers].filter(([, t]) => t.delay === delay); for (const [id, t] of ready) { if (!t.interval) timers.delete(id); t.fn(); } await settle(); },
  };
}
