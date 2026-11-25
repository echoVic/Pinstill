// The in-app colour palette (R7.8): a self-drawn panel shared by the overlay
// annotation toolbar and the main editor's property bar.
//
// It replaces the WKWebView-native `<input type="color">` flow with a
// deterministic UI: a preset grid (>= 32 swatches), an HSV square plus hue
// slider, a preview + hex field, and the recent-colour row persisted in
// colors.ts (`pinstill.recentColors`, now up to 8 entries).
//
// The panel is a per-page singleton. The frozen DOM hooks from work/plan.md
// R7.8 are: `[data-palette-toggle]`, `#colorPalettePanel`,
// `button[data-palette-color]`, `#paletteSV`, `#paletteHue`, `#paletteHex`,
// `#palettePreview`, `[data-palette-recent]` and `[data-palette-close]`.

import {
  DEFAULT_ANNOTATION_COLOR,
  loadRecentColors,
  MAX_RECENT_COLORS,
  normalizeColor,
} from "./colors";

/** How a colour choice should be recorded in the recent-colour list. */
export interface PaletteApplyOptions {
  remember: boolean;
}

export interface ColorPalettePanelOptions {
  /** Current applied colour, used to seed the panel when it opens. */
  getColor: () => string;
  /** Applies the colour to the page's active tool. */
  onColor: (color: string, options: PaletteApplyOptions) => void;
}

/**
 * 40 preset swatches, 8 columns x 5 rows. The first eight mirror the quick
 * swatch row (PRESET_COLORS in colors.ts) so selecting the first palette
 * swatch also lights up the quick row; the rest are hue/neutral families that
 * match the dark-warm design language.
 */
export const PALETTE_PRESETS: ReadonlyArray<string> = [
  DEFAULT_ANNOTATION_COLOR,
  "#e56e48",
  "#e46c4e",
  "#c94b35",
  "#f08a5d",
  "#f3a24e",
  "#efb94c",
  "#f6d186",
  "#f6eee1",
  "#e8d9a8",
  "#c9cf8a",
  "#a9c47f",
  "#78a576",
  "#5d9c7c",
  "#4f8f83",
  "#70a8c2",
  "#8a7d64",
  "#6f8fa8",
  "#5c7fa8",
  "#6d6fa8",
  "#8a6fa8",
  "#a86f9a",
  "#c27a86",
  "#d98a7a",
  "#1a1915",
  "#2f2d26",
  "#4b473c",
  "#6f6a5e",
  "#8e8a7e",
  "#b8b1a5",
  "#d9d2c4",
  "#ffffff",
  "#e05252",
  "#f28c28",
  "#f2c14e",
  "#9bc53d",
  "#3ca374",
  "#2a9d8f",
  "#3a86ff",
  "#9b5de5",
];

const SVG_ICON = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 22a1 1 0 0 1 0-20 10 9 0 0 1 10 9 5 5 0 0 1-5 5h-2.25a1.75 1.75 0 0 0-1.4 2.8l.3.4a1.75 1.75 0 0 1-1.4 2.8z"/><circle cx="13.5" cy="6.5" r=".5" fill="currentColor"/><circle cx="17.5" cy="10.5" r=".5" fill="currentColor"/><circle cx="8.5" cy="7.5" r=".5" fill="currentColor"/><circle cx="6.5" cy="12.5" r=".5" fill="currentColor"/></svg>`;

const PANEL_STYLE_ID = "pinstillColorPaletteStyles";

const PANEL_CSS = `
#colorPalettePanel{position:absolute;z-index:60;width:252px;box-sizing:border-box;padding:10px;border:1px solid rgba(242,238,228,.16);border-radius:12px;background:#201f1b;color:#f2eee4;box-shadow:0 22px 54px rgba(0,0,0,.5);font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;font-size:12px;line-height:1.4;user-select:none;-webkit-user-select:none}
#colorPalettePanel[hidden]{display:none!important}
#colorPalettePanel *{box-sizing:border-box}
.cpp-head{display:flex;align-items:center;justify-content:space-between;gap:8px;margin:0 2px 8px}
.cpp-title{font-size:11px;font-weight:600;letter-spacing:.08em;color:#e5c99a;text-transform:uppercase}
.cpp-close{width:20px;height:20px;padding:0;border:0;border-radius:6px;background:transparent;color:#a9a295;font-size:14px;line-height:1;cursor:pointer}
.cpp-close:hover{background:#343027;color:#f2eee4}
.cpp-close:focus-visible{outline:2px solid #f3a24e;outline-offset:1px}
.cpp-presets{display:grid;grid-template-columns:repeat(8,1fr);gap:4px;margin-bottom:9px}
.cpp-swatch{position:relative;height:21px;padding:0;border:1px solid rgba(255,255,255,.12);border-radius:5px;cursor:pointer;box-shadow:inset 0 0 0 1px rgba(0,0,0,.18);transition:border-color 120ms ease,transform 120ms ease}
.cpp-swatch:hover{border-color:rgba(255,255,255,.4);transform:translateY(-1px)}
.cpp-swatch:focus-visible{outline:2px solid #f3a24e;outline-offset:2px;z-index:1}
.cpp-swatch.selected{outline:2px solid #f3a24e;outline-offset:1px}
.cpp-sv-wrap{position:relative;margin-bottom:9px}
#paletteSV{display:block;width:100%;height:auto;border-radius:8px;cursor:crosshair;touch-action:none;border:1px solid rgba(255,255,255,.14)}
.cpp-row{display:flex;align-items:center;gap:8px;margin-bottom:9px}
.cpp-preview{flex:0 0 30px;width:30px;height:26px;border-radius:6px;border:1px solid rgba(255,255,255,.22);box-shadow:inset 0 0 0 1px rgba(0,0,0,.2)}
#paletteHue{flex:1;height:12px;margin:0;appearance:none;-webkit-appearance:none;border-radius:999px;background:linear-gradient(to right,#f00,#ff0,#0f0,#0ff,#00f,#f0f,#f00);border:1px solid rgba(255,255,255,.16);cursor:pointer}
#paletteHue::-webkit-slider-thumb{-webkit-appearance:none;width:14px;height:14px;border-radius:50%;background:#faf7f0;border:2px solid #201f1b;box-shadow:0 1px 4px rgba(0,0,0,.5);cursor:pointer}
#paletteHue::-moz-range-thumb{width:14px;height:14px;border-radius:50%;background:#faf7f0;border:2px solid #201f1b;cursor:pointer}
#paletteHue:focus-visible{outline:2px solid #f3a24e;outline-offset:3px}
.cpp-hex-row{display:flex;align-items:center;gap:8px;margin-bottom:9px}
.cpp-hex-label{font-size:10px;letter-spacing:.08em;color:#9d968a}
#paletteHex{flex:1;min-width:0;height:26px;padding:0 8px;border:1px solid rgba(255,255,255,.18);border-radius:6px;background:#302d25;color:#f6efe2;font:500 12px/1.2 ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.03em;user-select:text;-webkit-user-select:text}
#paletteHex::placeholder{color:#7c7568}
#paletteHex:focus-visible{outline:2px solid #f3a24e;outline-offset:1px;border-color:#f3a24e}
#paletteHex.invalid{border-color:#e46c4e;box-shadow:0 0 0 2px rgba(228,108,78,.22)}
.cpp-recent-block{border-top:1px solid rgba(242,238,228,.1);padding-top:8px}
.cpp-subtitle{font-size:10px;letter-spacing:.08em;color:#9d968a;margin:0 2px 6px;text-transform:uppercase}
.cpp-recents{display:flex;align-items:center;gap:5px;min-height:20px}
.cpp-recent{width:20px;height:20px;padding:0;border:1px solid rgba(255,255,255,.14);border-radius:50%;cursor:pointer;box-shadow:inset 0 0 0 1px rgba(0,0,0,.18)}
.cpp-recent:hover{border-color:rgba(255,255,255,.4)}
.cpp-recent.selected{outline:2px solid #f3a24e;outline-offset:1px}
.cpp-recent:focus-visible{outline:2px solid #f3a24e;outline-offset:2px}
.cpp-recents-empty{font-size:10px;color:#7c7568}
@keyframes cpp-in{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:none}}
#colorPalettePanel:not([hidden]){animation:cpp-in 140ms cubic-bezier(.22,.61,.36,1) both}
@media (prefers-reduced-motion: reduce){#colorPalettePanel:not([hidden]){animation-duration:.001ms!important}.cpp-swatch{transition-duration:.001ms!important}}
[data-palette-toggle]{cursor:pointer}
[data-palette-toggle]::after{content:none!important;display:none!important}
[data-palette-toggle] svg{width:11px;height:11px;display:block;pointer-events:none}
[data-palette-toggle].palette-open{outline:2px solid #f3a24e;outline-offset:2px}
.palette-toggle{display:inline-flex;align-items:center;justify-content:center;width:24px;height:24px;padding:0;border:1px solid rgba(255,255,255,.2);border-radius:7px;background:#302d25;color:#e9dfd0}
.palette-toggle:hover{background:#3b372c}
.palette-toggle:focus-visible{outline:2px solid #f3a24e;outline-offset:2px}
`;

/** Creates the frozen-hook trigger button, with a palette glyph. */
export function createPaletteToggleButton(className = ""): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = className;
  button.setAttribute("data-palette-toggle", "true");
  button.setAttribute("aria-label", "调色板");
  button.setAttribute("aria-expanded", "false");
  button.title = "调色板";
  button.innerHTML = SVG_ICON;
  return button;
}

// ---------------------------------------------------------------------------
// Colour conversions
// ---------------------------------------------------------------------------

interface Rgb {
  r: number;
  g: number;
  b: number;
}

interface Hsv {
  h: number;
  s: number;
  v: number;
}

/** Accepts `#rrggbb` or `rrggbb` and returns lowercase `#rrggbb`, else null. */
export function normalizePaletteHex(value: string): string | null {
  const match = /^#?([0-9a-fA-F]{6})$/.exec(value.trim());
  return match ? `#${match[1]!.toLowerCase()}` : null;
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}

function hexToRgb(hex: string): Rgb {
  const normalized = normalizePaletteHex(hex) ?? "#000000";
  return {
    r: parseInt(normalized.slice(1, 3), 16),
    g: parseInt(normalized.slice(3, 5), 16),
    b: parseInt(normalized.slice(5, 7), 16),
  };
}

function rgbToHex(rgb: Rgb): string {
  const part = (value: number) =>
    Math.min(255, Math.max(0, Math.round(value)))
      .toString(16)
      .padStart(2, "0");
  return `#${part(rgb.r)}${part(rgb.g)}${part(rgb.b)}`;
}

function rgbToHsv({ r, g, b }: Rgb): Hsv {
  const rn = r / 255;
  const gn = g / 255;
  const bn = b / 255;
  const max = Math.max(rn, gn, bn);
  const min = Math.min(rn, gn, bn);
  const delta = max - min;
  let h = 0;
  if (delta > 0) {
    if (max === rn) h = ((gn - bn) / delta) % 6;
    else if (max === gn) h = (bn - rn) / delta + 2;
    else h = (rn - gn) / delta + 4;
    h *= 60;
    if (h < 0) h += 360;
  }
  return { h, s: max === 0 ? 0 : delta / max, v: max };
}

function hsvToRgb(h: number, s: number, v: number): Rgb {
  const hue = ((h % 360) + 360) % 360;
  const c = v * s;
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
  const m = v - c;
  let rp = 0;
  let gp = 0;
  let bp = 0;
  if (hue < 60) [rp, gp, bp] = [c, x, 0];
  else if (hue < 120) [rp, gp, bp] = [x, c, 0];
  else if (hue < 180) [rp, gp, bp] = [0, c, x];
  else if (hue < 240) [rp, gp, bp] = [0, x, c];
  else if (hue < 300) [rp, gp, bp] = [x, 0, c];
  else [rp, gp, bp] = [c, 0, x];
  return { r: (rp + m) * 255, g: (gp + m) * 255, b: (bp + m) * 255 };
}

function hexToHsv(hex: string): Hsv {
  return rgbToHsv(hexToRgb(hex));
}

function hsvToHex(h: number, s: number, v: number): string {
  return rgbToHex(hsvToRgb(h, s, v));
}

// ---------------------------------------------------------------------------
// Panel
// ---------------------------------------------------------------------------

export class ColorPalettePanel {
  private readonly options: ColorPalettePanelOptions;

  private panel: HTMLDivElement | null = null;
  private presetsHost!: HTMLDivElement;
  private recentsHost!: HTMLDivElement;
  private sv!: HTMLCanvasElement;
  private svCtx: CanvasRenderingContext2D | null = null;
  private hue!: HTMLInputElement;
  private hex!: HTMLInputElement;
  private preview!: HTMLSpanElement;

  private h = 12;
  private s = 0.75;
  private v = 0.9;
  private anchor: HTMLElement | null = null;
  private draggingSV = false;
  private appliedColor = DEFAULT_ANNOTATION_COLOR;

  constructor(options: ColorPalettePanelOptions) {
    this.options = options;
  }

  isOpen(): boolean {
    return this.panel !== null && !this.panel.hidden;
  }

  contains(target: EventTarget | null): boolean {
    return (
      this.panel !== null && target instanceof Node && this.panel.contains(target)
    );
  }

  toggle(anchor: HTMLElement): void {
    if (this.isOpen()) this.close();
    else this.open(anchor);
  }

  open(anchor: HTMLElement): void {
    this.ensureBuilt();
    const panel = this.panel;
    if (!panel) return;
    this.anchor = anchor;
    const seeded = normalizeColor(this.options.getColor()) ?? this.appliedColor;
    this.setFromColor(seeded);
    this.refreshRecents();
    panel.hidden = false;
    anchor.setAttribute("aria-expanded", "true");
    anchor.classList.add("palette-open");
    this.position(anchor);
    this.syncSelection(this.appliedColor);
  }

  close(): void {
    if (!this.panel || this.panel.hidden) return;
    this.panel.hidden = true;
    if (this.anchor) {
      this.anchor.setAttribute("aria-expanded", "false");
      this.anchor.classList.remove("palette-open");
    }
    this.anchor = null;
    this.draggingSV = false;
  }

  /** Re-reads the current colour, e.g. after an external quick swatch click. */
  sync(color?: string): void {
    const next = normalizeColor(color ?? this.options.getColor());
    if (!next) return;
    this.appliedColor = next;
    if (!this.isOpen()) return;
    this.setFromColor(next);
    this.refreshRecents();
  }

  // ------------------------------------------------------------------ setup

  private ensureBuilt(): void {
    if (this.panel) return;
    this.ensureStyles();

    const panel = document.createElement("div");
    panel.id = "colorPalettePanel";
    panel.className = "color-palette-panel";
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-label", "调色板");
    panel.setAttribute("aria-modal", "false");
    panel.hidden = true;

    const head = document.createElement("div");
    head.className = "cpp-head";
    const title = document.createElement("span");
    title.className = "cpp-title";
    title.textContent = "调色板";
    const close = document.createElement("button");
    close.type = "button";
    close.className = "cpp-close";
    close.setAttribute("data-palette-close", "true");
    close.setAttribute("aria-label", "关闭调色板");
    close.textContent = "×";
    close.addEventListener("click", () => this.close());
    head.append(title, close);

    const presets = document.createElement("div");
    presets.className = "cpp-presets";
    presets.setAttribute("role", "group");
    presets.setAttribute("aria-label", "预设颜色");

    const svWrap = document.createElement("div");
    svWrap.className = "cpp-sv-wrap";
    const sv = document.createElement("canvas");
    sv.id = "paletteSV";
    sv.width = 224;
    sv.height = 126;
    sv.setAttribute("role", "slider");
    sv.setAttribute("aria-label", "饱和度与明度");
    sv.setAttribute("tabindex", "0");
    svWrap.append(sv);

    const hueRow = document.createElement("div");
    hueRow.className = "cpp-row";
    const hue = document.createElement("input");
    hue.id = "paletteHue";
    hue.type = "range";
    hue.min = "0";
    hue.max = "360";
    hue.step = "1";
    hue.setAttribute("aria-label", "色相");
    hueRow.append(hue);

    const hexRow = document.createElement("div");
    hexRow.className = "cpp-row cpp-hex-row";
    const preview = document.createElement("span");
    preview.id = "palettePreview";
    preview.className = "cpp-preview";
    preview.setAttribute("aria-label", "当前颜色预览");
    const hexLabel = document.createElement("span");
    hexLabel.className = "cpp-hex-label";
    hexLabel.textContent = "HEX";
    const hex = document.createElement("input");
    hex.id = "paletteHex";
    hex.type = "text";
    hex.maxLength = 7;
    hex.spellcheck = false;
    hex.autocomplete = "off";
    hex.placeholder = "#rrggbb";
    hex.setAttribute("aria-label", "十六进制颜色");
    hexRow.append(preview, hexLabel, hex);

    const recentBlock = document.createElement("div");
    recentBlock.className = "cpp-recent-block";
    const recentTitle = document.createElement("div");
    recentTitle.className = "cpp-subtitle";
    recentTitle.textContent = "最近使用";
    const recents = document.createElement("div");
    recents.className = "cpp-recents";
    recents.setAttribute("role", "group");
    recents.setAttribute("aria-label", "最近使用颜色");
    recentBlock.append(recentTitle, recents);

    panel.append(head, presets, svWrap, hueRow, hexRow, recentBlock);
    document.body.append(panel);

    this.panel = panel;
    this.presetsHost = presets;
    this.recentsHost = recents;
    this.sv = sv;
    this.svCtx = sv.getContext("2d");
    this.hue = hue;
    this.hex = hex;
    this.preview = preview;

    this.buildPresets();
    this.bindEvents();
  }

  private ensureStyles(): void {
    if (document.getElementById(PANEL_STYLE_ID)) return;
    const style = document.createElement("style");
    style.id = PANEL_STYLE_ID;
    style.textContent = PANEL_CSS;
    document.head.append(style);
  }

  private buildPresets(): void {
    this.presetsHost.replaceChildren();
    for (const color of PALETTE_PRESETS) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "cpp-swatch";
      button.dataset.paletteColor = color;
      button.style.backgroundColor = color;
      button.setAttribute("aria-label", `颜色 ${color}`);
      button.setAttribute("aria-pressed", "false");
      button.title = color;
      button.addEventListener("click", () => this.choose(color));
      this.presetsHost.append(button);
    }
  }

  private bindEvents(): void {
    // Close when clicking anywhere outside the panel (trigger clicks are
    // handled by the trigger's own toggle).
    window.addEventListener(
      "pointerdown",
      (event) => {
        if (!this.isOpen()) return;
        const target = event.target;
        if (this.contains(target)) return;
        if (target instanceof Element && target.closest("[data-palette-toggle]")) return;
        this.close();
      },
      true,
    );
    // Esc priority: while the panel is open, Esc closes only the panel and
    // never reaches the page's cancel/exit handlers (capture phase + stop).
    window.addEventListener(
      "keydown",
      (event) => {
        if (!this.isOpen() || event.key !== "Escape") return;
        event.preventDefault();
        event.stopImmediatePropagation();
        this.close();
      },
      true,
    );
    window.addEventListener("resize", () => {
      if (this.isOpen() && this.anchor?.isConnected) this.position(this.anchor);
    });
    // The panel lives in document coordinates (absolute), so it scrolls
    // together with its trigger; only re-clamp when the page scrolls while the
    // anchor is still on screen.
    window.addEventListener(
      "scroll",
      () => {
        if (this.isOpen() && this.anchor?.isConnected) this.position(this.anchor);
      },
      true,
    );

    this.hue.addEventListener("input", () => {
      this.h = Math.min(360, Math.max(0, Number(this.hue.value) || 0));
      this.drawSV();
      this.applyHsv(false);
    });
    this.hue.addEventListener("change", () => {
      this.h = Math.min(360, Math.max(0, Number(this.hue.value) || 0));
      this.drawSV();
      this.applyHsv(true);
    });

    this.bindSvDrag();

    this.hex.addEventListener("keydown", (event) => {
      if (event.key !== "Enter") return;
      event.preventDefault();
      event.stopPropagation();
      const normalized = normalizePaletteHex(this.hex.value);
      if (!normalized) {
        this.hex.classList.add("invalid");
        this.hex.setAttribute("aria-invalid", "true");
        return;
      }
      this.hex.classList.remove("invalid");
      this.hex.removeAttribute("aria-invalid");
      this.choose(normalized);
      this.hex.select();
    });
    this.hex.addEventListener("input", () => {
      if (!this.hex.value.trim()) return;
      this.hex.classList.remove("invalid");
      this.hex.removeAttribute("aria-invalid");
    });
    this.hex.addEventListener("blur", () => {
      // Leave the field showing the actually-applied colour after bad input.
      this.hex.classList.remove("invalid");
      this.hex.removeAttribute("aria-invalid");
      this.hex.value = this.appliedColor;
    });
  }

  private bindSvDrag(): void {
    const canvas = this.sv;
    const update = (event: PointerEvent): void => {
      const rect = canvas.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0) return;
      this.s = clamp01((event.clientX - rect.left) / rect.width);
      this.v = 1 - clamp01((event.clientY - rect.top) / rect.height);
      this.drawSV();
      this.applyHsv(false);
    };
    canvas.addEventListener("pointerdown", (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      this.draggingSV = true;
      try {
        canvas.setPointerCapture(event.pointerId);
      } catch {
        // Synthetic events (automation) still reach the move/up listeners.
      }
      update(event);
    });
    canvas.addEventListener("pointermove", (event) => {
      if (!this.draggingSV) return;
      event.preventDefault();
      update(event);
    });
    const finish = (event: PointerEvent): void => {
      if (!this.draggingSV) return;
      this.draggingSV = false;
      update(event);
      this.applyHsv(true);
    };
    canvas.addEventListener("pointerup", finish);
    canvas.addEventListener("pointercancel", finish);
  }

  // ---------------------------------------------------------------- applying

  private choose(color: string): void {
    const normalized = normalizeColor(color);
    if (!normalized) return;
    this.setFromColor(normalized);
    this.options.onColor(normalized, { remember: true });
    this.refreshRecents();
  }

  private setFromColor(color: string): void {
    const normalized = normalizeColor(color);
    if (!normalized) return;
    const { h, s, v } = hexToHsv(normalized);
    this.h = h;
    this.s = s;
    this.v = v;
    this.appliedColor = normalized;
    this.hue.value = String(Math.round(h));
    this.drawSV();
    this.preview.style.backgroundColor = normalized;
    this.hex.value = normalized;
    this.hex.classList.remove("invalid");
    this.hex.removeAttribute("aria-invalid");
    this.syncSelection(normalized);
  }

  private applyHsv(remember: boolean): void {
    const color = hsvToHex(this.h, this.s, this.v);
    this.appliedColor = color;
    this.preview.style.backgroundColor = color;
    this.hex.value = color;
    this.syncSelection(color);
    this.options.onColor(color, { remember });
    if (remember) this.refreshRecents();
  }

  private syncSelection(color: string): void {
    const normalized = color.toLowerCase();
    for (const button of this.presetsHost.querySelectorAll<HTMLButtonElement>(
      "button[data-palette-color]",
    )) {
      const selected = button.dataset.paletteColor === normalized;
      button.classList.toggle("selected", selected);
      button.setAttribute("aria-pressed", String(selected));
    }
    for (const button of this.recentsHost.querySelectorAll<HTMLButtonElement>(
      "button[data-palette-recent]",
    )) {
      const selected = button.dataset.paletteRecent === normalized;
      button.classList.toggle("selected", selected);
      button.setAttribute("aria-pressed", String(selected));
    }
  }

  private refreshRecents(): void {
    const colors = loadRecentColors().slice(0, MAX_RECENT_COLORS);
    this.recentsHost.replaceChildren();
    if (colors.length === 0) {
      const empty = document.createElement("span");
      empty.className = "cpp-recents-empty";
      empty.textContent = "还没有最近颜色";
      this.recentsHost.append(empty);
    }
    for (const color of colors) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "cpp-recent";
      button.dataset.paletteRecent = color;
      button.style.backgroundColor = color;
      button.setAttribute("aria-label", `最近使用颜色 ${color}`);
      button.setAttribute("aria-pressed", "false");
      button.title = color;
      button.addEventListener("click", () => this.choose(color));
      this.recentsHost.append(button);
    }
    this.syncSelection(this.appliedColor);
  }

  // -------------------------------------------------------------- geometry

  private position(anchor: HTMLElement): void {
    const panel = this.panel;
    if (!panel) return;
    const margin = 8;
    const gap = 8;
    const rect = anchor.getBoundingClientRect();
    const width = panel.offsetWidth;
    const height = panel.offsetHeight;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = rect.left;
    let top = rect.bottom + gap;
    if (top + height > vh - margin) top = rect.top - height - gap;
    left = Math.min(Math.max(margin, left), Math.max(margin, vw - width - margin));
    top = Math.min(Math.max(margin, top), Math.max(margin, vh - height - margin));
    panel.style.left = `${Math.round(left + window.scrollX)}px`;
    panel.style.top = `${Math.round(top + window.scrollY)}px`;
  }

  private drawSV(): void {
    const ctx = this.svCtx;
    if (!ctx) return;
    const { width, height } = this.sv;
    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = `hsl(${Math.round(this.h)} 100% 50%)`;
    ctx.fillRect(0, 0, width, height);
    const white = ctx.createLinearGradient(0, 0, width, 0);
    white.addColorStop(0, "rgba(255,255,255,1)");
    white.addColorStop(1, "rgba(255,255,255,0)");
    ctx.fillStyle = white;
    ctx.fillRect(0, 0, width, height);
    const black = ctx.createLinearGradient(0, 0, 0, height);
    black.addColorStop(0, "rgba(0,0,0,0)");
    black.addColorStop(1, "rgba(0,0,0,1)");
    ctx.fillStyle = black;
    ctx.fillRect(0, 0, width, height);

    const x = Math.min(width - 5, Math.max(5, this.s * width));
    const y = Math.min(height - 5, Math.max(5, (1 - this.v) * height));
    ctx.beginPath();
    ctx.arc(x, y, 5.5, 0, Math.PI * 2);
    ctx.lineWidth = 2;
    ctx.strokeStyle = this.v > 0.55 ? "#141412" : "#faf7f0";
    ctx.stroke();
    ctx.beginPath();
    ctx.arc(x, y, 4.5, 0, Math.PI * 2);
    ctx.lineWidth = 1;
    ctx.strokeStyle = this.v > 0.55 ? "#faf7f0" : "#141412";
    ctx.stroke();
  }
}
