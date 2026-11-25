import { cancelTranslation, copyRecognizedText, modelSettings, openRecognizedLink, pollTranslation, recognizeSelection, recognizeWithModel, startTranslation } from "./bridge";
import type { ModelSettings, RecognitionCode, RecognitionMode } from "./bridge";
import { translateLanguageLabel } from "./languages";
import type { Preferences } from "./settings";
import { setButtonPending } from "./util";

export type RecognitionAction = RecognitionMode | "translate";
type RecognitionPreferences = Pick<Preferences, "ocrLanguage" | "translateTo" | "sideBySide">;
interface TextCaptureRequest {
  mode: "ocr" | "translate";
  preferences: RecognitionPreferences;
  image: () => Promise<string>;
}

export class RecognitionPanel {
  private readonly title: HTMLElement;
  private readonly status: HTMLElement;
  private readonly results: HTMLElement;
  private readonly options: HTMLElement;
  private operation: AbortController | null = null;

  constructor(private readonly dialog: HTMLDialogElement) {
    this.title = dialog.querySelector<HTMLElement>("#recognitionTitle")!;
    this.status = dialog.querySelector<HTMLElement>("#recognitionStatus")!;
    this.results = dialog.querySelector<HTMLElement>("#recognitionResults")!;
    this.options = dialog.querySelector<HTMLElement>("#recognitionOptions")!;
    dialog.querySelector<HTMLButtonElement>("#recognitionClose")!.addEventListener("click", () => this.close());
    dialog.addEventListener("cancel", (event) => {
      event.preventDefault();
      this.close();
    });
  }

  get open(): boolean {
    return this.dialog.open;
  }

  close(): void {
    this.operation?.abort();
    this.operation = null;
    if (this.dialog.open) this.dialog.close();
    this.dialog.classList.remove("translation-panel");
    this.results.replaceChildren();
    this.options.replaceChildren();
    this.setStatus("");
  }

  async show(mode: RecognitionAction, preferences: RecognitionPreferences, image: () => Promise<string>): Promise<void> {
    if (this.open) return;
    const signal = this.startOperation();
    this.title.textContent = mode === "translate" ? "截图翻译" : mode === "ocr" ? "提取文字" : "识别二维码";
    this.setStatus(mode === "qr" ? "正在识别二维码…" : "正在读取模型配置…");
    this.dialog.showModal();
    try {
      if (mode === "qr") {
        const png = await image();
        if (signal.aborted) return;
        const result = await this.recognizeLocally(mode, preferences.ocrLanguage, png, signal);
        if (signal.aborted) return;
        const codes = result.codes ?? [];
        this.setStatus(codes.length ? "" : "选区里没有识别到二维码");
        for (const code of codes) this.renderCode(code);
        return;
      }
      const request: TextCaptureRequest = { mode, preferences, image };
      const settings = await modelSettings(signal).catch((error: unknown) => {
        if (mode === "translate") throw error;
        return undefined;
      });
      if (signal.aborted) return;
      if (!settings?.revision) {
        if (mode === "translate") throw new Error("请先在偏好设置 → 识别与翻译中配置模型服务");
        await this.readText(request, undefined, "native");
      } else if (settings.mode === "cloud") {
        this.confirmCloudOCR(request, settings);
      } else {
        await this.readText(request, settings, "model");
      }
    } catch (error) {
      if (!signal.aborted) this.setStatus(`${mode === "translate" ? "翻译失败" : "识别失败"}：${this.message(error)}`, true);
    }
  }

  private startOperation(): AbortSignal {
    this.operation?.abort();
    this.operation = new AbortController();
    return this.operation.signal;
  }

  private async recognizeLocally(mode: RecognitionMode, language: string, png: string, signal: AbortSignal) {
    const request = new AbortController();
    const cancel = () => request.abort(signal.reason);
    signal.addEventListener("abort", cancel, { once: true });
    let timer = 0;
    try {
      const timeout = new Promise<never>((_, reject) => {
        timer = window.setTimeout(() => {
          reject(new Error("本机识别超时，请重试"));
          request.abort();
        }, 15000);
      });
      return await Promise.race([timeout, recognizeSelection(mode, language, png, request.signal)]);
    } finally {
      window.clearTimeout(timer);
      signal.removeEventListener("abort", cancel);
    }
  }

  private confirmCloudOCR(request: TextCaptureRequest, settings: ModelSettings): void {
    const translating = request.mode === "translate";
    const note = document.createElement("p");
    note.className = "recognition-consent";
    note.setAttribute("data-recognition-model-consent", "");
    const host = new URL(settings.baseUrl).host;
    note.textContent = translating
      ? `将选区截图发送到 ${host} 识别并翻译。也可以在本机取字，仅发送文字。`
      : `将当前选区截图发送到 ${host} 进行识别。`;
    const actions = this.actions();
    this.localOCRButton(actions, request, settings);
    const confirm = this.button(actions, "confirm-model-ocr", translating ? "发送并翻译" : "发送并识别", () => this.readText(request, settings, "model"));
    confirm.classList.add("primary");
    this.options.replaceChildren(note, actions);
    this.setStatus("");
  }

  private localOCRButton(actions: HTMLElement, request: TextCaptureRequest, settings: ModelSettings): void {
    const label = request.mode === "translate" ? "在本机取字后翻译" : "使用本机 OCR";
    this.button(actions, "local-ocr", label, () => this.readText(request, settings, "native"));
  }

  private async readText(request: TextCaptureRequest, settings: ModelSettings | undefined, source: "model" | "native"): Promise<void> {
    const { mode, preferences, image } = request;
    const signal = this.startOperation();
    const model = source === "model" ? settings : undefined;
    this.results.replaceChildren();
    this.options.replaceChildren();
    this.dialog.classList.remove("translation-panel");
    this.title.textContent = mode === "translate" ? "截图翻译" : model ? "模型识别" : "提取文字";
    this.setStatus(model ? "模型正在识别选区…" : "正在本机提取文字…");
    if (model) {
      const actions = this.actions();
      this.localOCRButton(actions, request, model);
      this.options.append(actions);
    }
    try {
      const png = await image();
      if (signal.aborted) return;
      const text = model
        ? await recognizeWithModel(model.revision, png, signal)
        : (await this.recognizeLocally("ocr", preferences.ocrLanguage, png, signal)).text;
      if (signal.aborted) return;
      if (!text?.trim()) {
        this.setStatus("选区里没有识别到文字");
        return;
      }
      if (mode === "translate" && settings) {
        await this.translateAndRender(settings, preferences, text, signal);
      } else {
        if (model) this.renderModelText(model, preferences, text);
        else this.renderText(text);
        this.setStatus("");
      }
    } catch (error) {
      if (!signal.aborted) this.setStatus(`${mode === "translate" ? "翻译失败" : "识别失败"}：${this.message(error)}`, true);
    }
  }

  private async translateAndRender(settings: ModelSettings, preferences: RecognitionPreferences, text: string, signal: AbortSignal): Promise<void> {
    this.title.textContent = "截图翻译";
    this.dialog.classList.toggle("translation-panel", preferences.sideBySide);
    const columns = document.createElement("div");
    columns.className = "translation-columns";
    const original = this.renderTranslationText("原文", "original", text);
    if (preferences.sideBySide) columns.append(original);
    this.results.replaceChildren(columns);
    const translated = this.renderTranslationColumn("译文", "translation", "");
    columns.append(translated.column);
    const bilingual = this.actions();
    this.button(bilingual, "copy-bilingual", "复制对照", (signal) => {
      const value = translated.field.value.trim();
      if (!value) return Promise.reject(new Error("还没有译文"));
      return copyRecognizedText(`原文：\n${text}\n\n译文：\n${value}`, signal);
    }, "已复制对照");
    this.results.append(bilingual);
    this.setStatus(`正在翻译为 ${translateLanguageLabel(preferences.translateTo)}…`);
    let token = "";
    const cancelRemote = () => {
      if (token) void cancelTranslation(token);
    };
    signal.addEventListener("abort", cancelRemote, { once: true });
    try {
      token = await startTranslation(settings.revision, text, preferences.translateTo, "auto");
      for (;;) {
        if (signal.aborted) {
          void cancelTranslation(token);
          return;
        }
        const update = await pollTranslation(token);
        translated.field.value = update.text;
        if (update.error) throw new Error(update.error);
        if (update.done) break;
        await new Promise((resolve) => setTimeout(resolve, 120));
      }
      this.setStatus("");
    } catch (error) {
      if (!signal.aborted && !preferences.sideBySide) columns.prepend(original);
      throw error;
    } finally {
      signal.removeEventListener("abort", cancelRemote);
    }
  }

  private renderTranslationText(label: string, action: string, text: string): HTMLElement {
    return this.renderTranslationColumn(label, action, text).column;
  }

  private renderTranslationColumn(label: string, action: string, text: string): { column: HTMLElement; field: HTMLTextAreaElement } {
    const column = document.createElement("div");
    column.className = "translation-column";
    const heading = document.createElement("h3");
    heading.textContent = label;
    const field = this.textField(label, text);
    const actions = this.actions();
    this.button(actions, `copy-${action}`, `复制${label}`, (signal) => copyRecognizedText(field.value, signal), `已复制${label}`);
    column.append(heading, field, actions);
    return { column, field };
  }

  private renderModelText(settings: ModelSettings, preferences: RecognitionPreferences, text: string): void {
    this.results.replaceChildren();
    this.dialog.classList.remove("translation-panel");
    this.title.textContent = "模型识别";
    if (!text) return;
    this.renderText(text);
    const actions = this.actions();
    this.button(actions, "translate-model-text", "翻译识别结果", async (signal) => {
      try {
        await this.translateAndRender(settings, preferences, text, signal);
      } catch (error) {
        if (!signal.aborted) this.renderModelText(settings, preferences, text);
        throw error;
      }
    });
    this.results.append(actions);
  }

  private textField(label: string, text: string): HTMLTextAreaElement {
    const field = document.createElement("textarea");
    field.className = "recognition-text";
    field.setAttribute("aria-label", label);
    field.readOnly = true;
    field.value = text;
    return field;
  }

  private renderText(text: string): void {
    const field = this.textField("识别出的文字", text);
    const actions = this.actions();
    const selected = () => field.value.slice(field.selectionStart, field.selectionEnd);
    const copySelection = this.button(actions, "copy-selection", "复制选中文字", (signal) => copyRecognizedText(selected(), signal), "已复制选中文字");
    copySelection.disabled = true;
    field.addEventListener("select", () => { copySelection.disabled = !selected(); });
    this.button(actions, "copy-all", "复制全文", (signal) => copyRecognizedText(field.value, signal), "已复制全文");
    this.results.append(field, actions);
  }

  private renderCode(code: RecognitionCode): void {
    const row = document.createElement("div");
    row.className = "recognition-code";
    const value = document.createElement("bdi");
    value.setAttribute("data-recognition-code", "");
    value.textContent = code.text;
    const actions = this.actions();
    this.button(actions, "copy-code", "复制", (signal) => copyRecognizedText(code.text, signal), "已复制二维码内容");
    const url = code.url;
    if (url) this.button(actions, "open", "在浏览器打开", (signal) => openRecognizedLink(url, signal));
    row.append(value, actions);
    this.results.append(row);
  }

  private actions(): HTMLElement {
    const actions = document.createElement("div");
    actions.className = "recognition-actions";
    return actions;
  }

  private button(
    parent: HTMLElement,
    action: string,
    label: string,
    run: (signal: AbortSignal) => Promise<void>,
    success = "",
  ): HTMLButtonElement {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "action-btn";
    button.textContent = label;
    button.setAttribute(`data-recognition-${action}`, "");
    button.addEventListener("click", async () => {
      const signal = this.operation?.signal;
      if (!signal || signal.aborted || button.disabled) return;
      setButtonPending(button, true);
      try {
        await run(signal);
        if (!signal.aborted) this.setStatus(success);
      } catch (error) {
        if (!signal.aborted) this.setStatus(`${label}失败：${this.message(error)}`, true);
      } finally {
        setButtonPending(button, false);
      }
    });
    parent.append(button);
    return button;
  }

  private setStatus(text: string, error = false): void {
    this.status.textContent = text;
    this.status.classList.toggle("error", error);
  }

  private message(error: unknown): string {
    return error instanceof Error ? error.message : String(error);
  }
}
