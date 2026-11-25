// Pure text helpers, glossary persistence, and translation style metadata.

export type TranslationStyle = "standard" | "polish" | "technical" | "summary";

export interface TranslationStyleOption {
  code: TranslationStyle;
  label: string;
}

export interface GlossaryEntry {
  source: string;
  target: string;
}

export const GLOSSARY_KEY = "pinstill.glossary";
export const MAX_GLOSSARY_ENTRIES = 50;

export const TRANSLATION_STYLES: readonly TranslationStyleOption[] = [
  { code: "standard", label: "标准" },
  { code: "polish", label: "地道润色" },
  { code: "technical", label: "技术文档" },
  { code: "summary", label: "简洁摘要" },
];

export function normalizeTranslationStyle(value: string | null | undefined): TranslationStyle {
  return TRANSLATION_STYLES.some((style) => style.code === value)
    ? (value as TranslationStyle)
    : "standard";
}

function cleanGlossaryValue(value: unknown): string {
  if (typeof value !== "string") return "";
  return value.replace(/[\u0000-\u001f\u007f]/g, "").trim().slice(0, 60);
}

export function normalizeGlossaryEntries(raw: unknown): GlossaryEntry[] {
  if (!Array.isArray(raw)) return [];
  const seen = new Set<string>();
  const out: GlossaryEntry[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const record = item as { source?: unknown; target?: unknown };
    const source = cleanGlossaryValue(record.source);
    if (!source) continue;
    const key = source.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    const target = cleanGlossaryValue(record.target);
    out.push({ source, target });
    if (out.length >= MAX_GLOSSARY_ENTRIES) break;
  }
  return out;
}

export function loadGlossary(): GlossaryEntry[] {
  try {
    const raw = localStorage.getItem(GLOSSARY_KEY);
    if (!raw) return [];
    return normalizeGlossaryEntries(JSON.parse(raw));
  } catch {
    return [];
  }
}

export function saveGlossary(entries: GlossaryEntry[]): GlossaryEntry[] {
  const normalized = normalizeGlossaryEntries(entries);
  try {
    localStorage.setItem(GLOSSARY_KEY, JSON.stringify(normalized));
  } catch {
    // Storage may be unavailable.
  }
  return normalized;
}

const CJK_CHAR_RE = /^[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af]+$/;
const LATIN_WORD_RE = /^[\p{L}'-]+$/u;
const DICT_CJK_RE = /[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af]/u;
const DICT_FORBIDDEN_RE = /[.!?:;。！？：；()[\]{}<>=_`/\\@]/;

export function isDictionaryQuery(text: string): boolean {
  const trimmed = text.trim();
  if (!trimmed || [...trimmed].length > 40 || /[\r\n]/.test(trimmed)) return false;
  const lower = trimmed.toLowerCase();
  if (lower.startsWith("http://") || lower.startsWith("https://") || lower.startsWith("www.")) {
    return false;
  }
  if (DICT_FORBIDDEN_RE.test(trimmed)) return false;
  const fields = trimmed.split(/\s+/);
  if (fields.length === 0 || fields.length > 3) return false;
  if (fields.length === 1 && CJK_CHAR_RE.test(fields[0]!)) {
    return [...fields[0]!].length <= 6;
  }
  return fields.every(
    (token) =>
      LATIN_WORD_RE.test(token) &&
      /\p{L}/u.test(token) &&
      !DICT_CJK_RE.test(token),
  );
}

export function formatCharCount(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return "";
  return `${[...trimmed].length} 字符`;
}

const BLOCK_START_RE = /^(?:[-*•·]\s+|\d+[.)、]\s*|[（(]\d+[）)]\s*|#{1,6}\s+|>\s*)/;
const HEADING_RE = /^#{1,6}\s+/;
const HYPHEN_END_RE = /[A-Za-z]-$/;
const LOWER_START_RE = /^[a-z]/;
const CJK_END_RE = /[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af，。！？；：、》」』）】]$/;
const CJK_START_RE = /^[\u4e00-\u9fff\u3040-\u30ff\uac00-\ud7af《「『（【]/;

/**
 * Merges unintended soft line breaks copied from PDFs, terminals, or OCR
 * while preserving paragraph breaks, Markdown headings/lists, and fenced code.
 */
export function mergeBrokenLines(input: string): string {
  const lines = input.replace(/\r\n?/g, "\n").split("\n");
  const out: string[] = [];
  let inCodeFence = false;
  let pendingBlank = false;
  let prevWasHeading = false;

  for (const rawLine of lines) {
    const trimmed = rawLine.trim();
    if (trimmed.startsWith("```")) {
      if (pendingBlank && out.length > 0) {
        out.push("");
        pendingBlank = false;
      }
      out.push(rawLine);
      inCodeFence = !inCodeFence;
      prevWasHeading = false;
      continue;
    }
    if (inCodeFence) {
      out.push(rawLine);
      continue;
    }
    if (trimmed === "") {
      if (out.length > 0) pendingBlank = true;
      prevWasHeading = false;
      continue;
    }
    if (pendingBlank) {
      out.push("");
      out.push(trimmed);
      pendingBlank = false;
      prevWasHeading = HEADING_RE.test(trimmed);
      continue;
    }
    if (out.length === 0 || BLOCK_START_RE.test(trimmed) || prevWasHeading) {
      out.push(trimmed);
      prevWasHeading = HEADING_RE.test(trimmed);
      continue;
    }
    const prev = out[out.length - 1]!;
    if (HYPHEN_END_RE.test(prev) && LOWER_START_RE.test(trimmed)) {
      out[out.length - 1] = prev.slice(0, -1) + trimmed;
    } else if (CJK_END_RE.test(prev) && CJK_START_RE.test(trimmed)) {
      out[out.length - 1] = prev + trimmed;
    } else {
      out[out.length - 1] = `${prev} ${trimmed}`;
    }
    prevWasHeading = false;
  }

  return out.join("\n");
}
