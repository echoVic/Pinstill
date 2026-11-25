// Target languages offered by the translation UI. The codes are BCP 47 tags
// stored in preferences and sent to the model service; keep them in sync with
// modelLanguages in internal/app/languages.go.

export interface TargetLanguage {
  code: string;
  label: string;
}

export const TARGET_LANGUAGES: readonly TargetLanguage[] = [
  { code: "zh-Hans", label: "简体中文" },
  { code: "zh-Hant", label: "繁體中文" },
  { code: "en", label: "English" },
  { code: "ja", label: "日本語" },
  { code: "ko", label: "한국어" },
  { code: "fr", label: "Français" },
  { code: "de", label: "Deutsch" },
  { code: "es", label: "Español" },
  { code: "pt", label: "Português" },
  { code: "it", label: "Italiano" },
  { code: "ru", label: "Русский" },
  { code: "ar", label: "العربية" },
  { code: "hi", label: "हिन्दी" },
  { code: "th", label: "ไทย" },
  { code: "vi", label: "Tiếng Việt" },
  { code: "id", label: "Bahasa Indonesia" },
  { code: "tr", label: "Türkçe" },
  { code: "nl", label: "Nederlands" },
  { code: "pl", label: "Polski" },
  { code: "sv", label: "Svenska" },
];

export const DEFAULT_TRANSLATE_TO = "zh-Hans";

// Earlier builds stored the language label itself ("English"), so saved
// preferences need translating to codes before use.
const LEGACY_LANGUAGE_NAMES: Record<string, string> = {
  "简体中文": "zh-Hans",
  "English": "en",
  "日本語": "ja",
  "한국어": "ko",
};

export function normalizeTranslateTo(value: string): string {
  const legacy = LEGACY_LANGUAGE_NAMES[value];
  if (legacy) return legacy;
  return TARGET_LANGUAGES.some((language) => language.code === value) ? value : DEFAULT_TRANSLATE_TO;
}

export function translateLanguageLabel(code: string): string {
  return TARGET_LANGUAGES.find((language) => language.code === code)?.label ?? code;
}