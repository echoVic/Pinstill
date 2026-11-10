// Preferences: the UI schema of the settings categories, localStorage
// persistence, and the mapping to the native `Settings` object of plan §3.4.
//
// The UI stores more preferences than Go needs (tooltips, editor keys, …);
// only the contract fields are pushed to `Capture.ApplySettings`.

import type { Settings } from "./bridge";
import { DEFAULT_TOOL_CHOICES, toolName } from "./tools";

export const STORAGE_KEY = "pinstill.preferences";

/** Everything the design's settings UI persists locally. */
export interface Preferences {
  // 通用与截图
  launch: boolean;
  cursor: boolean;
  sound: boolean;
  afterCapture: "inplace" | "clipboard" | "pin";
  tooltip: boolean;
  tooltipDelay: number;
  // 快捷键
  captureKey: string;
  pinKey: string;
  ocrKey: string;
  enterCopy: boolean;
  // 保存与历史
  folder: string;
  format: "PNG" | "JPEG";
  quality: number;
  filenamePrefix: string;
  keepHistory: boolean;
  retentionDays: number;
  historyLimitMB: number;
  // 标注与水印
  defaultTool: string;
  defaultColor: string;
  defaultWidth: number;
  watermarkText: string;
  watermarkOpacity: number;
  // 贴图与桌面
  pinOpacity: number;
  pinBorder: boolean;
  // 识别与翻译
  ocrLanguage: string;
  ocrCopy: boolean;
  qrConfirm: boolean;
  translateTo: string;
  translationProvider: string;
  sideBySide: boolean;
}

interface RowBase {
  key: string;
  name: string;
  description: string;
}

/** The three OS-level permissions the permission centre reports (R7.1). */
export type PermissionSetting = "screen" | "accessibility" | "launch";

export type SettingRow =
  | (RowBase & { type: "toggle" })
  | (RowBase & {
      type: "select";
      options: ReadonlyArray<{ value: string; label: string }>;
      coerce?: "number";
    })
  | (RowBase & { type: "range"; min: number; max: number; step: number; unit: string })
  | (RowBase & { type: "shortcut" })
  | (RowBase & { type: "color" })
  | (RowBase & { type: "text" })
  | (RowBase & { type: "permission"; perm: PermissionSetting })
  | (RowBase & { type: "info"; text: string });

export interface SettingGroup {
  title: string;
  rows: SettingRow[];
}

export interface SettingsCategory {
  id: string;
  name: string;
  icon: string;
  description: string;
  groups: SettingGroup[];
}

const toolOptions = DEFAULT_TOOL_CHOICES.map((id) => ({ value: id, label: toolName(id) }));

/** The seven categories of the settings view (six design ones + 权限). */
export const SETTINGS_SCHEMA: SettingsCategory[] = [
  {
    id: "general",
    name: "通用与截图",
    icon: "drag",
    description: "从按下快捷键开始，让每次截图都更顺手。",
    groups: [
      {
        title: "启动与截图",
        rows: [
          {
            key: "launch",
            name: "登录后启动 Pinstill",
            description: "下次打开电脑时，自动在后台待命。",
            type: "toggle",
          },
          {
            key: "cursor",
            name: "截取鼠标指针",
            description: "将鼠标所在位置一起保留在截图里。",
            type: "toggle",
          },
          {
            key: "sound",
            name: "截图提示音",
            description: "截取完成时播放轻提示音。",
            type: "toggle",
          },
          {
            key: "afterCapture",
            name: "截图后",
            description: "选择截取完成后的默认动作。",
            type: "select",
            options: [
              { value: "inplace", label: "就地标注" },
              { value: "clipboard", label: "复制到剪贴板" },
              { value: "pin", label: "固定到屏幕" },
            ],
          },
        ],
      },
      {
        title: "工具栏",
        rows: [
          {
            key: "tooltip",
            name: "图标文字提示",
            description: "悬停时显示工具名称，不占用工具栏空间。",
            type: "toggle",
          },
          {
            key: "tooltipDelay",
            name: "提示出现延迟",
            description: "更快找到不熟悉的工具。",
            type: "range",
            min: 0,
            max: 600,
            step: 20,
            unit: "ms",
          },
        ],
      },
    ],
  },
  {
    id: "shortcuts",
    name: "快捷键",
    icon: "text",
    description: "常用动作，一次按键就到。点击组合键后可直接录入。",
    groups: [
      {
        title: "全局快捷键",
        rows: [
          {
            key: "captureKey",
            name: "区域截图",
            description: "拖动选择一个区域并开始标注。",
            type: "shortcut",
          },
          {
            key: "pinKey",
            name: "固定剪贴板图片",
            description: "将剪贴板中的图片固定到桌面。",
            type: "shortcut",
          },
          {
            key: "ocrKey",
            name: "提取屏幕文字",
            description: "选择区域后直接提取其中的文字。",
            type: "shortcut",
          },
        ],
      },
      {
        title: "编辑时",
        rows: [
          {
            key: "enterCopy",
            name: "Enter 保存到剪贴板",
            description: "完成标注后，按 Enter 复制当前截图。",
            type: "toggle",
          },
          {
            key: "editorKeys",
            name: "常用编辑按键",
            description: "撤销：⌘ / Ctrl Z　·　取消：Esc",
            type: "info",
            text: "固定按键",
          },
        ],
      },
    ],
  },
  {
    id: "saving",
    name: "保存与历史",
    icon: "download",
    description: "为截图安排好去处，随时回到上一次灵感。",
    groups: [
      {
        title: "文件导出",
        rows: [
          {
            key: "folder",
            name: "默认保存文件夹",
            description: "导出与固定到屏幕时使用的文件夹，支持 ~ 展开。",
            type: "text",
          },
          {
            key: "format",
            name: "图片格式",
            description: "PNG 保留清晰细节，JPEG 更适合控制体积。",
            type: "select",
            options: [
              { value: "PNG", label: "PNG" },
              { value: "JPEG", label: "JPEG" },
            ],
          },
          {
            key: "quality",
            name: "JPEG 图片质量",
            description: "仅用于 JPEG 导出。",
            type: "range",
            min: 50,
            max: 100,
            step: 5,
            unit: "%",
          },
          {
            key: "filenamePrefix",
            name: "文件名前缀",
            description: "导出时自动附加当天日期。",
            type: "text",
          },
        ],
      },
      {
        title: "历史记录",
        rows: [
          {
            key: "keepHistory",
            name: "保留截图历史",
            description: "在本机保存截图，方便再次标注与导出。",
            type: "toggle",
          },
          {
            key: "retentionDays",
            name: "自动清理",
            description: "超过保留时间后自动清理较旧的截图。",
            type: "select",
            coerce: "number",
            options: [
              { value: "7", label: "7 天" },
              { value: "30", label: "30 天" },
              { value: "90", label: "90 天" },
              { value: "0", label: "永不" },
            ],
          },
          {
            key: "historyLimitMB",
            name: "历史容量上限",
            description: "达到上限时优先清理较早的截图。",
            type: "select",
            coerce: "number",
            options: [
              { value: "500", label: "500 MB" },
              { value: "2048", label: "2 GB" },
              { value: "5120", label: "5 GB" },
            ],
          },
        ],
      },
    ],
  },
  {
    id: "annotation",
    name: "标注与水印",
    icon: "brush",
    description: "预设常用样式，截完就能开始表达。",
    groups: [
      {
        title: "默认标注",
        rows: [
          {
            key: "defaultTool",
            name: "默认工具",
            description: "点击“截一张”时使用的标注工具。",
            type: "select",
            options: toolOptions,
          },
          {
            key: "defaultColor",
            name: "标注颜色",
            description: "应用到接下来绘制的图形与文字。",
            type: "color",
          },
          {
            key: "defaultWidth",
            name: "画笔粗细",
            description: "与编辑器中的粗细选项同步。",
            type: "range",
            min: 1,
            max: 10,
            step: 1,
            unit: "px",
          },
        ],
      },
      {
        title: "水印",
        rows: [
          {
            key: "watermarkText",
            name: "默认水印文字",
            description: "切换水印工具后，点击截图放置。",
            type: "text",
          },
          {
            key: "watermarkOpacity",
            name: "水印透明度",
            description: "轻量标记，保留画面内容。",
            type: "range",
            min: 10,
            max: 90,
            step: 5,
            unit: "%",
          },
        ],
      },
    ],
  },
  {
    id: "pinning",
    name: "贴图与桌面",
    icon: "pin",
    description: "把截图钉在桌面最上层，随时对照手边的工作。",
    groups: [
      {
        title: "桌面贴图",
        rows: [
          {
            key: "pinOpacity",
            name: "贴图透明度",
            description: "新建贴图的默认不透明度；贴图窗口内也可以随时调整。",
            type: "range",
            min: 30,
            max: 100,
            step: 5,
            unit: "%",
          },
          {
            key: "pinBorder",
            name: "显示贴图边框",
            description: "清晰区分贴图与桌面内容。",
            type: "toggle",
          },
        ],
      },
    ],
  },
  {
    id: "recognition",
    name: "识别与翻译",
    icon: "translate",
    description: "读懂截图里的文字、语言和链接。",
    groups: [
      {
        title: "文字识别",
        rows: [
          {
            key: "ocrLanguage",
            name: "识别语言",
            description: "优先识别常用语言，减少切换。",
            type: "select",
            options: [
              { value: "中英文混合", label: "中英文混合" },
              { value: "简体中文", label: "简体中文" },
              { value: "English", label: "English" },
              { value: "日本語", label: "日本語" },
            ],
          },
          {
            key: "ocrCopy",
            name: "识别后自动复制",
            description: "文字提取成功后写入剪贴板。",
            type: "toggle",
          },
          {
            key: "qrConfirm",
            name: "打开二维码链接前确认",
            description: "先显示链接内容，再决定是否打开。",
            type: "toggle",
          },
        ],
      },
      {
        title: "翻译",
        rows: [
          {
            key: "translateTo",
            name: "目标语言",
            description: "默认将识别出的文字翻译为此语言。",
            type: "select",
            options: [
              { value: "简体中文", label: "简体中文" },
              { value: "English", label: "English" },
              { value: "日本語", label: "日本語" },
              { value: "한국어", label: "한국어" },
            ],
          },
          {
            key: "translationProvider",
            name: "翻译服务",
            description: "识别功能将在后续版本接入，此处先保存偏好。",
            type: "select",
            options: [
              { value: "系统翻译", label: "系统翻译" },
              { value: "自定义服务（待接入）", label: "自定义服务（待接入）" },
            ],
          },
          {
            key: "sideBySide",
            name: "同时显示原文",
            description: "对照阅读原文与翻译结果。",
            type: "toggle",
          },
        ],
      },
    ],
  },
  {
    id: "permissions",
    name: "权限",
    icon: "permissions",
    description: "Pinstill 只在本机处理截图。系统权限决定能否读取屏幕内容与自动滚动。",
    groups: [
      {
        title: "系统权限",
        rows: [
          {
            key: "screenPermission",
            name: "屏幕录制",
            description: "截图必需。未授权时 Pinstill 无法读取屏幕内容，主窗口会显示授权提示。",
            type: "permission",
            perm: "screen",
          },
          {
            key: "accessibilityPermission",
            name: "辅助功能",
            description: "滚动截图的自动滚动需要它；未授权时仍可手动滚动并拼接。",
            type: "permission",
            perm: "accessibility",
          },
          {
            key: "launchItem",
            name: "登录时启动",
            description: "读取 macOS「登录项」里的真实状态；与「通用与截图」中的开关联动。",
            type: "permission",
            perm: "launch",
          },
        ],
      },
    ],
  },
];

export const DEFAULT_PREFERENCES: Preferences = {
  launch: false,
  cursor: false,
  sound: true,
  afterCapture: "inplace",
  tooltip: true,
  tooltipDelay: 120,
  captureKey: "CmdOrCtrl+Shift+1",
  pinKey: "CmdOrCtrl+Shift+2",
  ocrKey: "CmdOrCtrl+Shift+O",
  enterCopy: true,
  folder: "~/Pictures/Pinstill",
  format: "PNG",
  quality: 90,
  filenamePrefix: "Pinstill",
  keepHistory: true,
  retentionDays: 30,
  historyLimitMB: 2048,
  defaultTool: "arrow",
  defaultColor: "#e56e48",
  defaultWidth: 4,
  watermarkText: "Pinstill",
  watermarkOpacity: 35,
  pinOpacity: 100,
  pinBorder: true,
  ocrLanguage: "中英文混合",
  ocrCopy: true,
  qrConfirm: true,
  translateTo: "简体中文",
  translationProvider: "系统翻译",
  sideBySide: true,
};

export const SHORTCUT_KEYS = ["captureKey", "pinKey", "ocrKey"] as const;

/** Labels shown when a global shortcut fails to register. */
export const SETTINGS_KEY_LABELS: Record<string, string> = {
  captureKey: "区域截图快捷键",
  pinKey: "固定剪贴板图片快捷键",
  ocrKey: "提取屏幕文字快捷键",
  launchAtLogin: "登录后启动",
};

function isMacPlatform(): boolean {
  const navigatorWithHints = navigator as Navigator & {
    userAgentData?: { platform?: string };
  };
  const hint =
    navigatorWithHints.userAgentData?.platform ??
    navigatorWithHints.platform ??
    navigatorWithHints.userAgent;
  return /mac|darwin|iphone|ipad/i.test(hint);
}

/** Turns the design's glyph strings (e.g. "⌘ ⇧ 1", saved by earlier builds) into accelerators. */
function normalizeLegacyAccelerator(value: string): string {
  if (!/[⌘⇧⌃⌥]/.test(value)) return value;
  const modifiers: string[] = [];
  let key = "";
  for (const part of value.split(/\s+/).filter(Boolean)) {
    if (part === "⌘") modifiers.push("CmdOrCtrl");
    else if (part === "⌃") modifiers.push("Ctrl");
    else if (part === "⌥") modifiers.push("Alt");
    else if (part === "⇧") modifiers.push("Shift");
    else key = part;
  }
  return [...modifiers, key].filter(Boolean).join("+");
}

/** Loads persisted preferences, merging only values whose type still matches. */
export function loadPreferences(): Preferences {
  const preferences: Preferences = { ...DEFAULT_PREFERENCES };
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return preferences;
    const saved = JSON.parse(raw) as Record<string, unknown>;
    const target = preferences as unknown as Record<string, unknown>;
    for (const key of Object.keys(DEFAULT_PREFERENCES)) {
      const candidate = saved[key];
      if (typeof candidate === typeof target[key]) target[key] = candidate;
    }
    // Renamed in the real integration: earlier builds stored "filename".
    if (typeof saved.filename === "string" && typeof saved.filenamePrefix !== "string") {
      preferences.filenamePrefix = saved.filename;
    }
  } catch {
    // Corrupt storage falls back to the defaults.
  }
  preferences.captureKey = normalizeLegacyAccelerator(preferences.captureKey);
  preferences.pinKey = normalizeLegacyAccelerator(preferences.pinKey);
  preferences.ocrKey = normalizeLegacyAccelerator(preferences.ocrKey);
  // Round 2 shipped "toolbar" (action bar); Round 3 shows the full toolbar.
  const afterCapture = preferences.afterCapture as string;
  if (afterCapture === "toolbar") preferences.afterCapture = "inplace";
  else if (!["inplace", "clipboard", "pin"].includes(afterCapture)) {
    // R8 removed the main-window editor; older "editor" values fall back to
    // the in-place toolbar.
    preferences.afterCapture = "inplace";
  }
  return preferences;
}

/** Persists preferences in the current browser/webview. */
export function savePreferences(preferences: Preferences): boolean {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(preferences));
    return true;
  } catch {
    return false;
  }
}

/** Reads a row value out of the preferences bag. */
export function preferenceValue(preferences: Preferences, key: string): string | number | boolean {
  return (preferences as unknown as Record<string, string | number | boolean>)[key];
}

/** Writes a row value into the preferences bag with its declared type. */
export function setPreferenceValue(
  preferences: Preferences,
  row: SettingRow,
  value: string | number | boolean,
): void {
  let coerced: string | number | boolean = value;
  if (row.type === "toggle") coerced = Boolean(value);
  else if (row.type === "range") coerced = Number(value);
  else if (row.type === "select" && row.coerce === "number") coerced = Number(value);
  (preferences as unknown as Record<string, string | number | boolean>)[row.key] = coerced;
}

const NUMBER_FALLBACK: Record<string, number> = {
  quality: DEFAULT_PREFERENCES.quality,
  pinOpacity: DEFAULT_PREFERENCES.pinOpacity,
  retentionDays: DEFAULT_PREFERENCES.retentionDays,
  historyLimitMB: DEFAULT_PREFERENCES.historyLimitMB,
};

function numberIn(value: unknown, fallback: number, min: number, max: number): number {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed)) return fallback;
  return Math.min(max, Math.max(min, Math.round(parsed)));
}

/** Maps the UI preferences to the frozen `Settings` object of plan §3.4. */
export function toNativeSettings(preferences: Preferences): Settings {
  const afterCapture: Settings["afterCapture"] =
    preferences.afterCapture === "clipboard" || preferences.afterCapture === "pin"
      ? preferences.afterCapture
      : "inplace";
  const format: Settings["format"] = preferences.format === "JPEG" ? "JPEG" : "PNG";
  return {
    captureKey: preferences.captureKey,
    pinKey: preferences.pinKey,
    ocrKey: preferences.ocrKey,
    afterCapture,
    format,
    quality: numberIn(preferences.quality, NUMBER_FALLBACK.quality, 50, 100),
    filenamePrefix: preferences.filenamePrefix || DEFAULT_PREFERENCES.filenamePrefix,
    folder: preferences.folder || DEFAULT_PREFERENCES.folder,
    includeCursor: Boolean(preferences.cursor),
    sound: Boolean(preferences.sound),
    launchAtLogin: Boolean(preferences.launch),
    keepHistory: Boolean(preferences.keepHistory),
    retentionDays: Math.max(0, Number(preferences.retentionDays) || 0),
    historyLimitMB: Math.max(0, Number(preferences.historyLimitMB) || 0),
    tooltip: Boolean(preferences.tooltip),
    pinOpacity: numberIn(preferences.pinOpacity, DEFAULT_PREFERENCES.pinOpacity, 30, 100),
    pinBorder: Boolean(preferences.pinBorder),
  };
}

const MODIFIER_DISPLAY_MAC: Record<string, string> = {
  cmdorctrl: "⌘",
  commandorcontrol: "⌘",
  command: "⌘",
  cmd: "⌘",
  super: "⌘",
  meta: "⌘",
  win: "⌘",
  ctrl: "⌃",
  control: "⌃",
  alt: "⌥",
  option: "⌥",
  shift: "⇧",
};

const MODIFIER_DISPLAY_OTHER: Record<string, string> = {
  cmdorctrl: "Ctrl",
  commandorcontrol: "Ctrl",
  command: "Win",
  cmd: "Win",
  super: "Win",
  meta: "Win",
  win: "Win",
  ctrl: "Ctrl",
  control: "Ctrl",
  alt: "Alt",
  option: "Alt",
  shift: "Shift",
};

const KEY_DISPLAY: Record<string, string> = {
  space: "Space",
  escape: "Esc",
  up: "↑",
  down: "↓",
  left: "←",
  right: "→",
  pageup: "PageUp",
  pagedown: "PageDown",
};

/** Formats an accelerator for display, e.g. "CmdOrCtrl+Shift+1" → "⌘ ⇧ 1". */
export function formatAccelerator(accelerator: string): string {
  const mac = isMacPlatform();
  const modifierDisplay = mac ? MODIFIER_DISPLAY_MAC : MODIFIER_DISPLAY_OTHER;
  const parts = accelerator
    .split("+")
    .map((part) => part.trim())
    .filter(Boolean);
  const displayed: string[] = [];
  for (const part of parts) {
    const modifier = modifierDisplay[part.toLowerCase()];
    if (modifier) {
      displayed.push(modifier);
      continue;
    }
    if (part.length === 1) displayed.push(part.toUpperCase());
    else displayed.push(KEY_DISPLAY[part.toLowerCase()] ?? part);
  }
  return displayed.join(" ");
}

const MODIFIER_KEYS = new Set([
  "meta",
  "control",
  "alt",
  "shift",
  "capslock",
  "fn",
  "altgraph",
]);

const SPECIAL_KEY_NAMES: Record<string, string> = {
  " ": "Space",
  spacebar: "Space",
  enter: "Enter",
  return: "Enter",
  tab: "Tab",
  backspace: "Backspace",
  delete: "Delete",
  insert: "Insert",
  escape: "Escape",
  esc: "Escape",
  arrowup: "Up",
  arrowdown: "Down",
  arrowleft: "Left",
  arrowright: "Right",
  home: "Home",
  end: "End",
  pageup: "PageUp",
  pagedown: "PageDown",
  "+": "Plus",
};

function acceleratorKeyName(key: string): string | null {
  const lower = key.toLowerCase();
  if (SPECIAL_KEY_NAMES[lower]) return SPECIAL_KEY_NAMES[lower];
  if (/^f([1-9]|1[0-9]|2[0-4])$/.test(lower)) return key.toUpperCase();
  if (key.length === 1) {
    if (/[a-zA-Z0-9]/.test(key)) return key.toUpperCase();
    return key;
  }
  return null;
}

const CODE_KEY_NAMES: Record<string, string> = {
  Minus: "-",
  Equal: "=",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  Backquote: "`",
  NumpadAdd: "NumAdd",
  NumpadSubtract: "NumSub",
  NumpadMultiply: "NumMult",
  NumpadDivide: "NumDiv",
  NumpadDecimal: "NumDec",
};

// The physical key (`code`) keeps Shift out of the key name: Cmd+Shift+1 on a
// US layout reports e.key === "!", but the accelerator is "…+Shift+1".
function acceleratorKeyFromEvent(event: KeyboardEvent): string | null {
  const code = event.code ?? "";
  const letter = /^Key([A-Z])$/.exec(code);
  if (letter) return letter[1];
  const digit = /^Digit([0-9])$/.exec(code);
  if (digit) return digit[1];
  const numpad = /^Numpad([0-9])$/.exec(code);
  if (numpad) return `Num${numpad[1]}`;
  if (CODE_KEY_NAMES[code]) return CODE_KEY_NAMES[code];
  if (/^F([1-9]|1[0-9]|2[0-4])$/.test(code)) return code;
  return acceleratorKeyName(event.key);
}

export type AcceleratorRecordResult =
  | { accelerator: string }
  | { error: string }
  | null;

/**
 * Builds a MyGo accelerator from a keydown event. Returns `null` for modifier
 * keys (still recording), or an error when no primary modifier is held.
 */
export function recordAccelerator(event: KeyboardEvent): AcceleratorRecordResult {
  if (MODIFIER_KEYS.has(event.key.toLowerCase())) return null;
  const mac = isMacPlatform();
  const modifiers: string[] = [];
  if (event.metaKey) modifiers.push(mac ? "CmdOrCtrl" : "Cmd");
  if (event.ctrlKey) modifiers.push(mac ? "Ctrl" : "CmdOrCtrl");
  if (event.altKey) modifiers.push("Alt");
  if (event.shiftKey) modifiers.push("Shift");
  if (!event.metaKey && !event.ctrlKey && !event.altKey) {
    return { error: "请包含 ⌘、Ctrl 或 Alt 组合键" };
  }
  const name = acceleratorKeyFromEvent(event);
  if (!name) return null;
  return { accelerator: [...modifiers, name].join("+") };
}

/** Compares accelerators case-insensitively for duplicate detection. */
export function sameAccelerator(a: string, b: string): boolean {
  const normalize = (value: string) =>
    value
      .split("+")
      .map((part) => part.trim().toLowerCase())
      .filter(Boolean)
      .join("+");
  return normalize(a) === normalize(b);
}
