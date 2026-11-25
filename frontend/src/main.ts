// The main Pinstill window (R8): history + preferences only.
//
// The main-window editor was removed by product decision: annotation happens
// only in the region-selection overlay toolbar. This window lists stored
// captures ("历史") and opens an in-app preview modal with copy / save / pin /
// delete. Preferences are unchanged (permission centre included).
//
// When the page runs in a plain browser the bridge degrades to demo history
// entries, so the static preview stays clickable without console errors.

import {
  applySettings,
  beginRegion,
  hasNative,
  historyClear,
  historyCopy,
  historyHang,
  historyList,
  historyPin,
  historyRemove,
  historySave,
  launchAtLogin,
  listArchivedPins,
  onCaptureCancelled,
  onCaptureFailed,
  onHistoryChanged,
  onToast,
  openLoginItemsSettings,
  openPermissionSettings,
  openScrollAccessibilitySettings,
  permissionGranted,
  requestScreenPermission,
  restoreArchivedPin,
  scrollAccessibilityGranted,
  type HistoryEntry,
  type PinArchiveEntry,
} from "./bridge";
import { refreshPinstillIcons } from "./icons";
import { renderGlossarySettings } from "./glossary-settings";
import { renderModelSettings } from "./model-settings";
import { renderTTSSettings } from "./tts-settings";
import {
  DEFAULT_PREFERENCES,
  SETTINGS_KEY_LABELS,
  SETTINGS_SCHEMA,
  SHORTCUT_KEYS,
  formatAccelerator,
  isMacPlatform,
  loadPreferences,
  preferenceValue,
  recordAccelerator,
  sameAccelerator,
  savePreferences,
  setPreferenceValue,
  toNativeSettings,
  type PermissionSetting,
  type Preferences,
  type SettingRow,
} from "./settings";
import { TOOL_ICONS } from "./tools";
import {
  flashButtonSuccess,
  formatBytes,
  setButtonPending,
} from "./util";

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

type View = "history" | "settings";
type ShortcutKey = (typeof SHORTCUT_KEYS)[number];

const preferences: Preferences = loadPreferences();
let activeView: View = "history";
let settingsCategory = SETTINGS_SCHEMA[0]!.id;
let recordingShortcut: ShortcutKey | null = null;
let historyEntries: HistoryEntry[] = [];
let applySequence = 0;
let applyTimer: number | undefined;
let applyingSettings = false;
let pendingSettings = false;
let appliedSettingsJSON: string | undefined;
let historySequence = 0;
let historyTimer: number | undefined;
let clearArmed = false;
let clearTimer: number | undefined;
let armedDelete: { id: string; button: HTMLButtonElement; timer: number } | null = null;
let toastTimer: number | undefined;
let permissionClickBound = false;
let bannerDismissed = false;
let permissionRefreshSequence = 0;

interface PreviewState {
  index: number;
  trigger: HTMLElement | null;
  deleteArmed: boolean;
  deleteTimer: number | undefined;
}

let preview: PreviewState = { index: -1, trigger: null, deleteArmed: false, deleteTimer: undefined };

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
// Capture entry (the overlay does the actual selection + annotation)
// ---------------------------------------------------------------------------

async function startCapture(): Promise<void> {
  try {
    if (!hasNative) {
      showToast("浏览器预览 · 桌面应用中会打开区域选择覆盖层", "info");
      return;
    }
    await beginRegion();
  } catch (error) {
    showToast(`无法开始截取：${errorMessage(error)}`, "error");
  }
}

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------

function scheduleHistoryRefresh(): void {
  if (historyTimer) window.clearTimeout(historyTimer);
  historyTimer = window.setTimeout(() => void refreshHistory(), 140);
}

async function refreshHistory(): Promise<void> {
  const sequence = ++historySequence;
  let entries: HistoryEntry[];
  try {
    entries = await historyList();
  } catch {
    if (sequence === historySequence) showToast("无法刷新历史截图，请稍后重试", "error");
    return;
  }
  if (sequence !== historySequence) return;
  const previewId = currentPreviewEntry()?.id;
  historyEntries = entries;
  renderHistory();
  updateCounts();
  if (previewVisible()) {
    if (historyEntries.length === 0) {
      closeHistoryPreview();
    } else {
      const retainedIndex = historyEntries.findIndex((entry) => entry.id === previewId);
      preview.index = retainedIndex >= 0
        ? retainedIndex
        : Math.min(Math.max(0, preview.index), historyEntries.length - 1);
      renderHistoryPreview();
    }
  }
}

function updateCounts(): void {
  const historyTotal = document.getElementById("historyTotal");
  if (historyTotal) historyTotal.textContent = `· ${historyEntries.length} 张`;
}

const HISTORY_RENDER_LIMIT = 60;

let archivedPins: PinArchiveEntry[] = [];
let archiveSequence = 0;
const restoringPins = new Set<string>();

async function refreshArchivedPins(): Promise<void> {
  const sequence = ++archiveSequence;
  try {
    const entries = await listArchivedPins();
    if (sequence !== archiveSequence) return;
    archivedPins = entries;
    renderArchivedPins();
    el("archivedPinStatus").textContent = "";
  } catch (error) {
    if (sequence === archiveSequence) el("archivedPinStatus").textContent = `无法刷新归档贴图：${errorMessage(error)}`;
  }
}

function renderArchivedPins(): void {
  const grid = el("archivedPinGrid");
  grid.replaceChildren();
  el("archivedPinTotal").textContent = `· ${archivedPins.length} 张`;
  el("archivedPinEmpty").hidden = archivedPins.length !== 0;
  for (const entry of archivedPins) {
    const card = document.createElement("article");
    card.className = "history-card";
    const image = document.createElement("img");
    image.className = "archived-pin-image";
    image.src = entry.url;
    image.alt = `归档贴图 · ${formatHistoryTime(entry.createdAt)}`;
    image.loading = "lazy";
    const meta = document.createElement("p");
    meta.className = "archived-pin-meta";
    meta.textContent = `${formatHistoryTime(entry.createdAt)} · ${entry.width} × ${entry.height}`;
    image.addEventListener("error", () => { meta.textContent = "无法载入贴图预览，可尝试恢复"; });
    const button = document.createElement("button");
    button.type = "button";
    button.className = "btn small archived-pin-restore";
    button.textContent = "恢复到桌面";
    setButtonPending(button, restoringPins.has(entry.id));
    button.addEventListener("click", () => void restorePinFromArchive(entry.id));
    card.append(image, meta, button);
    grid.append(card);
  }
}

async function restorePinFromArchive(id: string): Promise<void> {
  if (restoringPins.has(id)) return;
  restoringPins.add(id);
  ++archiveSequence; // Invalidate a list requested before this restoration.
  renderArchivedPins();
  el("archivedPinStatus").textContent = "";
  try {
    await restoreArchivedPin(id);
    archivedPins = archivedPins.filter(entry => entry.id !== id);
    await refreshArchivedPins();
  } catch (error) {
    el("archivedPinStatus").textContent = `恢复贴图失败：${errorMessage(error)}`;
  } finally {
    restoringPins.delete(id);
    renderArchivedPins();
  }
}

/** Restores the delete button after a confirm timeout / grid re-render. */
function disarmHistoryDelete(): void {
  if (!armedDelete) return;
  if (armedDelete.timer) window.clearTimeout(armedDelete.timer);
  const button = armedDelete.button;
  armedDelete = null;
  button.classList.remove("armed");
  button.textContent = "×";
  button.setAttribute("aria-label", "删除这张截图");
}

/** First click arms the delete; the second click within 3s really removes it. */
function armHistoryDelete(id: string, button: HTMLButtonElement): void {
  disarmHistoryDelete();
  button.classList.add("armed");
  button.textContent = "确认?";
  button.setAttribute("aria-label", "再次点击确认删除这张截图");
  armedDelete = {
    id,
    button,
    timer: window.setTimeout(() => disarmHistoryDelete(), 3200),
  };
}

function renderHistory(): void {
  disarmHistoryDelete();
  const grid = el("historyGrid");
  grid.replaceChildren();
  const empty = el("historyEmpty");
  empty.textContent = `还没有截图 · 点击右上角「截一张」，或按 ${formatAccelerator(
    preferences.captureKey,
  )} 开始`;
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
      `预览 ${formatHistoryTime(entry.createdAt)} 的截图（${entry.width} × ${entry.height}）`,
    );
    const thumb = document.createElement("span");
    thumb.className = "thumb";
    const image = document.createElement("img");
    image.alt = "";
    image.loading = "lazy";
    image.decoding = "async";
    const markBroken = () => {
      image.remove();
      thumb.classList.add("broken");
      thumb.setAttribute("aria-hidden", "true");
    };
    if (entry.thumbUrl) {
      image.addEventListener("error", markBroken);
      image.src = entry.thumbUrl;
      thumb.append(image);
    } else {
      markBroken();
    }
    const caption = document.createElement("p");
    caption.textContent = `${formatHistoryTime(entry.createdAt)} · ${entry.width} × ${entry.height} · ${formatBytes(entry.size)}`;
    open.append(thumb, caption);
    open.addEventListener("click", () => openHistoryPreview(entry.id, open));
    // The whole card is clickable (selftests and the design both click the card
    // itself, not only the inner button).
    card.addEventListener("click", (event) => {
      const target = event.target;
      if (target instanceof Element && target.closest(".history-delete")) return;
      if (target instanceof Element && target.closest(".history-open")) return;
      openHistoryPreview(entry.id, open);
    });

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "history-delete";
    remove.textContent = "×";
    remove.setAttribute("aria-label", "删除这张截图");
    remove.addEventListener("click", (event) => {
      event.stopPropagation();
      if (armedDelete?.id === entry.id) {
        disarmHistoryDelete();
        void removeHistoryEntry(entry.id);
        return;
      }
      armHistoryDelete(entry.id, remove);
    });

    card.append(open, remove);
    grid.append(card);
  }

  let note = document.getElementById("historyNote");
  if (historyEntries.length > HISTORY_RENDER_LIMIT) {
    if (!note) {
      note = document.createElement("p");
      note.id = "historyNote";
      note.className = "history-note";
      grid.after(note);
    }
    note.textContent = `仅显示最近 ${HISTORY_RENDER_LIMIT} 张截图，共 ${historyEntries.length} 张。`;
    note.hidden = false;
  } else if (note) {
    note.hidden = true;
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
  const button = document.getElementById("historyClear") as HTMLButtonElement | null;
  setButtonPending(button, true);
  try {
    await historyClear();
    setButtonPending(button, false);
    flashButtonSuccess(button);
    showToast("已清空截图历史", "success");
    if (previewVisible()) closeHistoryPreview();
  } catch (error) {
    setButtonPending(button, false);
    showToast(`清空失败：${errorMessage(error)}`, "error");
  }
  await refreshHistory();
}

// ---------------------------------------------------------------------------
// History preview modal
// ---------------------------------------------------------------------------

function previewVisible(): boolean {
  const modal = document.getElementById("historyPreview");
  return !!modal && !modal.hidden;
}

function currentPreviewEntry(): HistoryEntry | null {
  return historyEntries[preview.index] ?? null;
}

function resetPreviewDeleteConfirm(): void {
  if (preview.deleteTimer) window.clearTimeout(preview.deleteTimer);
  preview.deleteTimer = undefined;
  preview.deleteArmed = false;
  const button = document.querySelector<HTMLButtonElement>(
    '#historyPreview [data-history-action="delete"]',
  );
  if (button) {
    button.classList.remove("armed");
    button.textContent = "删除";
    button.setAttribute("aria-label", "删除这张截图");
  }
}

function renderHistoryPreview(): void {
  const modal = document.getElementById("historyPreview");
  if (!modal) return;
  const entry = currentPreviewEntry();
  if (!entry) {
    closeHistoryPreview();
    return;
  }
  const image = el<HTMLImageElement>("historyPreviewImage");
  if (image.getAttribute("src") !== entry.url) image.src = entry.url;
  el("historyPreviewTitle").textContent = `历史截图 · ${formatHistoryTime(entry.createdAt)}`;
  el("historyPreviewMeta").textContent = `${entry.width} × ${entry.height} px · ${formatBytes(entry.size)} · 第 ${preview.index + 1} / ${historyEntries.length} 张`;
  resetPreviewDeleteConfirm();
}

function openHistoryPreview(id: string, trigger: HTMLElement | null): void {
  const index = historyEntries.findIndex((entry) => entry.id === id);
  if (index < 0) {
    showToast("找不到这张截图", "error");
    return;
  }
  preview = { index, trigger, deleteArmed: false, deleteTimer: undefined };
  renderHistoryPreview();
  const modal = el("historyPreview");
  modal.hidden = false;
  document.body.classList.add("preview-open");
  const close = modal.querySelector<HTMLButtonElement>('[data-history-action="close"]');
  close?.focus();
}

function closeHistoryPreview(): void {
  const modal = document.getElementById("historyPreview");
  if (!modal || modal.hidden) return;
  modal.hidden = true;
  document.body.classList.remove("preview-open");
  resetPreviewDeleteConfirm();
  const trigger = preview.trigger;
  preview = { index: -1, trigger: null, deleteArmed: false, deleteTimer: undefined };
  if (trigger && trigger.isConnected) trigger.focus();
}

/** ←/→ step through adjacent history entries while the preview is open. */
function stepHistoryPreview(delta: number): void {
  if (!previewVisible() || historyEntries.length === 0) return;
  const next = Math.min(historyEntries.length - 1, Math.max(0, preview.index + delta));
  if (next === preview.index) return;
  preview.index = next;
  renderHistoryPreview();
}

async function runPreviewAction(action: string, button: HTMLButtonElement): Promise<void> {
  const entry = currentPreviewEntry();
  if (!entry) return;
  if (action === "close") {
    closeHistoryPreview();
    return;
  }
  if (action === "delete") {
    if (!preview.deleteArmed) {
      preview.deleteArmed = true;
      button.classList.add("armed");
      button.textContent = "确认删除?";
      button.setAttribute("aria-label", "再次点击确认删除这张截图");
      preview.deleteTimer = window.setTimeout(() => resetPreviewDeleteConfirm(), 3200);
      return;
    }
    resetPreviewDeleteConfirm();
    setButtonPending(button, true);
    try {
      await historyRemove(entry.id);
      setButtonPending(button, false);
      showToast("已删除这张截图", "success");
      await refreshHistory();
    } catch (error) {
      setButtonPending(button, false);
      showToast(`删除失败：${errorMessage(error)}`, "error");
    }
    return;
  }

  setButtonPending(button, true);
  try {
    if (action === "copy") {
      await historyCopy(entry.id);
      setButtonPending(button, false);
      flashButtonSuccess(button);
      showToast("已复制到剪贴板", "success");
    } else if (action === "save") {
      const path = await historySave(entry.id);
      setButtonPending(button, false);
      if (path) {
        flashButtonSuccess(button);
        showToast(`已保存：${path}`, "success");
      } else {
        showToast("已取消保存", "info");
      }
    } else if (action === "pin") {
      await historyPin(entry.id);
      setButtonPending(button, false);
      flashButtonSuccess(button);
      showToast(
        hasNative ? "已固定到屏幕" : "浏览器预览 · 桌面应用中会创建置顶贴图窗口",
        "success",
      );
    } else if (action === "hang") {
      await historyHang(entry.id);
      setButtonPending(button, false);
      flashButtonSuccess(button);
      showToast(hasNative ? "已挂到桌面晾绳" : "浏览器预览 · 桌面应用中可把截图挂到桌面晾绳", "success");
    }
  } catch (error) {
    setButtonPending(button, false);
    const labels: Record<string, string> = { copy: "复制", save: "保存", pin: "贴图", hang: "晾图" };
    showToast(`${labels[action] ?? "操作"}失败：${errorMessage(error)}`, "error");
  }
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

function showView(view: View): void {
  activeView = view;
  document.querySelectorAll<HTMLButtonElement>("[data-view]").forEach((button) => {
    const active = button.dataset.view === view;
    button.classList.toggle("active", active);
    if (active) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  });
  const settings = view === "settings";
  el("settingsView").hidden = !settings;
  el("historyView").hidden = settings;
  const title = document.querySelector<HTMLElement>(".title");
  const subtitle = document.querySelector<HTMLElement>(".sub");
  if (title) title.textContent = settings ? "让 Pinstill 更顺手。" : "截取后都在这里。";
  if (subtitle) {
    subtitle.textContent = settings
      ? "把常用习惯设好，剩下的交给每一次截图。"
      : "标注在选区工具栏里完成。点历史卡片即可预览、复制、保存或贴到屏幕上。";
  }
  const topActions = document.querySelector<HTMLElement>(".top .actions");
  if (topActions) topActions.hidden = settings;
  if (settings) {
    renderSettings();
    return;
  }
  void refreshHistory();
  void refreshArchivedPins();
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
    button.dataset.settingsCat = category.id;
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
  refreshPinstillIcons();

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
  if (category.id === "recognition") {
    const macNative = hasNative && isMacPlatform();
    body.append(renderModelSettings(macNative));
    body.append(renderTTSSettings(macNative));
    body.append(renderGlossarySettings());
  }
  // Permissions come from the OS, not from saved preferences: no "reset".
  const resetButton = document.getElementById("resetSettings") as HTMLButtonElement | null;
  if (resetButton) {
    resetButton.hidden = category.id === "permissions";
    resetButton.textContent = category.id === "recognition" ? "恢复识别与翻译偏好" : "恢复本分类默认值";
  }
  if (category.id === "permissions") void refreshPermissionCenter();
}

function renderSettingRow(row: SettingRow): HTMLElement {
  const unavailableOCR = row.key === "ocrLanguage" && (!hasNative || !isMacPlatform());
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
  description.textContent = unavailableOCR ? "请在 macOS 桌面版使用。" : row.description;
  copy.append(label, description);

  const control = document.createElement("div");
  control.className = "settings-control";

  if (row.type === "toggle") {
    const wrap = document.createElement("label");
    wrap.className = "switch";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.disabled = Boolean(row.disabled);
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
    select.disabled = Boolean(row.disabled) || unavailableOCR;
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
    button.disabled = Boolean(row.disabled);
    const key = row.key as ShortcutKey;
    button.textContent =
      row.disabled ? "未启用 · 待接入" : recordingShortcut === key
        ? "按下组合键…"
        : formatAccelerator(String(preferenceValue(preferences, row.key)));
    if (recordingShortcut === key) button.classList.add("recording");
    button.addEventListener("click", () => {
      if (row.disabled) return;
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
  } else if (row.type === "permission") {
    wrapper.classList.add("perm-row");
    label.removeAttribute("for");
    const spec = PERMISSION_ROWS[row.perm];
    const status = document.createElement("span");
    status.id = spec.statusId;
    status.className = "perm-status muted";
    status.setAttribute("role", "status");
    status.setAttribute("aria-live", "polite");
    status.textContent = hasNative ? "未授权 · 正在检测…" : BROWSER_PERMISSION_TEXT;
    const actions = document.createElement("div");
    actions.className = "perm-actions";
    for (const item of spec.actions) {
      const actionButton = document.createElement("button");
      actionButton.type = "button";
      actionButton.className = item.primary ? "perm-btn primary" : "perm-btn";
      actionButton.dataset.permAction = item.action;
      actionButton.textContent = item.label;
      actionButton.addEventListener("click", () =>
        void runPermissionAction(row.perm, item.action, actionButton),
      );
      actions.append(actionButton);
    }
    control.append(status, actions);
    if (row.perm === "launch") {
      const note = document.createElement("span");
      note.className = "perm-note";
      note.id = "permLaunchNote";
      note.textContent = launchPreferenceNote();
      control.append(note);
    }
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
  if (changedKey === "defaultColor" || changedKey === "defaultWidth") {
    // The overlay reads these from storage the next time a capture starts.
  }
  updateShortcutButton();
}

function scheduleApplySettings(delay = 160): void {
  ++applySequence;
  if (applyTimer) window.clearTimeout(applyTimer);
  applyTimer = window.setTimeout(() => {
    applyTimer = undefined;
    void pushNativeSettings();
  }, delay);
}

async function pushNativeSettings(): Promise<void> {
  pendingSettings = true;
  if (applyingSettings) return;
  applyingSettings = true;
  try {
    while (pendingSettings) {
      pendingSettings = false;
      const sequence = applySequence;
      const snapshot = toNativeSettings(preferences);
      const json = JSON.stringify(snapshot);
      // Annotation-only preferences do not need native shortcut registration
      // or history pruning. Failed/partially applied snapshots remain retryable.
      if (json === appliedSettingsJSON) {
        renderSettingsApplyStatus({});
        continue;
      }
      try {
        const failures = await applySettings(snapshot);
        appliedSettingsJSON = Object.keys(failures ?? {}).length === 0 ? json : undefined;
        if (sequence === applySequence) renderSettingsApplyStatus(failures);
      } catch (error) {
        appliedSettingsJSON = undefined;
        if (sequence === applySequence) {
          setSettingsStatus(`设置同步失败：${errorMessage(error)}`, "error");
        }
      }
    }
  } finally {
    applyingSettings = false;
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
  flashButtonSuccess(document.getElementById("resetSettings") as HTMLButtonElement | null);
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
      const requestAccess = () => {
        void (async () => {
          try {
            const granted = await requestScreenPermission();
            if (granted) {
              setPermissionLabel("已准备好截取");
              return;
            }
            await openPermissionSettings();
          } catch {
            await openPermissionSettings().catch(() => {});
          }
        })();
      };
      status?.addEventListener("click", requestAccess);
      status?.addEventListener("keydown", (event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          requestAccess();
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
// Permission centre (settings → 权限) + main-window banner (R7.1)
// ---------------------------------------------------------------------------

type PermissionTone = "ok" | "warn" | "muted";

interface PermissionActionSpec {
  action: string;
  label: string;
  primary?: boolean;
}

// Frozen DOM hooks from work/plan.md R7.1: status ids and data-perm-action
// values must not change (headless assertions and PINSTILL_SELFTEST=perms).
const PERMISSION_ROWS: Record<
  PermissionSetting,
  { statusId: string; actions: PermissionActionSpec[] }
> = {
  screen: {
    statusId: "permScreenStatus",
    actions: [
      { action: "screen-request", label: "请求权限", primary: true },
      { action: "screen-open", label: "打开系统设置" },
      { action: "screen-check", label: "重新检测" },
    ],
  },
  accessibility: {
    statusId: "permAccessibilityStatus",
    actions: [
      { action: "accessibility-open", label: "打开系统设置" },
      { action: "accessibility-check", label: "重新检测" },
    ],
  },
  launch: {
    statusId: "permLaunchStatus",
    actions: [{ action: "login-open", label: "打开登录项设置" }],
  },
};

const BROWSER_PERMISSION_TEXT = "浏览器预览 · 无需权限";

function setPermissionStatusText(id: string, text: string, tone: PermissionTone): void {
  const status = document.getElementById(id);
  if (!status) return;
  status.textContent = text;
  status.className = `perm-status ${tone}`;
}

function launchPreferenceNote(): string {
  return hasNative
    ? `偏好开关：${preferences.launch ? "已开启" : "已关闭"} · 与「通用与截图」的登录开关联动`
    : "浏览器预览 · 桌面应用中读取系统登录项的真实状态";
}

/** Re-reads the three OS permissions; safe to call when the category is closed. */
async function refreshPermissionCenter(): Promise<void> {
  if (!document.getElementById("permScreenStatus")) return;
  const sequence = ++permissionRefreshSequence;
  const note = document.getElementById("permLaunchNote");
  if (note) note.textContent = launchPreferenceNote();
  if (!hasNative) {
    setPermissionStatusText("permScreenStatus", BROWSER_PERMISSION_TEXT, "muted");
    setPermissionStatusText("permAccessibilityStatus", BROWSER_PERMISSION_TEXT, "muted");
    setPermissionStatusText("permLaunchStatus", BROWSER_PERMISSION_TEXT, "muted");
    return;
  }
  const [screen, accessibility, systemLaunch] = await Promise.all([
    settle(permissionGranted),
    settle(scrollAccessibilityGranted),
    settle(launchAtLogin),
  ]);
  if (sequence !== permissionRefreshSequence) return;

  if (screen.ok) {
    setPermissionStatusText(
      "permScreenStatus",
      screen.value ? "已授权 · 可以截取屏幕" : "未授权 · 无法读取屏幕内容",
      screen.value ? "ok" : "warn",
    );
  } else {
    setPermissionStatusText("permScreenStatus", "未授权 · 原生桥接未就绪", "muted");
  }

  if (accessibility.ok) {
    setPermissionStatusText(
      "permAccessibilityStatus",
      accessibility.value ? "已授权 · 自动滚动可用" : "未授权 · 自动滚动需手动",
      accessibility.value ? "ok" : "warn",
    );
  } else {
    setPermissionStatusText("permAccessibilityStatus", "未授权 · 原生桥接未就绪", "muted");
  }

  if (systemLaunch.ok) {
    if (systemLaunch.value) {
      setPermissionStatusText("permLaunchStatus", "已授权 · 系统登录项已生效", "ok");
    } else if (preferences.launch) {
      setPermissionStatusText(
        "permLaunchStatus",
        "未授权 · 已开启偏好，但系统登录项未生效",
        "warn",
      );
    } else {
      setPermissionStatusText("permLaunchStatus", "未授权 · 登录时不会启动", "muted");
    }
  } else {
    setPermissionStatusText("permLaunchStatus", "未授权 · 无法读取系统登录项状态", "muted");
  }
  if (note) note.textContent = launchPreferenceNote();
}

async function runPermissionAction(
  kind: PermissionSetting,
  action: string,
  button: HTMLButtonElement,
): Promise<void> {
  if (!hasNative) {
    showToast("浏览器预览 · 权限操作只在桌面应用中生效", "info");
    return;
  }
  setButtonPending(button, true);
  try {
    if (action === "screen-request") {
      let granted = false;
      try {
        granted = await requestScreenPermission();
      } catch {
        granted = false;
      }
      if (granted) {
        setButtonPending(button, false);
        flashButtonSuccess(button);
        showToast("已获得屏幕录制权限，可以截图了", "success");
      } else {
        showToast("请在系统设置中允许 Pinstill 录制屏幕", "info");
        await openPermissionSettings().catch(() => {});
      }
    } else if (action === "screen-open") {
      await openPermissionSettings();
      showToast("已打开系统设置 · 授权后点「重新检测」", "info");
    } else if (action === "accessibility-open") {
      await openScrollAccessibilitySettings();
      showToast("已打开辅助功能设置 · 授权后点「重新检测」", "info");
    } else if (action === "login-open") {
      await openLoginItemsSettings();
      showToast("已打开登录项设置", "info");
    } else {
      // screen-check / accessibility-check.
      showToast("正在重新检测权限…", "info");
    }
  } catch (error) {
    showToast(`权限操作未完成：${errorMessage(error)}`, "error");
  }
  // Keep the button pending until the status lines have actually re-read the
  // OS state (R7 review MINOR-2).
  await refreshPermissionCenter();
  await refreshPermissionStatus();
  void refreshPermissionBanner();
  setButtonPending(button, false);
}

function permissionBanner(): HTMLElement | null {
  return document.getElementById("permissionBanner");
}

/** Refreshes the main-window banner; hidden when granted, in-browser or dismissed. */
async function refreshPermissionBanner(): Promise<void> {
  const banner = permissionBanner();
  if (!banner) return;
  if (!hasNative || bannerDismissed) {
    banner.hidden = true;
    return;
  }
  try {
    banner.hidden = await permissionGranted();
  } catch {
    banner.hidden = false;
  }
}

async function handleBannerAction(action: string, button: HTMLButtonElement): Promise<void> {
  if (!hasNative) return;
  const banner = permissionBanner();
  if (action === "dismiss") {
    bannerDismissed = true;
    if (banner) banner.hidden = true;
    return;
  }
  setButtonPending(button, true);
  try {
    if (action === "grant") {
      let granted = false;
      try {
        granted = await requestScreenPermission();
      } catch {
        granted = false;
      }
      if (granted) {
        setButtonPending(button, false);
        flashButtonSuccess(button);
        showToast("已获得屏幕录制权限，可以截图了", "success");
      } else {
        showToast("请在系统设置中允许 Pinstill 录制屏幕", "info");
        await openPermissionSettings().catch(() => {});
      }
    } else if (action === "settings") {
      await openPermissionSettings();
      showToast("已打开系统设置 · 授权后回来即可截图", "info");
    }
  } catch (error) {
    showToast(`权限操作未完成：${errorMessage(error)}`, "error");
  } finally {
    setButtonPending(button, false);
  }
  await refreshPermissionBanner();
  await refreshPermissionStatus();
  void refreshPermissionCenter();
}

/** Settles a native permission reader without throwing (binding may be absent). */
async function settle<T>(
  read: () => Promise<T>,
): Promise<{ ok: true; value: T } | { ok: false; error: unknown }> {
  try {
    return { ok: true, value: await read() };
  } catch (error) {
    return { ok: false, error };
  }
}

// ---------------------------------------------------------------------------
// Mode-specific copy
// ---------------------------------------------------------------------------

function applyModeTexts(): void {
  if (!hasNative) return;
  const previewNote = document.getElementById("settingsPreviewNote");
  if (previewNote) {
    previewNote.textContent = "设置保存在本机，并即时同步到 Pinstill。";
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
document
  .querySelectorAll<HTMLButtonElement>("#permissionBanner [data-banner-action]")
  .forEach((button) => {
    button.addEventListener("click", () =>
      void handleBannerAction(button.dataset.bannerAction ?? "", button),
    );
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
el("archivedPinRefresh").addEventListener("click", () => void refreshArchivedPins());

// History preview actions + backdrop.
document
  .querySelectorAll<HTMLButtonElement>("#historyPreview [data-history-action]")
  .forEach((button) => {
    button.addEventListener("click", () =>
      void runPreviewAction(button.dataset.historyAction ?? "", button),
    );
  });
document
  .getElementById("historyPreviewBackdrop")
  ?.addEventListener("click", () => closeHistoryPreview());
el<HTMLImageElement>("historyPreviewImage").addEventListener("error", () => {
  showToast("截图原图加载失败", "error");
});

document.addEventListener("keydown", (event) => {
  if (recordingShortcut) {
    handleShortcutRecording(event);
    return;
  }
  if (previewVisible()) {
    if (event.key === "Escape") {
      event.preventDefault();
      closeHistoryPreview();
      return;
    }
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      stepHistoryPreview(-1);
      return;
    }
    if (event.key === "ArrowRight") {
      event.preventDefault();
      stepHistoryPreview(1);
      return;
    }
  }
  const target = event.target as HTMLElement | null;
  const tagName = target?.tagName ?? "";
  if (["INPUT", "TEXTAREA", "SELECT"].includes(tagName)) return;
});

// Native events (no-ops in the browser preview).
onCaptureCancelled(() => {
  showToast("已取消截取", "info");
});
onCaptureFailed((payload) => {
  showToast(payload.message || "截取失败", "error");
  if (payload.permission) {
    setPermissionLabel("需要屏幕录制权限", true);
    void refreshPermissionBanner();
    void refreshPermissionCenter();
  }
  // Go decides whether the window should be reopened (only when it was
  // visible before the capture); the page must not force it open.
});
onHistoryChanged(() => scheduleHistoryRefresh());
onToast((payload) => showToast(payload.text, (payload.kind as "info" | "success" | "error") ?? "info"));

// Initial state.
updateShortcutButton();
applyPreferences();
applyModeTexts();
const OPEN_SETTINGS_KEY = "pinstill.openSettingsCategory";

function consumePendingSettingsCategory(): boolean {
  let category = "";
  try {
    category = localStorage.getItem(OPEN_SETTINGS_KEY) ?? "";
    if (category) {
      if (typeof localStorage.removeItem === "function") {
        localStorage.removeItem(OPEN_SETTINGS_KEY);
      } else {
        localStorage.setItem(OPEN_SETTINGS_KEY, "");
      }
    }
  } catch {
    return false;
  }
  if (!category || !SETTINGS_SCHEMA.some((entry) => entry.id === category)) {
    return false;
  }
  settingsCategory = category;
  recordingShortcut = null;
  showView("settings");
  return true;
}

if (!consumePendingSettingsCategory()) {
  showView("history");
}
void refreshPermissionStatus();
void refreshPermissionBanner();
void pushNativeSettings();

window.addEventListener("storage", (event) => {
  if (event.key === OPEN_SETTINGS_KEY) {
    consumePendingSettingsCategory();
  }
});

// Keep the permission hint fresh when the app regains focus (e.g. after the
// user granted screen recording in System Settings).
window.addEventListener("focus", () => {
  consumePendingSettingsCategory();
  if (!hasNative) return;
  void refreshPermissionStatus();
  void refreshPermissionBanner();
  void refreshPermissionCenter();
});
