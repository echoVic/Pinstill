// Shared colour palette for the annotation tools (overlay + main editor).
//
// Preset swatches, a native colour picker (`<input type="color">`) for custom
// colours, and the most recently used custom colours persisted in
// localStorage so both pages share the same palette.

export const PRESET_COLORS: ReadonlyArray<string> = [
  "#e56e48",
  "#efb94c",
  "#78a576",
  "#70a8c2",
  "#f6eee1",
  "#e46c4e",
  "#8a7d64",
  "#ffffff",
];

export const RECENT_COLORS_KEY = "tupen.recentColors";
export const MAX_RECENT_COLORS = 4;

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;

export function normalizeColor(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  return HEX_COLOR.test(trimmed) ? trimmed.toLowerCase() : null;
}

/** Reads the recent custom colours (newest first). */
export function loadRecentColors(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_COLORS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    const colors: string[] = [];
    for (const entry of parsed) {
      const color = normalizeColor(entry);
      if (color && !colors.includes(color)) colors.push(color);
      if (colors.length >= MAX_RECENT_COLORS) break;
    }
    return colors;
  } catch {
    return [];
  }
}

/** Adds a custom colour to the front of the recent list and persists it. */
export function rememberColor(value: string): string[] {
  const color = normalizeColor(value);
  if (!color) return loadRecentColors();
  const colors = [color, ...loadRecentColors().filter((entry) => entry !== color)].slice(
    0,
    MAX_RECENT_COLORS,
  );
  try {
    localStorage.setItem(RECENT_COLORS_KEY, JSON.stringify(colors));
  } catch {
    // Storage may be unavailable; the in-memory list still updates.
  }
  return colors;
}

export function isPresetColor(value: string): boolean {
  const color = normalizeColor(value);
  return color !== null && PRESET_COLORS.some((preset) => preset.toLowerCase() === color);
}
