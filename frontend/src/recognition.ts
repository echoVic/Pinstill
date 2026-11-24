import { copyRecognizedText, openRecognizedLink, recognizeSelection } from "./bridge";
import type { RecognitionCode, RecognitionMode } from "./bridge";
import { setButtonPending } from "./util";

export class RecognitionPanel {
  private readonly title: HTMLElement;
  private readonly status: HTMLElement;
  private readonly results: HTMLElement;
  private operation: AbortController | null = null;

  constructor(private readonly dialog: HTMLDialogElement) {
    this.title = dialog.querySelector<HTMLElement>("#recognitionTitle")!;
    this.status = dialog.querySelector<HTMLElement>("#recognitionStatus")!;
    this.results = dialog.querySelector<HTMLElement>("#recognitionResults")!;
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
    this.results.replaceChildren();
    this.setStatus("");
  }

  async show(mode: RecognitionMode, language: string, image: () => Promise<string>): Promise<void> {
    if (this.open) return;
    const controller = new AbortController();
    const signal = controller.signal;
    this.operation = controller;
    this.title.textContent = mode === "ocr" ? "提取文字" : "识别二维码";
    this.setStatus(mode === "ocr" ? "正在提取文字…" : "正在识别二维码…");
    this.dialog.showModal();
    try {
      const png = await image();
      if (signal.aborted) return;
      const result = await recognizeSelection(mode, language, png, signal);
      if (signal.aborted) return;
      if (mode === "ocr") {
        this.setStatus(result.text ? "" : "选区里没有识别到文字");
        if (result.text) this.renderText(result.text);
      } else {
        const codes = result.codes ?? [];
        this.setStatus(codes.length ? "" : "选区里没有识别到二维码");
        for (const code of codes) this.renderCode(code);
      }
    } catch (error) {
      if (!signal.aborted) this.setStatus(`识别失败：${this.message(error)}`, true);
    }
  }

  private renderText(text: string): void {
    const field = document.createElement("textarea");
    field.className = "recognition-text";
    field.setAttribute("aria-label", "识别出的文字");
    field.readOnly = true;
    field.value = text;
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
