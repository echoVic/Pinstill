// Lightweight localStorage-backed translation history and favorites.

import { normalizeTranslationStyle, type TranslationStyle } from "./translate-text";

export interface TranslateHistoryItem {
  id: string;
  source: string;
  result: string;
  sourceLang: string;
  targetLang: string;
  style: TranslationStyle;
  starred: boolean;
  createdAt: number;
}

export const TRANSLATE_HISTORY_KEY = "pinstill.translateHistory";
export const TRANSLATE_HISTORY_ENABLED_KEY = "pinstill.translateHistoryEnabled";
export const MAX_UNSTARRED_HISTORY = 50;
export const MAX_STARRED_HISTORY = 100;

let seq = 0;

function makeHistoryId(now: number): string {
  seq += 1;
  return `tr-${now}-${seq}`;
}

export function isTranslateHistoryEnabled(): boolean {
  try {
    return localStorage.getItem(TRANSLATE_HISTORY_ENABLED_KEY) !== "0";
  } catch {
    return true;
  }
}

export function setTranslateHistoryEnabled(enabled: boolean): void {
  try {
    localStorage.setItem(TRANSLATE_HISTORY_ENABLED_KEY, enabled ? "1" : "0");
  } catch {
    // Storage may be unavailable.
  }
}

function normalizeHistoryItems(raw: unknown): TranslateHistoryItem[] {
  if (!Array.isArray(raw)) return [];
  const items: TranslateHistoryItem[] = [];
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const item = entry as Partial<TranslateHistoryItem>;
    const source = typeof item.source === "string" ? item.source.trim() : "";
    const result = typeof item.result === "string" ? item.result.trim() : "";
    if (!source || !result) continue;
    const createdAt =
      typeof item.createdAt === "number" && Number.isFinite(item.createdAt)
        ? item.createdAt
        : Date.now();
    items.push({
      id: typeof item.id === "string" && item.id ? item.id : makeHistoryId(createdAt),
      source,
      result,
      sourceLang: typeof item.sourceLang === "string" && item.sourceLang ? item.sourceLang : "auto",
      targetLang: typeof item.targetLang === "string" && item.targetLang ? item.targetLang : "zh-Hans",
      style: normalizeTranslationStyle(item.style),
      starred: Boolean(item.starred),
      createdAt,
    });
  }
  return trimHistoryItems(items);
}

function trimHistoryItems(items: TranslateHistoryItem[]): TranslateHistoryItem[] {
  const out: TranslateHistoryItem[] = [];
  let starredCount = 0;
  let unstarredCount = 0;
  for (const item of items) {
    if (item.starred) {
      if (starredCount >= MAX_STARRED_HISTORY) continue;
      starredCount += 1;
      out.push(item);
    } else {
      if (unstarredCount >= MAX_UNSTARRED_HISTORY) continue;
      unstarredCount += 1;
      out.push(item);
    }
  }
  return out;
}

export function loadTranslateHistory(): TranslateHistoryItem[] {
  try {
    const raw = localStorage.getItem(TRANSLATE_HISTORY_KEY);
    if (!raw) return [];
    return normalizeHistoryItems(JSON.parse(raw));
  } catch {
    return [];
  }
}

export function saveTranslateHistory(items: TranslateHistoryItem[]): TranslateHistoryItem[] {
  const normalized = trimHistoryItems(items);
  try {
    localStorage.setItem(TRANSLATE_HISTORY_KEY, JSON.stringify(normalized));
  } catch {
    // Storage may be unavailable.
  }
  return normalized;
}

export function recordTranslateHistory(
  entry: Omit<TranslateHistoryItem, "id" | "starred" | "createdAt">,
  options: { forceStar?: boolean } = {},
): TranslateHistoryItem {
  const source = entry.source.trim();
  const result = entry.result.trim();
  const sourceLang = entry.sourceLang || "auto";
  const now = Date.now();
  const items = loadTranslateHistory();
  const matchIndex = items.findIndex(
    (item) =>
      item.source === source &&
      item.sourceLang === sourceLang &&
      item.targetLang === entry.targetLang &&
      item.style === entry.style,
  );
  const existing = matchIndex >= 0 ? items[matchIndex] : undefined;
  const next: TranslateHistoryItem = {
    id: existing?.id ?? makeHistoryId(now),
    source,
    result,
    sourceLang,
    targetLang: entry.targetLang,
    style: entry.style,
    starred: Boolean(existing?.starred || options.forceStar),
    createdAt: now,
  };
  if (matchIndex >= 0) items.splice(matchIndex, 1);
  items.unshift(next);
  saveTranslateHistory(items);
  return next;
}

export function toggleStarHistoryItem(id: string): TranslateHistoryItem | null {
  const items = loadTranslateHistory();
  const item = items.find((entry) => entry.id === id);
  if (!item) return null;
  item.starred = !item.starred;
  saveTranslateHistory(items);
  return item;
}

export function removeHistoryItem(id: string): TranslateHistoryItem[] {
  const remaining = loadTranslateHistory().filter((entry) => entry.id !== id);
  return saveTranslateHistory(remaining);
}

export function clearUnstarredHistory(): TranslateHistoryItem[] {
  const starredOnly = loadTranslateHistory().filter((entry) => entry.starred);
  return saveTranslateHistory(starredOnly);
}
