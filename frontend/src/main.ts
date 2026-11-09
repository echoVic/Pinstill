// The main Tupen window: the design prototype's single-page UI, wired to the
// native capture/history/settings bindings through bridge.ts.
//
// Views: 截图架 (editor) · 历史 (stored captures) · 偏好设置 (six categories).
// When the page runs in a plain browser the bridge degrades to the prototype
// simulation and the design still demos without errors.

import {
  copyImage,
  exportImage,
  historyClear,
  historyGet,
  historyList,
  historyRemove,
  hasNative,
  onCaptureCancelled,
  onCaptureCompleted,
  onCaptureFailed,
  onHistoryChanged,
  onToast,
  openPermissionSettings,
  permissionGranted,
  pinImage,
  beginRegion,
  applySettings,
  showMainWindow,
  type CaptureCompleted,
  type HistoryEntry,
} from "./bridge";
import { Editor } from "./editor";
import { refreshTupenIcons } from "./icons";
import {
  DEFAULT_PREFERENCES,
  SETTINGS_KEY_LABELS,
  SETTINGS_SCHEMA,
  SHORTCUT_KEYS,
  formatAccelerator,
  loadPreferences,
  preferenceValue,
  recordAccelerator,
  sameAccelerator,
  savePreferences,
  setPreferenceValue,
  toNativeSettings,
  type Preferences,
  type SettingRow,
} from "./settings";
import { TOOL_ICONS } from "./tools";
import { formatBytes, loadCanvasImage } from "./util";

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

type View = "rack" | "history" | "settings";
type ShortcutKey = (typeof SHORTCUT_KEYS)[number];

const preferences: Preferences = loadPreferences();
let activeView: View = "rack";
let settingsCategory = SETTINGS_SCHEMA[0]!.id;
let recordingShortcut: ShortcutKey | null = null;
let historyEntries: HistoryEntry[] = [];
let applySequence = 0;
let applyTimer: number | undefined;
let historyTimer: number | undefined;
let clearArmed = false;
let clearTimer: number | undefined;
let toastTimer: number | undefined;
let permissionClickBound = false;

// ---------------------------------------------------------------------------
// Editor
// ---------------------------------------------------------------------------

const editor = new Editor({
  canvas: el<HTMLCanvasElement>("shotCanvas"),
  toolbar: el("captureToolbar"),
  properties: el("toolProperties"),
  resultPanel: el("resultPanel"),
  status: el("editorStatus"),
  captureSize: el("captureSize"),
  captureHint: el("captureHint"),
  getPreferences: () => preferences,
  actions: {
    pin: () => pinCurrent(),
    translate: () =>
      editor.showResult(
        "翻译 · 待接入",
        "翻译服务尚未接入。当前截图只在本机处理，接入后可在结果面板查看译文。",
      ),
    ocr: () =>
      editor.showResult(
        "提取文字 · 待接入",
        "文字识别服务尚未接入。当前截图不会上传到任何服务器。",
      ),
    qr: () =>
      editor.showResult(
        "识别二维码 · 待接入",
        "二维码识别尚未接入。接入后会在结果面板展示识别出的链接或文本。",
      ),
    scroll: () =>
      editor.showResult(
        "滚动截图 · 待接入",
        "屏幕滚动采集尚未接入。当前仍可对普通截图继续标注与导出。",
      ),
    download: () => exportCurrent(),
    clipboard: () => copyCurrent(),
  },
});

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error) return error;
  return "未知错误";
}

function showToast(text: string, kind: "info" | "success" | "error" = "info"): void {
  const toast = el("toast");
  toast.textContent = text;
  toast.dataset.kind = kind;
  toast.classList.add("show");
  if (toastTimer) window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toast.classList.remove("show"), 2800);
}

function dateStamp(): string {
  const now = new Date();
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

function safeFilenamePrefix(value: string): string {
  const cleaned = value
    .trim()
    .replace(/[\\/:*?"<>|\s]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return cleaned || "Tupen";
}

function formatHistoryTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "未知时间";
  const now = new Date();
  const time = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const startOfDate = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  const dayDiff = Math.round((startOfToday - startOfDate) / 86_400_000);
  if (dayDiff === 0) return `今天 ${time}`;
  if (dayDiff === 1) return `昨天 ${time}`;
  if (date.getFullYear() === now.getFullYear()) {
    return `${date.getMonth() + 1}月${date.getDate()}日`;
  }
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

function setSettingsStatus(text: string, kind: "info" | "error" = "info"): void {
  const status = el("settingsStatus");
  status.textContent = text;
  status.dataset.kind = kind;
}

function updateShortcutButton(): void {
  el("shortcut").textContent = `${formatAccelerator(preferences.captureKey)}　快捷键`;
}

// ---------------------------------------------------------------------------
// Editor actions (toolbar)
// ---------------------------------------------------------------------------

async function copyCurrent(): Promise<void> {
  if (!editor.hasCapture()) {
    showToast("先截取一张图片，再保存到剪贴板", "info");
    return;
  }
  try {
    const base64 = await editor.toPNGBase64();
    await copyImage(base64);
    showToast("截图与标注已保存到剪贴板", "success");
  } catch (error) {
    if (!hasNative) {
      editor.showResult(
        "保存到剪贴板",
        "当前浏览器预览未开放图片剪贴板写入。桌面应用中会直接写入系统剪贴板，也可先使用「下载」保存 PNG。",
      );
      return;
    }
    showToast(`复制失败：${errorMessage(error)}`, "error");
  }
}

async function exportCurrent(): Promise<void> {
  if (!editor.hasCapture()) {
    showToast("先截取一张图片，再导出", "info");
    return;
  }
  try {
    const base64 = await editor.toPNGBase64();
    const suggestedName = `${safeFilenamePrefix(preferences.filenamePrefix)}-${dateStamp()}`;
    const path = await exportImage(base64, suggestedName);
    if (path) showToast(`已导出：${path}`, "success");
    else showToast("已取消导出", "info");
  } catch (error) {
    showToast(`导出失败：${errorMessage(error)}`, "error");
  }
}

async function pinCurrent(): Promise<void> {
  if (!editor.hasCapture()) {
    showToast("先截取一张图片，再固定到屏幕", "info");
    return;
  }
  try {
    const base64 = await editor.toPNGBase64();
    if (!hasNative) {
      showPinnedPreview(editor.getCanvas().toDataURL("image/png"));
      showToast("浏览器预览 · 桌面应用中会创建置顶贴图窗口", "info");
      return;
    }
    await pinImage(base64);
    showToast("已固定到屏幕", "success");
  } catch (error) {
    showToast(`固定到屏幕失败：${errorMessage(error)}`, "error");
  }
}

function showPinnedPreview(dataURL: string): void {
  const preview = el("pinnedPreview");
  const image = el<HTMLImageElement>("pinnedImage");
  image.src = dataURL;
  preview.style.opacity = String(preferences.pinOpacity / 100);
  preview.style.outline = preferences.pinBorder ? "1px solid #ffffff45" : "none";
  preview.hidden = false;
}

// ---------------------------------------------------------------------------
// Capture flow
// ---------------------------------------------------------------------------

async function startCapture(): Promise<void> {
  try {
    if (!hasNative) {
      showView("rack");
      // A simulated capture replaces the canvas like a real one would.
      editor.setDemo();
      showToast("浏览器预览 · 已模拟一次截取（桌面应用中会打开区域选择覆盖层）", "info");
      return;
    }
    await beginRegion();
  } catch (error) {
    showToast(`无法开始截取：${errorMessage(error)}`, "error");
  }
}

async function handleCaptureCompleted(payload: CaptureCompleted): Promise<void> {
  try {
    const image = await loadCanvasImage(payload.url);
    editor.setBase(image);
    showView("rack");
    showToast("已截取一张截图，放进顶部截图架", "success");
    scheduleHistoryRefresh();
  } catch (error) {
    showToast(`截图已保存，但载入画布失败：${errorMessage(error)}`, "error");
  }
}

// ---------------------------------------------------------------------------
// History & rack
// ---------------------------------------------------------------------------

function scheduleHistoryRefresh(): void {
  if (historyTimer) window.clearTimeout(historyTimer);
  historyTimer = window.setTimeout(() => void refreshHistory(), 140);
}

async function refreshHistory(): Promise<void> {
  try {
    historyEntries = await historyList();
  } catch {
    // The native bindings may not be implemented yet; an empty history is fine.
    historyEntries = [];
  }
  renderRack();
  renderHistory();
  updateCounts();
}

function updateCounts(): void {
  const rackCount = document.getElementById("rackCount");
  if (rackCount) rackCount.textContent = String(Math.min(historyEntries.length, 6)).padStart(2, "0");
  const historyCount = document.getElementById("historyCount");
  if (historyCount) historyCount.textContent = String(historyEntries.length);
  const historyTotal = document.getElementById("historyTotal");
  if (historyTotal) historyTotal.textContent = `· ${historyEntries.length} 张`;
}

function renderRack(): void {
  const rack = el("rack");
  rack.replaceChildren();
  const label = document.createElement("div");
  label.className = "rack-label";
  label.textContent = "截图架 · 暂存待用";
  rack.append(label);
  const recent = historyEntries.slice(0, 6);
  for (const entry of recent) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "pin has-thumb";
    button.title = `打开 ${formatHistoryTime(entry.createdAt)} 的截图`;
    button.setAttribute("aria-label", `打开 ${formatHistoryTime(entry.createdAt)} 的截图`);
    const image = document.createElement("img");
    image.src = entry.thumbUrl;
    image.alt = "";
    image.loading = "lazy";
    image.decoding = "async";
    button.append(image);
    button.addEventListener("click", () => void openHistoryEntry(entry.id));
    rack.append(button);
  }
  for (let index = recent.length; index < 3; index += 1) {
    const placeholder = document.createElement("div");
    placeholder.className = "pin";
    placeholder.setAttribute("aria-hidden", "true");
    rack.append(placeholder);
  }
}

const HISTORY_RENDER_LIMIT = 60;

function renderHistory(): void {
  const grid = el("historyGrid");
  grid.replaceChildren();
  const empty = el("historyEmpty");
  empty.hidden = historyEntries.length > 0;
  grid.hidden = historyEntries.length === 0;
  for (const entry of historyEntries.slice(0, HISTORY_RENDER_LIMIT)) {
    const card = document.createElement("article");
    card.className = "history-card";

    const open = document.createElement("button");
    open.type = "button";
    open.className = "history-open";
    open.setAttribute(
      "aria-label",
      `打开 ${formatHistoryTime(entry.createdAt)} 的截图（${entry.width} × ${entry.height}）`,
    );
    const thumb = document.createElement("span");
    thumb.className = "thumb";
    const image = document.createElement("img");
    image.src = entry.thumbUrl;
    image.alt = "";
    image.loading = "lazy";
    image.decoding = "async";
    thumb.append(image);
    const caption = document.createElement("p");
    caption.textContent = `${formatHistoryTime(entry.createdAt)} · ${entry.width} × ${entry.height} · ${formatBytes(entry.size)}`;
    open.append(thumb, caption);
    open.addEventListener("click", () => void openHistoryEntry(entry.id));

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "history-delete";
    remove.textContent = "×";
    remove.setAttribute("aria-label", "删除这张截图");
    remove.addEventListener("click", (event) => {
      event.stopPropagation();
      void removeHistoryEntry(entry.id);
    });

    card.append(open, remove);
    grid.append(card);
  }
  let note = document.getElementById("historyNote");
  if (historyEntries.length > HISTORY_RENDER_LIMIT) {
    if (!note) {
      note = document.createElement("p");
      note.id = "historyNote";
      note.className = "history-empty";
      grid.after(note);
    }
    note.textContent = `仅显示最近 ${HISTORY_RENDER_LIMIT} 张截图，共 ${historyEntries.length} 张。`;
    note.hidden = false;
  } else if (note) {
    note.hidden = true;
  }
}

async function openHistoryEntry(id: string): Promise<void> {
  try {
    const item = await historyGet(id);
    const image = await loadCanvasImage(item.url);
    editor.setBase(image, { width: item.width, height: item.height });
    showView("rack");
    showToast("已载入历史截图", "success");
  } catch (error) {
    showToast(`打开截图失败：${errorMessage(error)}`, "error");
  }
}

async function removeHistoryEntry(id: string): Promise<void> {
  try {
    await historyRemove(id);
    showToast("已删除这张截图", "success");
  } catch (error) {
    showToast(`删除失败：${errorMessage(error)}`, "error");
  }
  await refreshHistory();
}

function resetClearButton(): void {
  clearArmed = false;
  if (clearTimer) window.clearTimeout(clearTimer);
  const button = el<HTMLButtonElement>("historyClear");
  button.textContent = "清空历史";
  button.classList.remove("danger");
}

async function clearHistory(): Promise<void> {
  try {
    await historyClear();
    showToast("已清空截图历史", "success");
  } catch (error) {
    showToast(`清空失败：${errorMessage(error)}`, "error");
  }
  await refreshHistory();
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

function showView(view: View): void {
  activeView = view;
  document.querySelectorAll<HTMLButtonElement>("[data-view]").forEach((button) => {
    button.classList.toggle("active", button.dataset.view === view);
  });
  const settings = view === "settings";
  el("settingsView").hidden = !settings;
  el("workspaceView").hidden = settings;
  const title = document.querySelector<HTMLElement>(".title");
  const subtitle = document.querySelector<HTMLElement>(".sub");
  if (title) title.textContent = settings ? "让 Tupen 更顺手。" : "截取、标注，再带走。";
  if (subtitle) {
    subtitle.textContent = settings
      ? "把常用习惯设好，剩下的交给每一次截图。"
      : "截图后就地编辑，一步复制、下载或固定到屏幕。";
  }
  const topActions = document.querySelector<HTMLElement>(".top .actions");
  if (topActions) topActions.hidden = settings;
  if (settings) {
    renderSettings();
    return;
  }
  el("historyView").classList.toggle("show", view === "history");
  el("rackView").style.display = view === "history" ? "none" : "block";
  if (view === "history") void refreshHistory();
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

function renderSettings(): void {
  const nav = el("settingsNav");
  nav.replaceChildren();
  for (const category of SETTINGS_SCHEMA) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = category.id === settingsCategory ? "active" : "";
    if (category.id === settingsCategory) button.setAttribute("aria-current", "page");
    button.innerHTML = `<i data-lucide="${TOOL_ICONS[category.icon]}" aria-hidden="true"></i><span>${category.name}</span>`;
    button.addEventListener("click", () => {
      settingsCategory = category.id;
      recordingShortcut = null;
      renderSettings();
    });
    nav.append(button);
  }
  refreshTupenIcons();

  const category = SETTINGS_SCHEMA.find((entry) => entry.id === settingsCategory) ?? SETTINGS_SCHEMA[0]!;
  const body = el("settingsBody");
  body.replaceChildren();
  const kicker = document.createElement("div");
  kicker.className = "settings-kicker";
  kicker.textContent = `PREFERENCES / ${String(SETTINGS_SCHEMA.indexOf(category) + 1).padStart(2, "0")}`;
  const heading = document.createElement("h2");
  heading.className = "settings-heading";
  heading.textContent = category.name;
  const description = document.createElement("p");
  description.className = "settings-description";
  description.textContent = category.description;
  body.append(kicker, heading, description);

  for (const group of category.groups) {
    const card = document.createElement("section");
    card.className = "settings-card";
    const groupTitle = document.createElement("h3");
    groupTitle.textContent = group.title;
    card.append(groupTitle);
    for (const row of group.rows) card.append(renderSettingRow(row));
    body.append(card);
  }
}

function renderSettingRow(row: SettingRow): HTMLElement {
  const wrapper = document.createElement("div");
  wrapper.className = "setting-row";
  const copy = document.createElement("div");
  const label = document.createElement("label");
  label.className = "setting-label";
  label.htmlFor = `setting-${row.key}`;
  label.textContent = row.name;
  const description = document.createElement("div");
  description.className = "setting-description";
  description.id = `desc-${row.key}`;
  description.textContent = row.description;
  copy.append(label, description);

  const control = document.createElement("div");
  control.className = "settings-control";

  if (row.type === "toggle") {
    const wrap = document.createElement("label");
    wrap.className = "switch";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = Boolean(preferenceValue(preferences, row.key));
    const track = document.createElement("span");
    track.className = "switch-track";
    wrap.append(input, track);
    control.append(wrap);
    input.id = `setting-${row.key}`;
    input.setAttribute("aria-label", row.name);
    input.setAttribute("aria-describedby", description.id);
    input.addEventListener("change", () => commitSetting(row, input));
  } else if (row.type === "select") {
    const select = document.createElement("select");
    for (const option of row.options) {
      const element = document.createElement("option");
      element.value = option.value;
      element.textContent = option.label;
      select.append(element);
    }
    select.value = String(preferenceValue(preferences, row.key));
    control.append(select);
    select.id = `setting-${row.key}`;
    select.setAttribute("aria-label", row.name);
    select.setAttribute("aria-describedby", description.id);
    select.addEventListener("change", () => commitSetting(row, select));
  } else if (row.type === "range") {
    const input = document.createElement("input");
    input.type = "range";
    input.min = String(row.min);
    input.max = String(row.max);
    input.step = String(row.step);
    input.value = String(preferenceValue(preferences, row.key));
    const range = document.createElement("div");
    range.className = "setting-range";
    const output = document.createElement("output");
    output.textContent = `${input.value}${row.unit}`;
    input.addEventListener("input", () => {
      output.textContent = `${input.value}${row.unit}`;
      applyPreferenceLive(row, input);
    });
    input.addEventListener("change", () => scheduleApplySettings());
    range.append(input, output);
    control.append(range);
    input.id = `setting-${row.key}`;
    input.setAttribute("aria-label", row.name);
    input.setAttribute("aria-describedby", description.id);
  } else if (row.type === "shortcut") {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "shortcut-record";
    button.dataset.key = row.key;
    const key = row.key as ShortcutKey;
    button.textContent =
      recordingShortcut === key
        ? "按下组合键…"
        : formatAccelerator(String(preferenceValue(preferences, row.key)));
    if (recordingShortcut === key) button.classList.add("recording");
    button.addEventListener("click", () => {
      recordingShortcut = key;
      renderSettings();
      setSettingsStatus("请按下组合键，Esc 取消录入", "info");
    });
    control.append(button);
    button.id = `setting-${row.key}`;
    button.setAttribute("aria-label", `${row.name}，当前 ${formatAccelerator(String(preferenceValue(preferences, row.key)))}`);
  } else if (row.type === "color") {
    const input = document.createElement("input");
    input.type = "color";
    input.value = String(preferenceValue(preferences, row.key));
    input.addEventListener("input", () => applyPreferenceLive(row, input));
    input.addEventListener("change", () => scheduleApplySettings());
    control.append(input);
    input.id = `setting-${row.key}`;
    input.setAttribute("aria-label", row.name);
    input.setAttribute("aria-describedby", description.id);
  } else if (row.type === "text") {
    const input = document.createElement("input");
    input.type = "text";
    input.value = String(preferenceValue(preferences, row.key));
    input.maxLength = 120;
    input.addEventListener("change", () => commitSetting(row, input));
    control.append(input);
    input.id = `setting-${row.key}`;
    input.setAttribute("aria-label", row.name);
    input.setAttribute("aria-describedby", description.id);
  } else {
    control.textContent = row.text;
    control.style.cssText = "font-size:11px;color:#aaa18f";
  }

  wrapper.append(copy, control);
  return wrapper;
}

function applyPreferenceLive(row: SettingRow, input: HTMLInputElement | HTMLSelectElement): void {
  setPreferenceValue(preferences, row, input.value);
  savePreferences(preferences);
  applyPreferences(row.key);
}

function commitSetting(row: SettingRow, input: HTMLInputElement | HTMLSelectElement): void {
  const value =
    row.type === "toggle" && input instanceof HTMLInputElement ? input.checked : input.value;
  setPreferenceValue(preferences, row, value);
  savePreferences(preferences);
  applyPreferences(row.key);
  scheduleApplySettings();
}

function applyPreferences(changedKey?: string): void {
  document.documentElement.style.setProperty("--tooltip-delay", `${preferences.tooltipDelay}ms`);
  let style = document.getElementById("preferenceStyle");
  if (!style) {
    style = document.createElement("style");
    style.id = "preferenceStyle";
    document.head.append(style);
  }
  style.textContent = preferences.tooltip ? "" : ".capture-toolbar button span{display:none}";
  const preview = el("pinnedPreview");
  preview.style.opacity = String(preferences.pinOpacity / 100);
  preview.style.outline = preferences.pinBorder ? "1px solid #ffffff45" : "none";
  if (changedKey === "defaultColor" || changedKey === "defaultWidth") {
    // Applied the next time a capture or the default tool is (re)selected.
  }
  updateShortcutButton();
}

function scheduleApplySettings(delay = 160): void {
  if (applyTimer) window.clearTimeout(applyTimer);
  applyTimer = window.setTimeout(() => void pushNativeSettings(), delay);
}

async function pushNativeSettings(): Promise<void> {
  const sequence = ++applySequence;
  try {
    const failures = await applySettings(toNativeSettings(preferences));
    if (sequence !== applySequence) return;
    renderSettingsApplyStatus(failures);
  } catch (error) {
    if (sequence !== applySequence) return;
    setSettingsStatus(`设置同步失败：${errorMessage(error)}`, "error");
  }
}

function renderSettingsApplyStatus(failures: Record<string, string>): void {
  const entries = Object.entries(failures ?? {});
  if (entries.length === 0) {
    setSettingsStatus(
      hasNative ? "设置已同步到桌面应用" : "浏览器预览 · 设置保存在本地",
      "info",
    );
    return;
  }
  const parts = entries.map(
    ([key, message]) => `${SETTINGS_KEY_LABELS[key] ?? key}：${message}`,
  );
  setSettingsStatus(`部分设置未生效 — ${parts.join("；")}`, "error");
}

function handleShortcutRecording(event: KeyboardEvent): void {
  event.preventDefault();
  event.stopImmediatePropagation();
  if (event.key === "Escape") {
    recordingShortcut = null;
    renderSettings();
    setSettingsStatus("已取消录入", "info");
    return;
  }
  const result = recordAccelerator(event);
  if (!result) return;
  if ("error" in result) {
    setSettingsStatus(result.error, "error");
    return;
  }
  const key = recordingShortcut;
  if (!key) return;
  const duplicate = SHORTCUT_KEYS.some(
    (other) => other !== key && sameAccelerator(preferences[other], result.accelerator),
  );
  if (duplicate) {
    setSettingsStatus("这个组合键已被其他动作使用", "error");
    return;
  }
  preferences[key] = result.accelerator;
  savePreferences(preferences);
  recordingShortcut = null;
  renderSettings();
  applyPreferences();
  setSettingsStatus(`已录入 ${formatAccelerator(result.accelerator)}，正在注册…`, "info");
  scheduleApplySettings(0);
}

function resetCurrentCategory(): void {
  const category = SETTINGS_SCHEMA.find((entry) => entry.id === settingsCategory);
  if (!category) return;
  for (const group of category.groups) {
    for (const row of group.rows) {
      if (row.type === "info") continue;
      if (!(row.key in DEFAULT_PREFERENCES)) continue;
      (preferences as unknown as Record<string, unknown>)[row.key] = (
        DEFAULT_PREFERENCES as unknown as Record<string, unknown>
      )[row.key];
    }
  }
  savePreferences(preferences);
  applyPreferences();
  renderSettings();
  scheduleApplySettings(0);
  setSettingsStatus("本分类已恢复默认值", "info");
}

// ---------------------------------------------------------------------------
// Permission status
// ---------------------------------------------------------------------------

function setPermissionLabel(text: string, warning = false): void {
  const label = document.getElementById("permissionLabel");
  const status = document.getElementById("permissionStatus");
  if (label) label.textContent = text;
  if (status) status.classList.toggle("warn", warning);
}

async function refreshPermissionStatus(): Promise<void> {
  if (!hasNative) {
    setPermissionLabel("浏览器预览 · 原型演示");
    return;
  }
  try {
    const granted = await permissionGranted();
    setPermissionLabel(granted ? "已准备好截取" : "需要屏幕录制权限", !granted);
    if (!granted && !permissionClickBound) {
      permissionClickBound = true;
      const status = document.getElementById("permissionStatus");
      status?.addEventListener("click", () => void openPermissionSettings().catch(() => {}));
      status?.addEventListener("keydown", (event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          void openPermissionSettings().catch(() => {});
        }
      });
      status?.setAttribute("tabindex", "0");
      status?.setAttribute("role", "button");
    }
  } catch {
    setPermissionLabel("原生桥接未就绪", true);
  }
}

// ---------------------------------------------------------------------------
// Mode-specific copy
// ---------------------------------------------------------------------------

function applyModeTexts(): void {
  const previewNote = document.getElementById("settingsPreviewNote");
  const desktopNote = document.getElementById("desktopNote");
  if (!hasNative) return;
  if (previewNote) {
    previewNote.textContent = "设置保存在本机，并即时同步到 Tupen。识别与翻译功能将在后续版本接入。";
  }
  if (desktopNote) {
    desktopNote.querySelector<HTMLElement>(".side-title")!.textContent = "桌面应用";
    desktopNote.querySelector<HTMLElement>(".toolbar-note")!.textContent =
      "截图保存在本机。翻译、文字及二维码识别、滚动截图将在后续版本接入。";
  }
}

// ---------------------------------------------------------------------------
// Wiring
// ---------------------------------------------------------------------------

el("capture").addEventListener("click", () => void startCapture());
el("shortcut").addEventListener("click", () => {
  settingsCategory = "shortcuts";
  showView("settings");
});
document.querySelectorAll<HTMLButtonElement>("[data-view]").forEach((button) => {
  button.addEventListener("click", () => showView(button.dataset.view as View));
});
el("unpin").addEventListener("click", () => {
  el("pinnedPreview").hidden = true;
});
el("resetSettings").addEventListener("click", () => resetCurrentCategory());
el("historyClear").addEventListener("click", () => {
  const button = el<HTMLButtonElement>("historyClear");
  if (!clearArmed) {
    clearArmed = true;
    button.textContent = "再次点击清空";
    button.classList.add("danger");
    if (clearTimer) window.clearTimeout(clearTimer);
    clearTimer = window.setTimeout(resetClearButton, 3200);
    return;
  }
  resetClearButton();
  void clearHistory();
});

document.addEventListener("keydown", (event) => {
  if (recordingShortcut) {
    handleShortcutRecording(event);
    return;
  }
  const target = event.target as HTMLElement | null;
  const tagName = target?.tagName ?? "";
  if (["INPUT", "TEXTAREA", "SELECT"].includes(tagName)) return;
  if (activeView === "settings") return;
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "z" && !event.shiftKey) {
    event.preventDefault();
    editor.undo();
    return;
  }
  if (event.key === "Escape") {
    editor.exitCapture();
    return;
  }
  if (event.key === "Enter" && preferences.enterCopy && editor.hasCapture()) {
    const toolbar = el("captureToolbar");
    if (!toolbar.classList.contains("hidden")) {
      event.preventDefault();
      void copyCurrent();
    }
  }
});

// Native events (no-ops in the browser preview).
onCaptureCompleted((payload) => void handleCaptureCompleted(payload));
onCaptureCancelled(() => {
  showView("rack");
  editor.notify("已取消截取 · 点击“截一张”重新开始");
  showToast("已取消截取", "info");
});
onCaptureFailed((payload) => {
  showView("rack");
  showToast(payload.message || "截取失败", "error");
  if (payload.permission) {
    setPermissionLabel("需要屏幕录制权限", true);
    editor.showResult(
      "需要屏幕录制权限",
      "Tupen 需要系统的「屏幕录制」权限才能截图。请在系统设置中授权后重试。",
      { label: "打开系统设置", onClick: () => void openPermissionSettings().catch(() => {}) },
    );
  }
  void showMainWindow().catch(() => {});
});
onHistoryChanged(() => scheduleHistoryRefresh());
onToast((payload) => showToast(payload.text, (payload.kind as "info" | "success" | "error") ?? "info"));

// Initial state.
updateShortcutButton();
applyPreferences();
applyModeTexts();
if (hasNative) {
  editor.setEmpty();
} else {
  editor.setDemo();
}
void refreshHistory();
void refreshPermissionStatus();
void pushNativeSettings();

// Keep the permission hint fresh when the app regains focus (e.g. after the
// user granted screen recording in System Settings).
window.addEventListener("focus", () => {
  if (hasNative) void refreshPermissionStatus();
});
