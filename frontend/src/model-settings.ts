import { modelSettings, saveModelSettings, type ModelSettings } from "./bridge";
import { setButtonPending } from "./util";

function modelRow(control: HTMLInputElement | HTMLSelectElement, label: string, description: string): HTMLElement {
  const row = document.createElement("div");
  row.className = "setting-row";
  const copy = document.createElement("div");
  const title = document.createElement("label");
  title.htmlFor = control.id;
  title.className = "setting-label";
  title.textContent = label;
  const hint = document.createElement("div");
  hint.id = `${control.id}-hint`;
  hint.className = "setting-description";
  hint.textContent = description;
  control.setAttribute("aria-describedby", hint.id);
  copy.append(title, hint);
  const field = document.createElement("div");
  field.className = "settings-control";
  field.append(control);
  row.append(copy, field);
  return row;
}

function modelInput(id: string, type: string, maxLength: number, placeholder: string): HTMLInputElement {
  const field = document.createElement("input");
  field.id = id;
  field.type = type;
  field.maxLength = maxLength;
  field.placeholder = placeholder;
  field.autocomplete = "off";
  field.spellcheck = false;
  return field;
}

export function renderModelSettings(available: boolean): HTMLElement {
  const card = document.createElement("section");
  card.className = "settings-card";
  const heading = document.createElement("h3");
  heading.textContent = "模型服务";
  const form = document.createElement("form");
  form.id = "modelSettingsForm";
  form.autocomplete = "off";
  const mode = document.createElement("select");
  mode.id = "modelMode";
  for (const [value, label] of [["off", "关闭"], ["local", "本地模型"], ["cloud", "云端 API"]] as const) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    mode.append(option);
  }
  mode.value = "off";
  const modeRow = modelRow(mode, "接入方式", "配置后，提取文字和截图翻译优先用模型识别。发送选区图片到云端前会询问。");
  const base = modelInput("modelBaseUrl", "url", 512, "");
  const baseRow = modelRow(base, "服务地址", "填写 OpenAI 兼容接口的服务地址。只填到域名会自动补 /v1，填写完整路径则按原样使用。");
  const model = modelInput("modelName", "text", 200, "服务中的模型 ID");
  const modelNameRow = modelRow(model, "模型名", "优先选支持图片输入的模型。仅支持文字的模型可搭配本机取字来翻译。");
  const key = modelInput("modelApiKey", "password", 1024, "");
  key.autocomplete = "new-password";
  const keyRow = modelRow(key, "API Key", "密钥保存在系统钥匙串。切换云服务时请填写对应的密钥。");
  const rows = [baseRow, modelNameRow, keyRow];
  const controls = [mode, base, model, key];
  const footer = document.createElement("div");
  footer.className = "model-settings-footer";
  const status = document.createElement("p");
  status.className = "model-settings-status";
  status.setAttribute("role", "status");
  const save = document.createElement("button");
  save.type = "submit";
  save.className = "btn small";
  save.textContent = "保存模型配置";
  footer.append(status, save);
  form.append(modeRow, ...rows, footer);
  card.append(heading, form);

  let busy = false;
  let saved: ModelSettings | undefined;

  function updateFields(): void {
    const enabled = mode.value !== "off";
    for (const row of rows) row.hidden = !enabled;
    for (const control of controls) control.disabled = !available || busy || (control !== mode && !enabled);
    base.required = enabled;
    model.required = enabled;
    base.placeholder = mode.value === "local" ? "http://127.0.0.1:11434" : "https://api.openai.com";
    if (saved?.hasApiKey && saved.mode === mode.value && saved.baseUrl === base.value.trim()) {
      key.placeholder = "已保存，留空沿用";
    } else {
      key.placeholder = mode.value === "local" ? "可选" : "填写此服务的 API Key";
    }
  }

  function showSettings(settings: ModelSettings): void {
    saved = settings;
    mode.value = settings.mode;
    base.value = settings.baseUrl;
    model.value = settings.model;
    key.value = "";
  }

  function setBusy(pending: boolean): void {
    busy = pending;
    setButtonPending(save, pending);
    updateFields();
  }

  function showError(error: unknown): void {
    status.classList.add("error");
    status.textContent = error instanceof Error ? error.message : String(error);
  }

  mode.addEventListener("change", updateFields);
  base.addEventListener("input", updateFields);
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!available || busy) return;
    const input = { mode: mode.value, baseUrl: base.value, model: model.value, apiKey: key.value };
    setBusy(true);
    status.classList.remove("error");
    status.textContent = "正在保存…";
    try {
      const settings = await saveModelSettings(input);
      showSettings(settings);
      status.textContent = settings.mode === "off" ? "模型服务已关闭，配置已清除" : "模型配置已保存";
    } catch (error) {
      showError(error);
    } finally {
      setBusy(false);
    }
  });

  updateFields();
  if (!available) {
    save.disabled = true;
    status.textContent = "请在 macOS 桌面应用中配置模型服务。";
    return card;
  }
  status.textContent = "正在读取模型配置…";
  setBusy(true);
  void Promise.resolve().then(() => modelSettings()).then((settings) => {
    showSettings(settings);
    status.textContent = settings.mode === "off" ? "选择一种接入方式以启用。" : "模型配置已保存在系统钥匙串。";
  }).catch(showError).finally(() => setBusy(false));
  return card;
}
