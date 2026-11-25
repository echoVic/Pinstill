import {
  saveTTSSettings,
  testTTS,
  ttsSettings,
  type TTSSettings,
} from "./bridge";
import { playAudioPayload } from "./translate-speech";
import { flashButtonSuccess, setButtonPending } from "./util";

function ttsRow(control: HTMLElement, label: string, description: string, labelFor?: string): HTMLElement {
  const row = document.createElement("div");
  row.className = "setting-row";
  const copy = document.createElement("div");
  const title = document.createElement("label");
  if (labelFor) title.htmlFor = labelFor;
  title.className = "setting-label";
  title.textContent = label;
  const hint = document.createElement("div");
  hint.id = `${labelFor || control.id || "tts"}-hint`;
  hint.className = "setting-description";
  hint.textContent = description;
  if (labelFor) control.setAttribute("aria-describedby", hint.id);
  copy.append(title, hint);
  const field = document.createElement("div");
  field.className = "settings-control";
  field.append(control);
  row.append(copy, field);
  return row;
}

function ttsInput(id: string, type: string, maxLength: number, placeholder: string): HTMLInputElement {
  const field = document.createElement("input");
  field.id = id;
  field.type = type;
  field.maxLength = maxLength;
  field.placeholder = placeholder;
  field.autocomplete = "off";
  field.spellcheck = false;
  return field;
}

export function renderTTSSettings(available: boolean): HTMLElement {
  const card = document.createElement("section");
  card.className = "settings-card";
  const heading = document.createElement("h3");
  heading.textContent = "语音朗读（TTS）";
  const form = document.createElement("form");
  form.id = "ttsSettingsForm";
  form.autocomplete = "off";

  const provider = document.createElement("select");
  provider.id = "ttsProvider";
  for (const [value, label] of [
    ["system", "系统语音"],
    ["deepseek", "DeepSeek TTS（/v1/tools/tts）"],
    ["openai-local", "本地 OpenAI 兼容 TTS（/v1/audio/speech）"],
    ["openai-cloud", "云端 OpenAI 兼容 TTS（/v1/audio/speech）"],
  ] as const) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    provider.append(option);
  }
  provider.value = "system";
  const providerRow = ttsRow(
    provider,
    "朗读引擎",
    "默认使用系统语音。配置其他语音服务后，点击朗读会将所选原文或译文发送到该服务。",
    provider.id,
  );

  const base = ttsInput("ttsBaseUrl", "url", 512, "");
  const baseRow = ttsRow(
    base,
    "服务地址",
    "DeepSeek 默认为 https://api.deepseek.com/v1；OpenAI 兼容 TTS 填写到域名或 /v1 路径。",
    base.id,
  );

  const model = ttsInput("ttsModel", "text", 200, "如 gpt-4o-mini-tts / tts-1 / kokoro");
  const modelRow = ttsRow(
    model,
    "TTS 模型名",
    "填写专用语音合成模型 ID（DeepSeek TTS 无需模型名）。",
    model.id,
  );

  const voice = ttsInput("ttsVoice", "text", 64, "");
  const voiceRow = ttsRow(
    voice,
    "音色（Voice）",
    "DeepSeek 支持 stella（女声）/ tide（男声）；OpenAI 兼容服务可填 alloy、coral 等。",
    voice.id,
  );

  const key = ttsInput("ttsApiKey", "password", 1024, "");
  key.autocomplete = "new-password";
  const keyRow = ttsRow(
    key,
    "API Key",
    "密钥保存在系统钥匙串。若与上方云端模型地址一致，留空可直接复用已保存的密钥。",
    key.id,
  );

  const footer = document.createElement("div");
  footer.className = "model-settings-footer";
  const status = document.createElement("p");
  status.id = "ttsSettingsStatus";
  status.className = "model-settings-status";
  status.setAttribute("role", "status");
  const actions = document.createElement("div");
  actions.className = "model-settings-actions";
  const testButton = document.createElement("button");
  testButton.id = "ttsTestButton";
  testButton.type = "button";
  testButton.className = "btn small";
  testButton.textContent = "试听测试";
  const save = document.createElement("button");
  save.type = "submit";
  save.className = "btn small";
  save.textContent = "保存语音配置";
  actions.append(testButton, save);
  footer.append(status, actions);

  form.append(providerRow, baseRow, modelRow, voiceRow, keyRow, footer);
  card.append(heading, form);

  let busy = false;
  let saved: TTSSettings | undefined;
  let activePreview: { stop: () => void } | null = null;

  function setStatus(text: string, kind: "info" | "success" | "error" = "info"): void {
    status.classList.toggle("error", kind === "error");
    status.classList.toggle("success", kind === "success");
    status.textContent = text;
  }

  function updateFields(): void {
    const mode = provider.value;
    const enabled = mode !== "system";
    const needsModel = mode === "openai-local" || mode === "openai-cloud";
    baseRow.hidden = !enabled;
    modelRow.hidden = !needsModel;
    voiceRow.hidden = !enabled;
    keyRow.hidden = !enabled;

    provider.disabled = !available || busy;
    base.disabled = !available || busy || !enabled;
    model.disabled = !available || busy || !needsModel;
    voice.disabled = !available || busy || !enabled;
    key.disabled = !available || busy || !enabled;
    testButton.disabled = !available || busy || !enabled;
    save.disabled = !available || busy;

    base.required = enabled;
    model.required = needsModel;

    if (mode === "deepseek") {
      if (!base.value.trim()) base.value = "https://api.deepseek.com/v1";
      base.placeholder = "https://api.deepseek.com/v1";
      voice.placeholder = "stella（女声）或 tide（男声）";
    } else if (mode === "openai-local") {
      base.placeholder = "http://127.0.0.1:8880/v1";
      voice.placeholder = "如 af_sky / alloy";
    } else {
      base.placeholder = "https://api.openai.com/v1";
      voice.placeholder = "如 alloy / coral / sage";
    }

    const sameEndpoint = Boolean(
      saved?.hasApiKey && saved.provider === mode && saved.baseUrl === base.value.trim(),
    );
    if (sameEndpoint) {
      key.placeholder = "已保存，留空沿用";
    } else if (mode === "openai-local") {
      key.placeholder = "可选";
    } else {
      key.placeholder = "留空将尝试复用同地址模型密钥";
    }
  }

  function showSettings(settings: TTSSettings): void {
    saved = settings;
    provider.value = settings.provider || "system";
    base.value = settings.baseUrl;
    model.value = settings.model;
    voice.value = settings.voice;
    key.value = "";
    updateFields();
  }

  function setBusy(pending: boolean, actor: HTMLButtonElement = save): void {
    busy = pending;
    setButtonPending(actor, pending);
    updateFields();
  }

  function showError(error: unknown): void {
    setStatus(error instanceof Error ? error.message : String(error), "error");
  }

  provider.addEventListener("change", () => {
    if (provider.value === "openai-local" && base.value.trim() === "https://api.deepseek.com/v1") {
      base.value = "";
    }
    updateFields();
  });
  base.addEventListener("input", updateFields);

  testButton.addEventListener("click", async () => {
    if (!available || busy || provider.value === "system") return;
    if (activePreview) {
      activePreview.stop();
      activePreview = null;
    }
    setBusy(true, testButton);
    setStatus("正在合成试听音频…");
    try {
      const audio = await testTTS({
        provider: provider.value,
        baseUrl: base.value,
        model: model.value,
        voice: voice.value,
        apiKey: key.value,
      });
      activePreview = playAudioPayload(audio, {
        onEnd: () => {
          activePreview = null;
        },
        onError: (msg) => {
          activePreview = null;
          setStatus(msg, "error");
        },
      });
      setStatus("试听成功", "success");
      flashButtonSuccess(testButton);
    } catch (error) {
      showError(error);
    } finally {
      setBusy(false, testButton);
    }
  });

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!available || busy) return;
    setBusy(true, save);
    setStatus("正在保存语音配置…");
    try {
      const settings = await saveTTSSettings({
        provider: provider.value,
        baseUrl: base.value,
        model: model.value,
        voice: voice.value,
        apiKey: key.value,
      });
      showSettings(settings);
      setStatus(
        settings.provider === "system"
          ? "已切换为系统语音"
          : "语音配置已保存在系统钥匙串",
        "success",
      );
      flashButtonSuccess(save);
    } catch (error) {
      showError(error);
    } finally {
      setBusy(false, save);
    }
  });

  updateFields();
  if (!available) {
    save.disabled = true;
    status.textContent = "请在 macOS 桌面应用中配置远程 TTS 语音。";
    return card;
  }
  setStatus("正在读取语音配置…");
  setBusy(true, save);
  void Promise.resolve()
    .then(() => ttsSettings())
    .then((settings) => {
      showSettings(settings);
      setStatus(
        settings.provider === "system"
          ? "当前使用系统语音。"
          : "远程 TTS 语音已配置并保存在系统钥匙串。",
      );
    })
    .catch(showError)
    .finally(() => setBusy(false, save));
  return card;
}
