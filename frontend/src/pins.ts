import { listManagedPins, managePin, managePinGroup, onManagedPinsChanged, type ManagedPin } from "./bridge";

const list = document.getElementById("pinsList")!;
const status = document.getElementById("pinsStatus")!;
const empty = document.getElementById("pinsEmpty")!;
const choices = document.getElementById("pinGroupOptions")!;
const title = document.getElementById("pinsTitle")!;
let request = 0;
let busy = false;
const groupDrafts = new Map<string, string>();
const dateFormat = new Intl.DateTimeFormat("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" });

function element<Tag extends keyof HTMLElementTagNameMap>(tag: Tag, text = ""): HTMLElementTagNameMap[Tag] {
  const node = document.createElement(tag);
  if (text) node.textContent = text;
  return node;
}

function setStatus(text: string, error = false): void {
  status.textContent = text;
  status.dataset.error = error ? "1" : "0";
}

async function act(task: () => Promise<unknown>): Promise<void> {
  if (busy) return;
  busy = true;
  ++request;
  list.setAttribute("aria-busy", "true");
  setStatus("正在保存…");
  try {
    await task();
    await refresh();
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    await refresh();
    setStatus(detail, true);
  } finally {
    busy = false;
    list.setAttribute("aria-busy", "false");
  }
}

function button(label: string, action: () => void, key: string, disabled = false): HTMLButtonElement {
  const result = element("button", label);
  result.type = "button";
  result.disabled = disabled;
  result.dataset.control = key;
  result.addEventListener("click", action);
  return result;
}

function groupEditor(key: string, group: string, groupLabel: string, save: (value: string) => Promise<void>): HTMLFormElement {
  const form = element("form");
  form.className = "rename";
  const input = element("input");
  input.value = groupDrafts.get(key) ?? group;
  input.maxLength = 40;
  input.placeholder = "未分组";
  input.setAttribute("aria-label", groupLabel);
  input.setAttribute("list", "pinGroupOptions");
  input.dataset.control = `${key}:input`;
  input.addEventListener("input", () => groupDrafts.set(key, input.value));
  const submit = element("button", "保存分组");
  submit.type = "submit";
  submit.dataset.control = `${key}:save`;
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    if (input.value.trim() === group) return;
    void act(async () => {
      await save(input.value);
      groupDrafts.delete(key);
    });
  });
  form.append(input, submit);
  return form;
}

function pinRow(pin: ManagedPin): HTMLElement {
  const row = element("article");
  row.className = "pin-row";
  row.dataset.pinId = pin.id;
  row.dataset.hidden = pin.hidden ? "1" : "0";
  const picture = element("div");
  picture.className = `thumb${pin.rotation % 2 ? " sideways" : ""}`;
  const image = element("img");
  image.src = pin.url;
  image.alt = "";
  image.loading = "lazy";
  image.style.transform = `translate(-50%, -50%) rotate(${pin.rotation * 90}deg) scale(${pin.flipX ? -1 : 1}, ${pin.flipY ? -1 : 1})`;
  picture.append(image);
  const detail = element("div");
  detail.className = "detail";
  const date = new Date(pin.createdAt);
  const label = Number.isNaN(date.valueOf()) ? "桌面贴图" : dateFormat.format(date);
  detail.append(
    element("strong", label),
    element("span", [pin.hidden ? "已隐藏" : "桌面显示中", pin.clickThrough ? "鼠标穿透" : "可点击"].join(" · ")),
  );
  const tools = element("div");
  tools.className = "pin-tools";
  const controls = element("div");
  controls.className = "actions";
  controls.append(
    button(pin.hidden ? "显示" : "隐藏", () => void act(() => managePin(pin.id, pin.hidden ? "show" : "hide")), `${pin.id}:visibility`),
    button(pin.clickThrough ? "恢复点击" : "鼠标穿透", () => void act(() => managePin(pin.id, pin.clickThrough ? "unlock" : "lock")), `${pin.id}:click`),
    button("旋转 90°", () => void act(() => managePin(pin.id, "rotate-right")), `${pin.id}:rotate`),
  );
  tools.append(controls, groupEditor(`${pin.id}:group`, pin.group, `${label}贴图的分组`, async (group) => {
    await managePin(pin.id, "group", group);
  }));
  row.append(picture, detail, tools);
  return row;
}

function groupedSection(group: string, pins: ManagedPin[]): HTMLElement {
  const section = element("section");
  section.className = "group";
  const key = `group:${group}`;
  const header = element("header");
  header.className = "group-head";
  const label = element("h2", group || "未分组");
  const count = element("span", `${pins.length} 张`);
  count.className = "group-count";
  label.append(count);
  const actions = element("div");
  actions.className = "actions";
  actions.append(
    button("显示整组", () => void act(() => managePinGroup(group, "show")), `${key}:show`, pins.every((pin) => !pin.hidden)),
    button("隐藏整组", () => void act(() => managePinGroup(group, "hide")), `${key}:hide`, pins.every((pin) => pin.hidden)),
    button("整组恢复点击", () => void act(() => managePinGroup(group, "unlock")), `${key}:unlock`, pins.every((pin) => !pin.clickThrough)),
  );
  header.append(label, actions);
  section.append(header);
  if (group) {
    const rename = groupEditor(`${key}:rename`, group, `「${group}」的新分组名`, (name) => managePinGroup(group, "rename", name));
    rename.hidden = true;
    actions.append(button("重命名", () => {
      rename.hidden = !rename.hidden;
      if (!rename.hidden) rename.querySelector("input")?.focus();
    }, `${key}:rename`));
    section.append(rename);
  }
  section.append(...pins.map(pinRow));
  return section;
}

function render(pins: ManagedPin[]): void {
  const active = document.activeElement as HTMLElement | null;
  const focusKey = list.contains(active) ? active?.dataset.control : undefined;
  const groups = new Map<string, ManagedPin[]>();
  for (const pin of pins) {
    const group = groups.get(pin.group) ?? [];
    group.push(pin);
    groups.set(pin.group, group);
  }
  const sorted = [...groups].sort(([first], [second]) => {
    if (!first || !second) return Number(!first) - Number(!second);
    return first.localeCompare(second, "zh-CN");
  });
  list.replaceChildren(...sorted.map(([group, members]) => groupedSection(group, members)));
  choices.replaceChildren(...sorted.filter(([group]) => group).map(([group]) => {
    const option = element("option");
    option.value = group;
    return option;
  }));
  empty.hidden = pins.length !== 0;
  if (focusKey) {
    const next = [...list.querySelectorAll<HTMLElement>("[data-control]")].find((node) => node.dataset.control === focusKey);
    if (next && !next.closest("[hidden]") && !(next instanceof HTMLButtonElement && next.disabled)) next.focus();
    else title.focus();
  }
  setStatus(`${pins.length} 张贴图 · ${pins.filter((pin) => !pin.hidden).length} 张显示中`);
}

async function refresh(): Promise<void> {
  const version = ++request;
  try {
    const pins = await listManagedPins();
    if (version === request) render(pins);
  } catch (error) {
    if (version === request) setStatus(`无法读取贴图：${error instanceof Error ? error.message : String(error)}`, true);
  } finally {
    if (version === request && !busy) list.setAttribute("aria-busy", "false");
  }
}

onManagedPinsChanged(() => {
  if (!busy) void refresh();
});
void refresh();
