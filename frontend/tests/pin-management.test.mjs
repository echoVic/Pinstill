import test from 'node:test';
import assert from 'node:assert/strict';
import { page, settle } from './harness.mjs';

const presentation = {
  id: 'first', width: 180, height: 110, group: '设计', hidden: false,
  clickThrough: false, rotation: 0, flipX: false, flipY: false,
};

test('a pin group remains manageable with every pin hidden and passing through clicks', async () => {
  let entries = [
    { ...presentation, hidden: true, clickThrough: true, createdAt: '2026-11-23T04:00:00Z', url: 'shot://first' },
    { ...presentation, id: 'second', hidden: true, clickThrough: true, createdAt: '2026-11-23T03:00:00Z', url: 'shot://second' },
  ];
  const manager = page('pins', { bridge: {
    listManagedPins: async () => entries,
    managePinGroup: async (group, action) => {
      entries = entries.map((pin) => pin.group !== group ? pin : {
        ...pin, hidden: action === 'show' ? false : pin.hidden,
        clickThrough: action === 'unlock' ? false : pin.clickThrough,
      });
    },
  } });
  await settle();
  assert.match(manager.document.getElementById('pinsStatus').textContent, /0 张显示中/);
  manager.document.querySelector('[data-control="group:设计:unlock"]').click();
  await settle();
  assert.equal(manager.document.querySelector('[data-control="group:设计:unlock"]').disabled, true);
  manager.document.querySelector('[data-control="group:设计:show"]').click();
  await settle();
  assert.match(manager.document.getElementById('pinsStatus').textContent, /2 张显示中/);
  assert.equal(manager.document.querySelector('[data-control="first:click"]').textContent, '鼠标穿透');
});

test('a restored rotation renders the image and preserves the original copy action', async () => {
  const copied = [];
  const pin = page('pin', { bridge: { copyPin: async (id) => copied.push(id) } });
  pin.emit('onPinInit', {
    id: 'first', url: 'shot://first', width: 110, height: 180,
    presentation: { ...presentation, width: 110, height: 180, rotation: 1, flipY: true },
  });
  assert.equal(pin.document.getElementById('pinWindow').classList.contains('sideways'), true);
  assert.match(pin.document.getElementById('pinImage').style.transform, /rotate\(90deg\) scale\(1, -1\)/);
  pin.window.fire('dblclick', { target: pin.document.getElementById('pinImage') });
  await settle();
  assert.deepEqual(copied, ['first']);
});

test('a native resize is not re-saved from the pin page after the manager may have rotated it', async () => {
  const resized = [];
  const alreadyPersisted = [];
  const pin = page('pin', { bridge: {
    resizePin: async (...args) => resized.push(args),
    updatePinState: async (...args) => alreadyPersisted.push(args),
  }, globals: { performance: { now: () => 1000 } } });
  pin.emit('onPinInit', { id: 'first', url: 'shot://first', width: 180, height: 110, presentation });
  const image = pin.document.getElementById('pinImage');
  image.naturalWidth = 180;
  image.naturalHeight = 110;
  image.onload();
  pin.window.fire('wheel', { deltaY: -120 });
  await pin.tick(70);
  assert.deepEqual(resized, [['first', 198, 121]]);
  await pin.tick(420);
  assert.deepEqual(alreadyPersisted, []);
});
