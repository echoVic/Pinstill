import {
  deleteModelPreset,
  modelSettings,
  saveModelPreset,
  saveModelSettings,
  selectModelPreset,
  testModelConnection,
  type ModelSettings,
} from "./bridge";
import { flashButtonSuccess, setButtonPending } from "./util";

function modelRow(control: HTMLElement, label: string, description: string, labelFor?: string): HTMLElement {
  const row = document.createElement("div");
  row.className = "setting-row";
  const copy = document.createElement("div");
  const title = document.createElement("label");
  if (labelFor) title.htmlFor = labelFor;
  title.className = "setting-label";
  title.textContent = label;
  const hint = document.createElement("div");
  hint.id = `${labelFor || control.id || "model"}-hint`;
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

  const presetBar = document.createElement("div");
  presetBar.className = "model-preset-bar";
  const presetSelect = document.createElement("select");
  presetSelect.id = "modelPresetSelect";
  presetSelect.setAttribute("aria-label", "模型预设");
  const addPreset = document.createElement("button");
  addPreset.id = "modelPresetAdd";
  addPreset.type = "button";
  addPreset.className = "btn small";
  addPreset.textContent = "新增";
  const deletePreset = document.createElement("button");
  deletePreset.id = "modelPresetDelete";
  deletePreset.type = "button";
  deletePreset.className = "btn small";
  deletePreset.textContent = "删除";
  presetBar.append(presetSelect, addPreset, deletePreset);
  const presetRow = modelRow(presetBar, "模型预设", "可保存多个本地或云端模型配置，在翻译窗口中随时切换。", presetSelect.id);

  const presetName = modelInput("modelPresetName", "text", 40, "例如：DeepSeek / 本地 Ollama");
  const presetNameRow = modelRow(presetName, "预设名称", "便于在翻译窗口快速识别当前模型。", presetName.id);

  const mode = document.createElement("select");
  mode.id = "modelMode";
  for (const [value, label] of [["off", "关闭"], ["local", "本地模型"], ["cloud", "云端 API"]] as const) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    mode.append(option);
  }
  mode.value = "off";
  const modeRow = modelRow(mode, "接入方式", "配置后，提取文字和截图翻译优先用模型识别。发送选区图片到云端前会询问。", mode.id);
  const base = modelInput("modelBaseUrl", "url", 512, "");
  const baseRow = modelRow(base, "服务地址", "填写 OpenAI 兼容接口的服务地址。只填到域名会自动补 /v1，填写完整路径则按原样使用。", base.id);
  const model = modelInput("modelName", "text", 200, "服务中的模型 ID");
  const modelNameRow = modelRow(model, "模型名", "优先选支持图片输入的模型。仅支持文字的模型可搭配本机取字来翻译。", model.id);
  const key = modelInput("modelApiKey", "password", 1024, "");
  key.autocomplete = "new-password";
  const keyRow = modelRow(key, "API Key", "密钥保存在系统钥匙串。切换云服务时请填写对应的密钥。", key.id);
  const rows = [presetNameRow, baseRow, modelNameRow, keyRow];
  const controls = [presetName, mode, base, model, key];

  const footer = document.createElement("div");
  footer.className = "model-settings-footer";
  const status = document.createElement("p");
  status.className = "model-settings-status";
  status.setAttribute("role", "status");
  const actions = document.createElement("div");
  actions.className = "model-settings-actions";
  const testButton = document.createElement("button");
  testButton.id = "modelTestConnection";
  testButton.type = "button";
  testButton.className = "btn small";
  testButton.textContent = "测试连接";
  const save = document.createElement("button");
  save.type = "submit";
  save.className = "btn small";
  save.textContent = "保存模型配置";
  actions.append(testButton, save);
  footer.append(status, actions);
  form.append(presetRow, modeRow, ...rows, footer);
  card.append(heading, form);

  let busy = false;
  let saved: ModelSettings | undefined;
  let editingPresetId = "";
  let creatingPreset = false;

  function setStatus(text: string, kind: "info" | "success" | "error" = "info"): void {
    status.classList.toggle("error", kind === "error");
    status.classList.toggle("success", kind === "success");
    status.textContent = text;
  }

  function renderPresetOptions(): void {
    presetSelect.replaceChildren();
    const presets = saved?.presets ?? [];
    if (creatingPreset) {
      const draft = document.createElement("option");
      draft.value = "__new__";
      draft.textContent = "新建预设…";
      presetSelect.append(draft);
    } else if (!saved?.activeId) {
      const offOption = document.createElement("option");
      offOption.value = "";
      offOption.textContent = presets.length > 0 ? "未启用模型" : "当前配置";
      presetSelect.append(offOption);
    }
    for (const preset of presets) {
      const option = document.createElement("option");
      option.value = preset.id;
      option.textContent = preset.name || preset.model;
      presetSelect.append(option);
    }
    if (creatingPreset) {
      presetSelect.value = "__new__";
    } else {
      presetSelect.value = saved?.activeId ?? "";
    }
  }

  function updateFields(): void {
    const enabled = mode.value !== "off";
    const presets = saved?.presets ?? [];
    for (const row of rows) row.hidden = !enabled;
    for (const control of controls) {
      control.disabled = !available || busy || (control !== mode && !enabled);
    }
    presetSelect.disabled = !available || busy || presets.length === 0;
    addPreset.disabled = !available || busy || creatingPreset || presets.length >= 8;
    deletePreset.hidden = !editingPresetId || creatingPreset;
    deletePreset.disabled = !available || busy || !editingPresetId || creatingPreset;
    testButton.disabled = !available || busy || !enabled;
    save.disabled = !available || busy;
    base.required = enabled;
    model.required = enabled;
    base.placeholder = mode.value === "local" ? "http://127.0.0.1:11434" : "https://api.openai.com";
    const activePreset = presets.find((entry) => entry.id === editingPresetId);
    const hasSavedKey = activePreset
      ? activePreset.hasApiKey && activePreset.mode === mode.value && activePreset.baseUrl === base.value.trim()
      : Boolean(saved?.hasApiKey && saved.mode === mode.value && saved.baseUrl === base.value.trim());
    if (!creatingPreset && hasSavedKey) {
      key.placeholder = "已保存，留空沿用";
    } else {
      key.placeholder = mode.value === "local" ? "可选" : "填写此服务的 API Key";
    }
  }

  function showSettings(settings: ModelSettings): void {
    saved = settings;
    creatingPreset = false;
    editingPresetId = settings.activeId ?? "";
    const activePreset = settings.presets?.find((entry) => entry.id === editingPresetId);
    presetName.value = activePreset?.name ?? "";
    mode.value = settings.mode;
    base.value = settings.baseUrl;
    model.value = settings.model;
    key.value = "";
    renderPresetOptions();
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

  mode.addEventListener("change", updateFields);
  base.addEventListener("input", updateFields);

  presetSelect.addEventListener("change", async () => {
    if (!available || busy) return;
    const selectedId = presetSelect.value;
    if (selectedId === "__new__") return;
    creatingPreset = false;
    setBusy(true, save);
    setStatus("正在切换预设…");
    try {
      const settings = await selectModelPreset(selectedId);
      showSettings(settings);
      const activeName = settings.presets?.find((entry) => entry.id === settings.activeId)?.name;
      setStatus(
        settings.mode === "off"
          ? "已暂存预设，当前未启用模型服务"
          : `已切换到预设：${activeName || settings.model}`,
        "success",
      );
    } catch (error) {
      renderPresetOptions();
      showError(error);
    } finally {
      setBusy(false, save);
    }
  });

  addPreset.addEventListener("click", () => {
    if (!available || busy || creatingPreset) return;
    creatingPreset = true;
    editingPresetId = "";
    presetName.value = "";
    if (mode.value === "off") mode.value = "cloud";
    base.value = "";
    model.value = "";
    key.value = "";
    renderPresetOptions();
    updateFields();
    setStatus("填写新的模型配置后点击保存。");
    presetName.focus();
  });

  deletePreset.addEventListener("click", async () => {
    if (!available || busy || !editingPresetId) return;
    const targetId = editingPresetId;
    setBusy(true, deletePreset);
    setStatus("正在删除预设…");
    try {
      const settings = await deleteModelPreset(targetId);
      showSettings(settings);
      setStatus(settings.presets?.length ? "已删除预设" : "已删除全部预设，模型服务已关闭", "success");
    } catch (error) {
      showError(error);
    } finally {
      setBusy(false, deletePreset);
    }
  });

  testButton.addEventListener("click", async () => {
    if (!available || busy || mode.value === "off") return;
    setBusy(true, testButton);
    setStatus("正在测试连接…");
    try {
      const result = await testModelConnection({
        id: creatingPreset ? "" : editingPresetId,
        name: presetName.value,
        mode: mode.value,
        baseUrl: base.value,
        model: model.value,
        apiKey: key.value,
      });
      setStatus(result, "success");
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
    setStatus("正在保存…");
    try {
      let settings: ModelSettings;
      if (mode.value === "off") {
        if (saved?.presets?.length) {
          settings = await selectModelPreset("");
        } else {
          settings = await saveModelSettings({ mode: "off", baseUrl: "", model: "", apiKey: "" });
        }
      } else if (
        creatingPreset ||
        editingPresetId !== "" ||
        Boolean(saved?.presets?.length) ||
        presetName.value.trim() !== ""
      ) {
        settings = await saveModelPreset({
          id: creatingPreset ? "" : editingPresetId,
          name: presetName.value,
          mode: mode.value,
          baseUrl: base.value,
          model: model.value,
          apiKey: key.value,
        });
      } else {
        settings = await saveModelSettings({
          mode: mode.value,
          baseUrl: base.value,
          model: model.value,
          apiKey: key.value,
        });
      }
      showSettings(settings);
      setStatus(
        settings.mode === "off"
          ? settings.presets?.length
            ? "已暂存预设，当前未启用模型服务"
            : "模型服务已关闭，配置已清除"
          : "模型配置已保存",
        "success",
      );
      flashButtonSuccess(save);
    } catch (error) {
      showError(error);
    } finally {
      setBusy(false, save);
    }
  });

  renderPresetOptions();
  updateFields();
  if (!available) {
    save.disabled = true;
    status.textContent = "请在 macOS 桌面应用中配置模型服务。";
    return card;
  }
  setStatus("正在读取模型配置…");
  setBusy(true, save);
  void Promise.resolve().then(() => modelSettings()).then((settings) => {
    showSettings(settings);
    setStatus(
      settings.mode === "off"
        ? settings.presets?.length
          ? "已保存预设但未启用，可随时从上方下拉框切换。"
          : "选择一种接入方式以启用。"
        : "模型配置已保存在系统钥匙串。",
    );
  }).catch(showError).finally(() => setBusy(false, save));
  return card;
}
