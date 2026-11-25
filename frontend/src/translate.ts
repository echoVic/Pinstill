// Standalone text translation window opened from the menu bar or ⌘⇧3.

import { cancelTranslation, copyRecognizedText, modelSettings, pollTranslation, startTranslation } from "./bridge";
import { TARGET_LANGUAGES } from "./languages";
import { loadPreferences, savePreferences } from "./settings";

const source = document.querySelector<HTMLTextAreaElement>("#source")!;
const result = document.querySelector<HTMLTextAreaElement>("#result")!;
const sourceLang = document.querySelector<HTMLSelectElement>("#source-lang")!;
const target = document.querySelector<HTMLSelectElement>("#target")!;
const swap = document.querySelector<HTMLButtonElement>("#swap")!;
const translateButton = document.querySelector<HTMLButtonElement>("#translate")!;
const clearButton = document.querySelector<HTMLButtonElement>("#clear")!;
const copyResult = document.querySelector<HTMLButtonElement>("#copy-result")!;
const copyBoth = document.querySelector<HTMLButtonElement>("#copy-both")!;
const status = document.querySelector<HTMLSpanElement>("#status")!;

const clipboardSignal = new AbortController().signal;
const preferences = loadPreferences();
let busy = false;

function setStatus(text: string, error = false): void {
  status.textContent = text;
  status.classList.toggle("error", error);
}

const SOURCE_KEY = "pinstill.translateSource";

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
target.value = preferences.translateTo;
try {
  const savedSource = localStorage.getItem(SOURCE_KEY);
  if (savedSource) sourceLang.value = savedSource;
} catch {
  // Storage may be unavailable; the default still applies.
}

target.addEventListener("change", () => {
  preferences.translateTo = target.value;
  savePreferences(preferences);
});
sourceLang.addEventListener("change", () => {
  try {
    localStorage.setItem(SOURCE_KEY, sourceLang.value);
  } catch {
    // Storage may be unavailable; the choice still applies for this session.
  }
});
swap.addEventListener("click", () => {
  const source = sourceLang.value;
  const current = target.value;
  if (source === "auto") {
    sourceLang.value = current;
    target.value = current === "en" ? "zh-Hans" : "en";
  } else {
    sourceLang.value = current;
    target.value = source;
  }
  preferences.translateTo = target.value;
  savePreferences(preferences);
  try {
    localStorage.setItem(SOURCE_KEY, sourceLang.value);
  } catch {
    // Storage may be unavailable.
  }
});

async function translate(): Promise<void> {
  if (busy) return;
  const text = source.value.trim();
  if (!text) {
    source.focus();
    setStatus("先输入要翻译的文字", true);
    return;
  }
  const settings = await modelSettings();
  if (settings.mode === "off" || !settings.revision) {
    setStatus("先在偏好设置 → 识别与翻译中配置模型服务", true);
    return;
  }
  busy = true;
  translateButton.disabled = true;
  result.value = "";
  setStatus(`正在翻译为 ${target.selectedOptions[0]?.textContent ?? target.value}…`);
  let token = "";
  try {
    token = await startTranslation(settings.revision, text, target.value, sourceLang.value);
    for (;;) {
      const update = await pollTranslation(token);
      result.value = update.text;
      if (update.error) throw new Error(update.error);
      if (update.done) break;
      await new Promise((resolve) => setTimeout(resolve, 120));
    }
    setStatus("");
  } catch (error) {
    setStatus(error instanceof Error ? error.message : String(error), true);
  } finally {
    if (token) void cancelTranslation(token);
    busy = false;
    translateButton.disabled = false;
  }
}

async function copy(text: string, label: string): Promise<void> {
  if (!text.trim()) return;
  try {
    await copyRecognizedText(text, clipboardSignal);
    setStatus(`已复制${label}`);
  } catch (error) {
    setStatus(error instanceof Error ? error.message : "复制失败", true);
  }
}

translateButton.addEventListener("click", () => void translate());
clearButton.addEventListener("click", () => {
  source.value = "";
  result.value = "";
  setStatus("");
  source.focus();
});
copyResult.addEventListener("click", () => void copy(result.value, "译文"));
copyBoth.addEventListener("click", () => {
  const text = `原文：\n${source.value.trim()}\n\n译文：\n${result.value.trim()}`;
  void copy(text, "对照");
});
source.addEventListener("keydown", (event) => {
  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
    event.preventDefault();
    void translate();
  }
});
source.focus();