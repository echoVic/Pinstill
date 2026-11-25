// Speech synthesis helper supporting both remote TTS models (DeepSeek / OpenAI)
// and WebView/system speechSynthesis fallback.

export type SpeechSlot = "source" | "result" | null;

export interface AudioPayload {
  mimeType: string;
  dataBase64: string;
}

const LANGUAGE_LOCALES: Record<string, string> = {
  "zh-Hans": "zh-CN",
  "zh-Hant": "zh-TW",
  en: "en-US",
  ja: "ja-JP",
  ko: "ko-KR",
  fr: "fr-FR",
  de: "de-DE",
  es: "es-ES",
  pt: "pt-BR",
  it: "it-IT",
  ru: "ru-RU",
  ar: "ar-SA",
  hi: "hi-IN",
  th: "th-TH",
  vi: "vi-VN",
  id: "id-ID",
  tr: "tr-TR",
  nl: "nl-NL",
  pl: "pl-PL",
  sv: "sv-SE",
};

const SYNTHETIC_VOICE_NAMES = new Set([
  "albert",
  "bad news",
  "bahh",
  "bells",
  "boing",
  "bubbles",
  "cellos",
  "eddy",
  "flo",
  "fred",
  "good news",
  "grandma",
  "grandpa",
  "jester",
  "junior",
  "kathy",
  "organ",
  "ralph",
  "reed",
  "rocko",
  "sandy",
  "shelley",
  "superstar",
  "trinoids",
  "whisper",
  "wobble",
  "zarvox",
]);

const HIGH_QUALITY_RE = /(?:siri|premium|enhanced|natural|优质|增强)/i;

function isSyntheticNoveltyVoice(name: string): boolean {
  const base = name.split("(", 1)[0]!.trim().toLowerCase();
  return SYNTHETIC_VOICE_NAMES.has(base);
}

function normalizeLocale(locale: string): string {
  return locale.trim().replace(/_/g, "-").toLowerCase();
}

function scoreVoice(voice: SpeechSynthesisVoice, target: string, base: string): number {
  const lang = normalizeLocale(voice.lang);
  let score = 0;
  if (!isSyntheticNoveltyVoice(voice.name)) score += 1000;
  if (voice.localService) score += 200;
  if (HIGH_QUALITY_RE.test(voice.name)) score += 50;
  if (lang === target) score += 20;
  else if (lang.startsWith(`${base}-`) || lang === base) score += 10;
  if (voice.default) score += 5;
  return score;
}

export function speechLocaleFor(langCode: string, text: string): string {
  if (langCode && langCode !== "auto" && LANGUAGE_LOCALES[langCode]) {
    return LANGUAGE_LOCALES[langCode]!;
  }
  if (/[\u3040-\u30ff]/.test(text)) return "ja-JP";
  if (/[\uac00-\ud7af]/.test(text)) return "ko-KR";
  if (/[\u4e00-\u9fff]/.test(text)) return "zh-CN";
  if (/[\u0400-\u04ff]/.test(text)) return "ru-RU";
  if (/[\u0600-\u06ff]/.test(text)) return "ar-SA";
  if (/[\u0e00-\u0e7f]/.test(text)) return "th-TH";
  if (/[\u0900-\u097f]/.test(text)) return "hi-IN";
  return "en-US";
}

export function pickOfflineVoice(synth: SpeechSynthesis, locale: string): SpeechSynthesisVoice | null {
  if (typeof synth.getVoices !== "function") return null;
  const voices = synth.getVoices();
  if (!voices.length) return null;
  const target = normalizeLocale(locale);
  const base = target.split("-")[0]!;
  const matches = voices.filter((voice) => {
    const lang = normalizeLocale(voice.lang);
    return lang === target || lang.startsWith(`${base}-`) || lang === base;
  });
  if (!matches.length) return null;
  matches.sort((a, b) => scoreVoice(b, target, base) - scoreVoice(a, target, base));
  return matches[0] ?? null;
}

export function playAudioPayload(
  audio: AudioPayload,
  callbacks: { onEnd: () => void; onError: (message: string) => void },
): { stop: () => void } | null {
  const win = window as unknown as { Audio?: typeof Audio };
  const globalObj = globalThis as unknown as { Audio?: typeof Audio };
  const AudioCtor = win.Audio ?? globalObj.Audio;
  if (typeof AudioCtor !== "function") return null;

  let objectURL = "";
  let src = `data:${audio.mimeType};base64,${audio.dataBase64}`;
  if (
    typeof URL !== "undefined" &&
    typeof URL.createObjectURL === "function" &&
    typeof atob === "function"
  ) {
    try {
      const binary = atob(audio.dataBase64);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i += 1) {
        bytes[i] = binary.charCodeAt(i);
      }
      objectURL = URL.createObjectURL(new Blob([bytes], { type: audio.mimeType }));
      src = objectURL;
    } catch {
      objectURL = "";
    }
  }

  const element = new AudioCtor(src);
  let stopped = false;

  const cleanup = () => {
    element.onended = null;
    element.onerror = null;
    if (objectURL && typeof URL.revokeObjectURL === "function") {
      try {
        URL.revokeObjectURL(objectURL);
      } catch {
        // Ignore cleanup errors.
      }
      objectURL = "";
    }
  };

  element.onended = () => {
    if (stopped) return;
    cleanup();
    callbacks.onEnd();
  };
  element.onerror = () => {
    if (stopped) return;
    cleanup();
    callbacks.onError("语音播放失败");
  };

  try {
    const played = element.play();
    if (played && typeof played.catch === "function") {
      void played.catch(() => {
        if (stopped) return;
        cleanup();
        callbacks.onError("语音播放失败");
      });
    }
  } catch {
    cleanup();
    callbacks.onError("语音播放失败");
    return null;
  }

  return {
    stop() {
      if (stopped) return;
      stopped = true;
      try {
        element.pause();
      } catch {
        // Ignore pause errors.
      }
      cleanup();
    },
  };
}

export interface SpeechController {
  readonly supported: boolean;
  isAvailable(): boolean;
  setRemote(revision: string | null): void;
  active(): SpeechSlot;
  speak(slot: "source" | "result", text: string, langCode: string): boolean;
  stop(): void;
}

export function createSpeechController(callbacks: {
  onChange: (active: SpeechSlot) => void;
  onError: (message: string) => void;
  onSynthesizing?: (slot: SpeechSlot) => void;
  synthesize?: (revision: string, text: string, signal: AbortSignal) => Promise<AudioPayload>;
}): SpeechController {
  const win = window as unknown as {
    speechSynthesis?: SpeechSynthesis;
    SpeechSynthesisUtterance?: typeof SpeechSynthesisUtterance;
  };
  const globalObj = globalThis as unknown as {
    speechSynthesis?: SpeechSynthesis;
    SpeechSynthesisUtterance?: typeof SpeechSynthesisUtterance;
  };
  const synth = win.speechSynthesis ?? globalObj.speechSynthesis;
  const Utterance = win.SpeechSynthesisUtterance ?? globalObj.SpeechSynthesisUtterance;
  const supported = Boolean(
    synth &&
      typeof synth.speak === "function" &&
      typeof synth.cancel === "function" &&
      typeof Utterance === "function",
  );

  let runId = 0;
  let current: SpeechSlot = null;
  let remoteRevision: string | null = null;
  let activeAbort: AbortController | null = null;
  let activePlayer: { stop: () => void } | null = null;

  function stop(): void {
    runId += 1;
    if (activeAbort) {
      activeAbort.abort();
      activeAbort = null;
    }
    if (activePlayer) {
      activePlayer.stop();
      activePlayer = null;
    }
    if (synth && typeof synth.cancel === "function") {
      try {
        synth.cancel();
      } catch {
        // Ignore platform speech cancellation errors.
      }
    }
    callbacks.onSynthesizing?.(null);
    if (current !== null) {
      current = null;
      callbacks.onChange(null);
    }
  }

  function speakRemote(slot: "source" | "result", trimmed: string, revision: string): boolean {
    if (!callbacks.synthesize) return false;
    const myRun = ++runId;
    const abort = new AbortController();
    activeAbort = abort;
    current = slot;
    callbacks.onChange(slot);
    callbacks.onSynthesizing?.(slot);

    void callbacks
      .synthesize(revision, trimmed, abort.signal)
      .then((audio) => {
        if (myRun !== runId || abort.signal.aborted) return;
        activeAbort = null;
        callbacks.onSynthesizing?.(null);
        const player = playAudioPayload(audio, {
          onEnd: () => {
            if (myRun !== runId) return;
            activePlayer = null;
            current = null;
            callbacks.onChange(null);
          },
          onError: (message) => {
            if (myRun !== runId) return;
            activePlayer = null;
            current = null;
            callbacks.onChange(null);
            callbacks.onError(message);
          },
        });
        if (!player) {
          if (myRun === runId) {
            current = null;
            callbacks.onChange(null);
            callbacks.onError("当前环境不支持播放合成音频");
          }
          return;
        }
        activePlayer = player;
      })
      .catch((error: unknown) => {
        if (myRun !== runId || abort.signal.aborted) return;
        activeAbort = null;
        callbacks.onSynthesizing?.(null);
        current = null;
        callbacks.onChange(null);
        callbacks.onError(error instanceof Error ? error.message : String(error));
      });
    return true;
  }

  function speak(slot: "source" | "result", text: string, langCode: string): boolean {
    if (current === slot) {
      stop();
      return true;
    }
    const trimmed = text.trim();
    if (!trimmed) return false;

    if (remoteRevision && callbacks.synthesize) {
      stop();
      return speakRemote(slot, trimmed, remoteRevision);
    }

    if (!supported || !synth || !Utterance) return false;

    stop();
    const myRun = ++runId;
    const utterance = new Utterance(trimmed);
    const locale = speechLocaleFor(langCode, trimmed);
    utterance.lang = locale;
    const voice = pickOfflineVoice(synth, locale);
    if (voice) utterance.voice = voice;

    utterance.onend = () => {
      if (myRun !== runId) return;
      current = null;
      callbacks.onChange(null);
    };
    utterance.onerror = (event) => {
      if (myRun !== runId) return;
      current = null;
      callbacks.onChange(null);
      const code = (event as { error?: string } | undefined)?.error;
      if (code !== "canceled" && code !== "interrupted") {
        callbacks.onError("语音朗读失败");
      }
    };

    current = slot;
    callbacks.onChange(slot);
    try {
      synth.speak(utterance);
      return true;
    } catch {
      current = null;
      callbacks.onChange(null);
      callbacks.onError("语音朗读失败");
      return false;
    }
  }

  return {
    supported,
    isAvailable: () => supported || Boolean(remoteRevision && callbacks.synthesize),
    setRemote(revision: string | null) {
      remoteRevision = revision || null;
    },
    active: () => current,
    speak,
    stop,
  };
}
