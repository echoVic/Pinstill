// Standalone text translation window opened from the menu bar or ⌘⇧3.

import {
  cancelTranslation,
  copyRecognizedText,
  modelSettings,
  pollTranslation,
  recognizeTranslateImage,
  selectModelPreset,
  setTranslateAlwaysOnTop,
  showMainWindow,
  startTranslation,
  synthesizeSpeech,
  ttsSettings,
  type ModelSettings,
} from "./bridge";
import { TARGET_LANGUAGES, translateLanguageLabel } from "./languages";
import { loadPreferences, savePreferences } from "./settings";
import {
  clearUnstarredHistory,
  isTranslateHistoryEnabled,
  loadTranslateHistory,
  recordTranslateHistory,
  removeHistoryItem,
  setTranslateHistoryEnabled,
  toggleStarHistoryItem,
  type TranslateHistoryItem,
} from "./translate-history";
import { createSpeechController, type SpeechSlot } from "./translate-speech";
import {
  formatCharCount,
  isDictionaryQuery,
  loadGlossary,
  mergeBrokenLines,
  normalizeTranslationStyle,
  TRANSLATION_STYLES,
  type TranslationStyle,
} from "./translate-text";
import { blobToBase64, decodeImageBlob, flashButtonSuccess, setButtonPending } from "./util";

const source = document.querySelector<HTMLTextAreaElement>("#source")!;
const sourceField = document.querySelector<HTMLElement>("#source-field")!;
const sourceCount = document.querySelector<HTMLElement>("#source-count")!;
const result = document.querySelector<HTMLTextAreaElement>("#result")!;
const resultField = document.querySelector<HTMLElement>("#result-field")!;
const resultCount = document.querySelector<HTMLElement>("#result-count")!;
const sourceLang = document.querySelector<HTMLSelectElement>("#source-lang")!;
const target = document.querySelector<HTMLSelectElement>("#target")!;
const styleSelect = document.querySelector<HTMLSelectElement>("#style")!;
const presetSelect = document.querySelector<HTMLSelectElement>("#model-preset")!;
const pinButton = document.querySelector<HTMLButtonElement>("#pin-window")!;
const historyToggle = document.querySelector<HTMLButtonElement>("#history-toggle")!;
const historyDrawer = document.querySelector<HTMLElement>("#history-drawer")!;
const historyTabAll = document.querySelector<HTMLButtonElement>("#history-tab-all")!;
const historyTabStarred = document.querySelector<HTMLButtonElement>("#history-tab-starred")!;
const historyRecordToggle = document.querySelector<HTMLButtonElement>("#history-record-toggle")!;
const historyClear = document.querySelector<HTMLButtonElement>("#history-clear")!;
const historyClose = document.querySelector<HTMLButtonElement>("#history-close")!;
const historySearch = document.querySelector<HTMLInputElement>("#history-search")!;
const historyList = document.querySelector<HTMLElement>("#history-list")!;
const swap = document.querySelector<HTMLButtonElement>("#swap")!;
const translateButton = document.querySelector<HTMLButtonElement>("#translate")!;
const clearButton = document.querySelector<HTMLButtonElement>("#clear")!;
const speakSourceButton = document.querySelector<HTMLButtonElement>("#speak-source")!;
const autoMergeButton = document.querySelector<HTMLButtonElement>("#auto-merge")!;
const mergeLinesButton = document.querySelector<HTMLButtonElement>("#merge-lines")!;
const speakResultButton = document.querySelector<HTMLButtonElement>("#speak-result")!;
const starButton = document.querySelector<HTMLButtonElement>("#star-result")!;
const copyResult = document.querySelector<HTMLButtonElement>("#copy-result")!;
const copyBoth = document.querySelector<HTMLButtonElement>("#copy-both")!;
const openSettingsButton = document.querySelector<HTMLButtonElement>("#open-settings")!;
const status = document.querySelector<HTMLSpanElement>("#status")!;

const SOURCE_KEY = "pinstill.translateSource";
const STYLE_KEY = "pinstill.translateStyle";
const PIN_KEY = "pinstill.translatePinned";
const AUTO_MERGE_KEY = "pinstill.translateAutoMerge";
const OPEN_SETTINGS_KEY = "pinstill.openSettingsCategory";
const MAX_IMAGE_BYTES = 16 * 1024 * 1024;
const IMAGE_NAME_RE = /\.(png|jpe?g|webp|bmp|gif|tiff?|heic)$/i;

const clipboardSignal = new AbortController().signal;
const preferences = loadPreferences();

let busy = false;
let runId = 0;
let activeToken = "";
let activeAbort: AbortController | null = null;
let statusTimer = 0;
let modelAvailable = false;
let savedStyle: TranslationStyle = "standard";
let windowPinned = false;
let autoMerge = false;
let dragDepth = 0;
let currentHistoryId = "";
let currentStarred = false;
let historyFilter: "all" | "starred" = "all";
let historyOpen = false;
let historyEnabled = isTranslateHistoryEnabled();

interface StatusOptions {
  error?: boolean;
  loading?: boolean;
  showSettings?: boolean;
  autoClearMs?: number;
}

function setStatus(text: string, options: StatusOptions = {}): void {
  if (statusTimer) {
    window.clearTimeout(statusTimer);
    statusTimer = 0;
  }
  status.textContent = text;
  status.classList.toggle("error", Boolean(options.error));
  status.classList.toggle("is-loading", Boolean(options.loading));
  openSettingsButton.hidden = !options.showSettings;
  if (options.autoClearMs && text) {
    statusTimer = window.setTimeout(() => {
      statusTimer = 0;
      setStatus("");
    }, options.autoClearMs);
  }
}

const speech = createSpeechController({
  onChange: (active) => syncSpeechButtons(active),
  onError: (message) => setStatus(message, { error: true }),
  onSynthesizing: (slot) => {
    if (slot) {
      setStatus("正在合成语音…", { loading: true });
    } else if (!busy && status.textContent === "正在合成语音…") {
      setStatus("");
    }
  },
  synthesize: (revision, text, signal) => synthesizeSpeech(revision, text, signal),
});

function syncSpeechButton(
  button: HTMLButtonElement,
  slot: "source" | "result",
  hasText: boolean,
  active: SpeechSlot,
  defaultTitle: string,
): void {
  if (!speech.isAvailable()) {
    button.disabled = true;
    button.textContent = "朗读";
    button.classList.remove("is-speaking");
    button.setAttribute("aria-pressed", "false");
    button.title = "当前环境不支持语音朗读";
    return;
  }
  const isSpeaking = active === slot;
  button.disabled = !isSpeaking && !hasText;
  button.textContent = isSpeaking ? "停止" : "朗读";
  button.classList.toggle("is-speaking", isSpeaking);
  button.setAttribute("aria-pressed", isSpeaking ? "true" : "false");
  button.title = isSpeaking ? "停止朗读" : defaultTitle;
}

function syncSpeechButtons(active: SpeechSlot = speech.active()): void {
  syncSpeechButton(speakSourceButton, "source", source.value.trim().length > 0, active, "朗读原文");
  syncSpeechButton(speakResultButton, "result", result.value.trim().length > 0, active, "朗读译文");
}

function syncStarButton(): void {
  const canStar = source.value.trim().length > 0 && result.value.trim().length > 0;
  starButton.disabled = !canStar;
  starButton.textContent = currentStarred ? "★" : "☆";
  starButton.classList.toggle("is-starred", currentStarred);
  starButton.setAttribute("aria-pressed", currentStarred ? "true" : "false");
  starButton.title = currentStarred ? "取消收藏" : "收藏当前译文";
}

function syncActionState(): void {
  const hasSource = source.value.trim().length > 0;
  const hasResult = result.value.trim().length > 0;
  sourceCount.textContent = formatCharCount(source.value);
  resultCount.textContent = formatCharCount(result.value);
  mergeLinesButton.disabled =
    !source.value.includes("\n") || mergeBrokenLines(source.value) === source.value;
  copyResult.disabled = !hasResult;
  copyBoth.disabled = !hasSource || !hasResult;
  syncStarButton();
  syncSpeechButtons();
}

function applyPinnedState(pinned: boolean): void {
  windowPinned = pinned;
  pinButton.classList.toggle("is-pinned", pinned);
  pinButton.setAttribute("aria-pressed", pinned ? "true" : "false");
  pinButton.title = pinned ? "取消窗口置顶" : "窗口置顶";
}

function applyAutoMergeState(enabled: boolean): void {
  autoMerge = enabled;
  autoMergeButton.classList.toggle("is-active", enabled);
  autoMergeButton.setAttribute("aria-pressed", enabled ? "true" : "false");
}

function syncStyleAvailability(available: boolean): void {
  modelAvailable = available;
  styleSelect.disabled = !available;
  if (available) {
    styleSelect.value = savedStyle;
    styleSelect.title = "翻译风格（由大模型驱动）";
  } else {
    styleSelect.value = "standard";
    styleSelect.title = "配置大模型服务后可选择翻译风格";
  }
}

function syncModelState(settings: ModelSettings | null): void {
  const hasModel = Boolean(settings && settings.mode !== "off" && settings.revision);
  syncStyleAvailability(hasModel);
  const presets = settings?.presets ?? [];
  presetSelect.replaceChildren();
  if (presets.length === 0) {
    presetSelect.hidden = true;
    return;
  }
  presetSelect.hidden = false;
  if (!settings?.activeId) {
    const offOption = document.createElement("option");
    offOption.value = "";
    offOption.textContent = "未启用模型";
    offOption.disabled = true;
    presetSelect.append(offOption);
  }
  for (const preset of presets) {
    const option = document.createElement("option");
    option.value = preset.id;
    option.textContent = preset.name || preset.model;
    presetSelect.append(option);
  }
  presetSelect.value = settings?.activeId ?? "";
}

async function refreshModelAvailability(): Promise<void> {
  try {
    const settings = await modelSettings();
    syncModelState(settings);
  } catch {
    syncModelState(null);
  }
}

async function refreshTTSAvailability(): Promise<void> {
  try {
    const settings = await ttsSettings();
    speech.setRemote(settings.provider !== "system" && settings.revision ? settings.revision : null);
  } catch {
    speech.setRemote(null);
  }
  syncSpeechButtons();
}

function setTranslating(active: boolean): void {
  busy = active;
  translateButton.classList.toggle("is-stop", active);
  translateButton.classList.toggle("primary", !active);
  translateButton.textContent = active ? "停止" : "翻译";
  translateButton.setAttribute("aria-busy", active ? "true" : "false");
  resultField.classList.toggle("is-streaming", active);
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const timer = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      window.clearTimeout(timer);
      resolve();
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function styleLabel(code: TranslationStyle): string {
  return TRANSLATION_STYLES.find((item) => item.code === code)?.label ?? "标准";
}

function syncHistoryHeader(): void {
  historyToggle.classList.toggle("is-pinned", historyOpen);
  historyToggle.setAttribute("aria-pressed", historyOpen ? "true" : "false");
  historyTabAll.classList.toggle("is-active", historyFilter === "all");
  historyTabStarred.classList.toggle("is-active", historyFilter === "starred");
  historyRecordToggle.classList.toggle("is-active", historyEnabled);
  historyRecordToggle.setAttribute("aria-pressed", historyEnabled ? "true" : "false");
  historyRecordToggle.textContent = historyEnabled ? "记录" : "已暂停";
}

function restoreHistoryItem(item: TranslateHistoryItem): void {
  if (busy) stopTranslation(true);
  speech.stop();
  sourceLang.value = item.sourceLang;
  target.value = item.targetLang;
  preferences.translateTo = item.targetLang;
  savePreferences(preferences);
  try {
    localStorage.setItem(SOURCE_KEY, item.sourceLang);
  } catch {
    // Storage may be unavailable.
  }
  if (modelAvailable) {
    savedStyle = item.style;
    styleSelect.value = item.style;
  }
  source.value = item.source;
  result.value = item.result;
  currentHistoryId = item.id;
  currentStarred = item.starred;
  syncActionState();
  setHistoryOpen(false);
}

function renderHistoryDrawer(): void {
  syncHistoryHeader();
  if (!historyOpen) return;
  const query = historySearch.value.trim().toLowerCase();
  const items = loadTranslateHistory().filter((item) => {
    if (historyFilter === "starred" && !item.starred) return false;
    if (!query) return true;
    return (
      item.source.toLowerCase().includes(query) ||
      item.result.toLowerCase().includes(query)
    );
  });
  historyList.replaceChildren();
  if (items.length === 0) {
    const empty = document.createElement("p");
    empty.className = "history-empty";
    empty.textContent =
      historyFilter === "starred" ? "还没有收藏的译文。" : "暂无翻译记录。";
    historyList.append(empty);
    return;
  }
  for (const item of items) {
    const row = document.createElement("div");
    row.className = "history-item";
    row.setAttribute("data-history-id", item.id);

    const top = document.createElement("div");
    top.className = "history-item-top";
    const meta = document.createElement("span");
    meta.className = "history-item-meta";
    const srcLabel = item.sourceLang === "auto" ? "自动" : translateLanguageLabel(item.sourceLang);
    meta.textContent = `${srcLabel} → ${translateLanguageLabel(item.targetLang)} · ${styleLabel(item.style)}`;

    const itemActions = document.createElement("div");
    itemActions.className = "history-item-actions";

    const starBtn = document.createElement("button");
    starBtn.type = "button";
    starBtn.textContent = item.starred ? "★" : "☆";
    starBtn.className = item.starred ? "star-btn is-starred" : "star-btn";
    starBtn.setAttribute("data-history-star", item.id);
    starBtn.setAttribute("aria-label", item.starred ? "取消收藏" : "收藏");
    starBtn.addEventListener("click", (event) => {
      event.stopPropagation();
      const updated = toggleStarHistoryItem(item.id);
      if (updated && updated.id === currentHistoryId) {
        currentStarred = updated.starred;
        syncStarButton();
      }
      renderHistoryDrawer();
    });

    const delBtn = document.createElement("button");
    delBtn.type = "button";
    delBtn.textContent = "×";
    delBtn.setAttribute("data-history-delete", item.id);
    delBtn.setAttribute("aria-label", "删除记录");
    delBtn.addEventListener("click", (event) => {
      event.stopPropagation();
      removeHistoryItem(item.id);
      if (item.id === currentHistoryId) {
        currentHistoryId = "";
        currentStarred = false;
        syncStarButton();
      }
      renderHistoryDrawer();
    });

    itemActions.append(starBtn, delBtn);
    top.append(meta, itemActions);

    const srcEl = document.createElement("div");
    srcEl.className = "history-item-source";
    srcEl.textContent = item.source;
    const resEl = document.createElement("div");
    resEl.className = "history-item-result";
    resEl.textContent = item.result;

    row.append(top, srcEl, resEl);
    row.addEventListener("click", () => restoreHistoryItem(item));
    historyList.append(row);
  }
}

function setHistoryOpen(open: boolean): void {
  historyOpen = open;
  historyDrawer.hidden = !open;
  syncHistoryHeader();
  if (open) renderHistoryDrawer();
}

for (const language of TARGET_LANGUAGES) {
  const option = document.createElement("option");
  option.value = language.code;
  option.textContent = language.label;
  target.append(option);
}
for (const language of [{ code: "auto", label: "自动检测" }, ...TARGET_LANGUAGES]) {
  const option = document.createElement("option");
  option.value = language.code;
  option.textContent = language.label;
  sourceLang.append(option);
}
for (const style of TRANSLATION_STYLES) {
  const option = document.createElement("option");
  option.value = style.code;
  option.textContent = style.label;
  styleSelect.append(option);
}
target.value = preferences.translateTo;
try {
  const savedSource = localStorage.getItem(SOURCE_KEY);
  if (savedSource) sourceLang.value = savedSource;
  savedStyle = normalizeTranslationStyle(localStorage.getItem(STYLE_KEY));
  windowPinned = localStorage.getItem(PIN_KEY) === "1";
  autoMerge = localStorage.getItem(AUTO_MERGE_KEY) === "1";
} catch {
  // Storage may be unavailable; the defaults still apply.
}

function stopTranslation(silent = false): void {
  if (!busy) return;
  runId += 1;
  const token = activeToken;
  const abort = activeAbort;
  activeToken = "";
  activeAbort = null;
  abort?.abort();
  if (token) void cancelTranslation(token);
  setTranslating(false);
  syncActionState();
  if (!silent) {
    setStatus("已停止翻译", { autoClearMs: 2000 });
  }
}

function invalidateTranslation(): void {
  if (busy) stopTranslation(true);
  speech.stop();
  result.value = "";
  currentHistoryId = "";
  currentStarred = false;
  syncActionState();
  setStatus("");
}

async function translate(): Promise<void> {
  if (busy) return;
  const text = source.value.trim();
  if (!text) {
    source.focus();
    setStatus("先输入要翻译的文字", { error: true });
    return;
  }
  speech.stop();
  const myRun = ++runId;
  const abort = new AbortController();
  activeAbort = abort;
  activeToken = "";
  currentHistoryId = "";
  currentStarred = false;
  setTranslating(true);
  setStatus("正在读取模型配置…", { loading: true });
  let token = "";
  let completed = false;
  let activeStyle: TranslationStyle = "standard";
  try {
    const settings = await modelSettings();
    if (myRun !== runId || abort.signal.aborted) return;
    syncModelState(settings);
    if (!modelAvailable) {
      setStatus("先在偏好设置 → 识别与翻译中配置模型服务", {
        error: true,
        showSettings: true,
      });
      return;
    }
    activeStyle = modelAvailable
      ? normalizeTranslationStyle(styleSelect.value)
      : "standard";
    const glossary = loadGlossary();
    const dictionaryMode = activeStyle === "standard" && isDictionaryQuery(text);
    result.value = "";
    syncActionState();
    setStatus(
      dictionaryMode
        ? `正在查询词典（${translateLanguageLabel(target.value)}）…`
        : `正在翻译为 ${translateLanguageLabel(target.value)}…`,
      { loading: true },
    );
    token = await startTranslation(
      settings.revision,
      text,
      target.value,
      sourceLang.value,
      activeStyle,
      glossary,
    );
    if (myRun !== runId || abort.signal.aborted) {
      if (token) {
        void cancelTranslation(token);
        token = "";
      }
      return;
    }
    activeToken = token;
    for (;;) {
      const update = await pollTranslation(token);
      if (myRun !== runId || abort.signal.aborted) return;
      const prevLength = result.value.length;
      const scrollHeight = result.scrollHeight || 0;
      const clientHeight = result.clientHeight || 0;
      const stickToBottom =
        scrollHeight <= clientHeight ||
        scrollHeight - (result.scrollTop || 0) - clientHeight <= 24;
      result.value = update.text;
      syncActionState();
      if (stickToBottom && result.value.length > prevLength) {
        result.scrollTop = result.scrollHeight || 0;
      }
      if (update.error) throw new Error(update.error);
      if (update.done) break;
      await sleep(120, abort.signal);
      if (myRun !== runId || abort.signal.aborted) return;
    }
    completed = true;
    setStatus("");
  } catch (error) {
    if (myRun !== runId || abort.signal.aborted) return;
    setStatus(error instanceof Error ? error.message : String(error), { error: true });
  } finally {
    if (myRun === runId) {
      if (token && activeToken === token) {
        void cancelTranslation(token);
      }
      activeToken = "";
      activeAbort = null;
      setTranslating(false);
      if (completed && historyEnabled && result.value.trim()) {
        const recorded = recordTranslateHistory({
          source: text,
          result: result.value,
          sourceLang: sourceLang.value || "auto",
          targetLang: target.value,
          style: activeStyle,
        });
        currentHistoryId = recorded.id;
        currentStarred = recorded.starred;
        if (historyOpen) renderHistoryDrawer();
      }
      syncActionState();
    }
  }
}

async function imageBlobToPNGBase64(blob: Blob): Promise<string> {
  if (blob.size > MAX_IMAGE_BYTES) {
    throw new Error("图片过大，请选择 16 MiB 以内的图片");
  }
  if (blob.type === "image/png") {
    return blobToBase64(blob);
  }
  const decoded = await decodeImageBlob(blob);
  try {
    const width = "naturalWidth" in decoded ? decoded.naturalWidth : decoded.width;
    const height = "naturalHeight" in decoded ? decoded.naturalHeight : decoded.height;
    if (!width || !height) {
      throw new Error("无法读取图片尺寸");
    }
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("无法处理图片");
    ctx.drawImage(decoded, 0, 0);
    const pngBlob = await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((out) => {
        if (out) resolve(out);
        else reject(new Error("无法编码 PNG 图片"));
      }, "image/png");
    });
    return await blobToBase64(pngBlob);
  } finally {
    if ("close" in decoded && typeof decoded.close === "function") {
      decoded.close();
    }
  }
}

function extractImageFromDataTransfer(dt: DataTransfer | null | undefined): Blob | null {
  if (!dt) return null;
  if (dt.items) {
    for (const item of Array.from(dt.items)) {
      if (item.kind === "file" && item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (file) return file;
      }
    }
  }
  if (dt.files) {
    for (const file of Array.from(dt.files)) {
      if (file.type.startsWith("image/") || IMAGE_NAME_RE.test(file.name)) {
        return file;
      }
    }
  }
  return null;
}

function hasImageDrag(dt: DataTransfer | null | undefined): boolean {
  if (!dt) return false;
  if (dt.items) {
    for (const item of Array.from(dt.items)) {
      if (item.kind === "file" && (!item.type || item.type.startsWith("image/"))) {
        return true;
      }
    }
  }
  if (dt.files && dt.files.length > 0) return true;
  return Array.from(dt.types ?? []).includes("Files");
}

async function handleImageFile(file: Blob): Promise<void> {
  if (busy) stopTranslation(true);
  speech.stop();
  const myRun = ++runId;
  const abort = new AbortController();
  activeAbort = abort;
  activeToken = "";
  currentHistoryId = "";
  currentStarred = false;
  setTranslating(true);
  setStatus("正在识别图片中的文字…", { loading: true });
  try {
    const pngBase64 = await imageBlobToPNGBase64(file);
    if (myRun !== runId || abort.signal.aborted) return;
    const rawExtracted = (
      await recognizeTranslateImage(preferences.ocrLanguage, pngBase64, abort.signal)
    ).trim();
    if (myRun !== runId || abort.signal.aborted) return;
    if (!rawExtracted) {
      setStatus("图片中没有识别到文字", { error: true });
      return;
    }
    source.value = autoMerge ? mergeBrokenLines(rawExtracted) : rawExtracted;
    result.value = "";
    syncActionState();
    activeAbort = null;
    setTranslating(false);
    void translate();
  } catch (error) {
    if (myRun !== runId || abort.signal.aborted) return;
    setStatus(error instanceof Error ? error.message : String(error), { error: true });
  } finally {
    if (myRun === runId) {
      activeAbort = null;
      setTranslating(false);
      syncActionState();
    }
  }
}

async function copy(text: string, label: string, button: HTMLButtonElement): Promise<void> {
  if (!text.trim()) return;
  setButtonPending(button, true);
  try {
    await copyRecognizedText(text, clipboardSignal);
    setButtonPending(button, false);
    flashButtonSuccess(button);
    setStatus(`已复制${label}`, { autoClearMs: 2000 });
  } catch (error) {
    setButtonPending(button, false);
    setStatus(error instanceof Error ? error.message : "复制失败", { error: true });
  } finally {
    syncActionState();
  }
}

target.addEventListener("change", () => {
  preferences.translateTo = target.value;
  savePreferences(preferences);
  if (busy) {
    stopTranslation(true);
    if (source.value.trim()) void translate();
  } else if (source.value.trim() && result.value.trim()) {
    void translate();
  }
});

sourceLang.addEventListener("change", () => {
  try {
    localStorage.setItem(SOURCE_KEY, sourceLang.value);
  } catch {
    // Storage may be unavailable; the choice still applies for this session.
  }
  invalidateTranslation();
});

styleSelect.addEventListener("change", () => {
  if (!modelAvailable) {
    styleSelect.value = "standard";
    return;
  }
  savedStyle = normalizeTranslationStyle(styleSelect.value);
  try {
    localStorage.setItem(STYLE_KEY, savedStyle);
  } catch {
    // Storage may be unavailable; the choice still applies for this session.
  }
  if (busy) {
    stopTranslation(true);
    if (source.value.trim()) void translate();
  } else if (source.value.trim() && result.value.trim()) {
    void translate();
  }
});

presetSelect.addEventListener("change", async () => {
  const selectedId = presetSelect.value;
  if (!selectedId) return;
  if (busy) stopTranslation(true);
  try {
    const updated = await selectModelPreset(selectedId);
    syncModelState(updated);
    const activeName = updated.presets?.find((entry) => entry.id === updated.activeId)?.name;
    setStatus(`已切换模型：${activeName || updated.model}`, { autoClearMs: 1800 });
    if (source.value.trim() && result.value.trim()) {
      void translate();
    }
  } catch (error) {
    setStatus(error instanceof Error ? error.message : String(error), { error: true });
    void refreshModelAvailability();
  }
});

pinButton.addEventListener("click", async () => {
  const next = !windowPinned;
  try {
    const applied = await setTranslateAlwaysOnTop(next);
    applyPinnedState(applied);
    try {
      localStorage.setItem(PIN_KEY, applied ? "1" : "0");
    } catch {
      // Storage may be unavailable.
    }
    setStatus(applied ? "已置顶窗口" : "已取消置顶", { autoClearMs: 1600 });
  } catch (error) {
    setStatus(error instanceof Error ? error.message : String(error), { error: true });
  }
});

autoMergeButton.addEventListener("click", () => {
  const next = !autoMerge;
  applyAutoMergeState(next);
  try {
    localStorage.setItem(AUTO_MERGE_KEY, next ? "1" : "0");
  } catch {
    // Storage may be unavailable.
  }
  setStatus(next ? "已开启自动合行" : "已关闭自动合行", { autoClearMs: 1600 });
});

historyToggle.addEventListener("click", () => {
  setHistoryOpen(!historyOpen);
});
historyClose.addEventListener("click", () => setHistoryOpen(false));
historyTabAll.addEventListener("click", () => {
  historyFilter = "all";
  renderHistoryDrawer();
});
historyTabStarred.addEventListener("click", () => {
  historyFilter = "starred";
  renderHistoryDrawer();
});
historySearch.addEventListener("input", () => renderHistoryDrawer());
historyRecordToggle.addEventListener("click", () => {
  historyEnabled = !historyEnabled;
  setTranslateHistoryEnabled(historyEnabled);
  syncHistoryHeader();
  setStatus(historyEnabled ? "已开启翻译记录" : "已暂停翻译记录", { autoClearMs: 1800 });
});
historyClear.addEventListener("click", () => {
  clearUnstarredHistory();
  if (currentHistoryId && !currentStarred) {
    currentHistoryId = "";
    syncStarButton();
  }
  renderHistoryDrawer();
  setStatus("已清空未收藏历史", { autoClearMs: 1800 });
});

starButton.addEventListener("click", () => {
  const srcText = source.value.trim();
  const resText = result.value.trim();
  if (!srcText || !resText) return;
  if (currentHistoryId) {
    const updated = toggleStarHistoryItem(currentHistoryId);
    if (updated) {
      currentStarred = updated.starred;
    } else {
      const created = recordTranslateHistory(
        {
          source: srcText,
          result: resText,
          sourceLang: sourceLang.value || "auto",
          targetLang: target.value,
          style: modelAvailable ? normalizeTranslationStyle(styleSelect.value) : "standard",
        },
        { forceStar: true },
      );
      currentHistoryId = created.id;
      currentStarred = created.starred;
    }
  } else {
    const created = recordTranslateHistory(
      {
        source: srcText,
        result: resText,
        sourceLang: sourceLang.value || "auto",
        targetLang: target.value,
        style: modelAvailable ? normalizeTranslationStyle(styleSelect.value) : "standard",
      },
      { forceStar: true },
    );
    currentHistoryId = created.id;
    currentStarred = created.starred;
  }
  syncStarButton();
  flashButtonSuccess(starButton);
  if (historyOpen) renderHistoryDrawer();
  setStatus(currentStarred ? "已收藏" : "已取消收藏", { autoClearMs: 1600 });
});

swap.addEventListener("click", () => {
  const hadResult = result.value.trim();
  const wasBusy = busy;
  if (wasBusy) stopTranslation(true);
  const currentSource = sourceLang.value;
  const currentTarget = target.value;
  if (currentSource === "auto") {
    sourceLang.value = currentTarget;
    target.value = currentTarget === "en" ? "zh-Hans" : "en";
  } else {
    sourceLang.value = currentTarget;
    target.value = currentSource;
  }
  preferences.translateTo = target.value;
  savePreferences(preferences);
  try {
    localStorage.setItem(SOURCE_KEY, sourceLang.value);
  } catch {
    // Storage may be unavailable.
  }
  currentHistoryId = "";
  currentStarred = false;
  if (hadResult) {
    source.value = hadResult;
    result.value = "";
    syncActionState();
    void translate();
  } else if (wasBusy && source.value.trim()) {
    void translate();
  }
});

translateButton.addEventListener("click", () => {
  if (busy) stopTranslation();
  else void translate();
});

clearButton.addEventListener("click", () => {
  if (busy) stopTranslation(true);
  speech.stop();
  source.value = "";
  result.value = "";
  currentHistoryId = "";
  currentStarred = false;
  syncActionState();
  setStatus("");
  source.focus();
});

speakSourceButton.addEventListener("click", () => {
  speech.speak("source", source.value, sourceLang.value);
});

speakResultButton.addEventListener("click", () => {
  speech.speak("result", result.value, target.value);
});

mergeLinesButton.addEventListener("click", () => {
  const merged = mergeBrokenLines(source.value);
  if (merged === source.value) {
    setStatus("没有可合并的断行", { autoClearMs: 1800 });
    return;
  }
  source.value = merged;
  invalidateTranslation();
  flashButtonSuccess(mergeLinesButton);
  setStatus("已合并断行", { autoClearMs: 2000 });
  source.focus();
});

openSettingsButton.addEventListener("click", () => {
  try {
    localStorage.setItem(OPEN_SETTINGS_KEY, "recognition");
  } catch {
    // Storage may be unavailable.
  }
  void showMainWindow();
});

copyResult.addEventListener("click", () => {
  if (!result.value.trim()) {
    setStatus("还没有译文", { error: true });
    return;
  }
  void copy(result.value, "译文", copyResult);
});

copyBoth.addEventListener("click", () => {
  if (!source.value.trim() || !result.value.trim()) {
    setStatus("还没有译文", { error: true });
    return;
  }
  const text = `原文：\n${source.value.trim()}\n\n译文：\n${result.value.trim()}`;
  void copy(text, "对照", copyBoth);
});

source.addEventListener("input", invalidateTranslation);
source.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
    event.preventDefault();
    if (busy) stopTranslation(true);
    void translate();
  }
});

document.addEventListener("paste", (event) => {
  const image = extractImageFromDataTransfer(event.clipboardData);
  if (image) {
    event.preventDefault();
    void handleImageFile(image);
    return;
  }
  if (
    autoMerge &&
    (event.target === source || document.activeElement === source) &&
    typeof event.clipboardData?.getData === "function"
  ) {
    const pasted = event.clipboardData.getData("text/plain");
    if (pasted && pasted.includes("\n")) {
      const merged = mergeBrokenLines(pasted);
      if (merged !== pasted) {
        event.preventDefault();
        if (typeof source.selectionStart === "number" && typeof source.selectionEnd === "number") {
          const start = source.selectionStart;
          const end = source.selectionEnd;
          source.value = source.value.slice(0, start) + merged + source.value.slice(end);
          const caret = start + merged.length;
          source.setSelectionRange(caret, caret);
        } else {
          source.value = merged;
        }
        invalidateTranslation();
        setStatus("已自动合并断行", { autoClearMs: 1600 });
      }
    }
  }
});

document.addEventListener("dragenter", (event) => {
  if (!hasImageDrag(event.dataTransfer)) return;
  event.preventDefault();
  dragDepth += 1;
  sourceField.classList.add("is-dragover");
});

document.addEventListener("dragover", (event) => {
  if (!hasImageDrag(event.dataTransfer)) return;
  event.preventDefault();
});

document.addEventListener("dragleave", () => {
  if (dragDepth > 0) dragDepth -= 1;
  if (dragDepth === 0) sourceField.classList.remove("is-dragover");
});

document.addEventListener("drop", (event) => {
  dragDepth = 0;
  sourceField.classList.remove("is-dragover");
  const image = extractImageFromDataTransfer(event.dataTransfer);
  if (!image) return;
  event.preventDefault();
  void handleImageFile(image);
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (busy) {
    event.preventDefault();
    stopTranslation();
  } else if (historyOpen) {
    event.preventDefault();
    setHistoryOpen(false);
  } else if (speech.active() !== null) {
    event.preventDefault();
    speech.stop();
  }
});

document.addEventListener("visibilitychange", () => {
  if (document.hidden) {
    if (busy) stopTranslation(true);
    speech.stop();
  }
});

window.addEventListener("pagehide", () => {
  speech.stop();
});

window.addEventListener("focus", () => {
  void refreshModelAvailability();
  void refreshTTSAvailability();
  if (
    !historyOpen &&
    document.activeElement !== result &&
    document.activeElement !== sourceLang &&
    document.activeElement !== target &&
    document.activeElement !== styleSelect &&
    document.activeElement !== presetSelect
  ) {
    source.focus();
  }
});

applyPinnedState(windowPinned);
applyAutoMergeState(autoMerge);
syncHistoryHeader();
if (windowPinned) {
  void setTranslateAlwaysOnTop(true)
    .then(applyPinnedState)
    .catch(() => applyPinnedState(false));
}
syncStyleAvailability(false);
syncActionState();
void refreshModelAvailability();
void refreshTTSAvailability();
source.focus();
