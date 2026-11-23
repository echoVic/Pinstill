import { copyPixelColor, sampleOverlayPixels, type PixelSample } from "./bridge";

type Point = { x: number; y: number };
type Viewport = { width: number; height: number };

const SAMPLE_INTERVAL = 65;

export class PrecisionInspector {
  private readonly loupe = document.getElementById("precisionLoupe")!;
  private readonly grid = document.getElementById("precisionPixels") as HTMLImageElement;
  private readonly reading = document.getElementById("precisionColor")!;
  private readonly ruler = document.getElementById("precisionRuler")!;
  private readonly line = document.getElementById("precisionRulerLine")!;
  private readonly label = document.getElementById("precisionRulerLabel")!;
  private point: Point | null = null;
  private origin: Point | null = null;
  private sample: PixelSample | null = null;
  private sampling = false;
  private loupeEnabled = false;
  private loupeSession = 0;
  private revision = 0;
  private sampleTimer: number | undefined;

  constructor(private viewport: () => Viewport, private notify: (message: string) => void) {}

  move(x: number, y: number): void {
    if (!Number.isFinite(x) || !Number.isFinite(y)) return;
    if (this.point?.x === x && this.point.y === y) return;
    this.point = { x, y };
    this.invalidateSample();
    this.renderRuler();
    if (this.loupeEnabled) {
      this.loupe.hidden = false;
      this.place(this.loupe, this.point, 134, 161);
      this.scheduleSample();
    }
  }

  reset(): void {
    this.point = null;
    this.origin = null;
    this.ruler.hidden = true;
    this.closeLoupe();
  }

  leave(): void {
    this.point = null;
    ++this.loupeSession;
    this.invalidateSample();
    this.loupe.hidden = true;
    this.grid.hidden = true;
    this.ruler.hidden = true;
  }

  toggleLoupe(): void {
    if (this.loupeEnabled) {
      this.closeLoupe();
      return;
    }
    if (!this.point) return;
    this.loupeEnabled = true;
    this.loupe.hidden = false;
    this.place(this.loupe, this.point, 134, 161);
    this.scheduleSample(true);
  }

  toggleRuler(): void {
    this.origin = this.origin ? null : this.point;
    this.renderRuler();
  }

  async copyColor(): Promise<void> {
    if (!this.sample && !this.loupeEnabled) this.toggleLoupe();
    if (!this.sample) {
      this.notify("请等取色读数更新后按 C 复制");
      return;
    }
    const hex = this.sample.hex;
    try {
      await copyPixelColor(hex);
      if (this.loupeEnabled) this.notify(`已复制 ${hex.toUpperCase()}`);
    } catch {
      if (this.loupeEnabled) this.notify("无法复制颜色");
    }
  }

  private closeLoupe(): void {
    this.loupeEnabled = false;
    ++this.loupeSession;
    this.loupe.hidden = true;
    this.invalidateSample();
    this.grid.removeAttribute("src");
    this.grid.hidden = true;
    if (this.sampleTimer !== undefined) window.clearTimeout(this.sampleTimer);
    this.sampleTimer = undefined;
  }

  private invalidateSample(): void {
    ++this.revision;
    this.sample = null;
    this.reading.textContent = "读取像素…";
  }

  private scheduleSample(immediate = false): void {
    if (!this.loupeEnabled || !this.point || this.sampling || this.sampleTimer !== undefined) return;
    if (immediate) {
      void this.readSample();
    } else {
      this.sampleTimer = window.setTimeout(() => {
        this.sampleTimer = undefined;
        void this.readSample();
      }, SAMPLE_INTERVAL);
    }
  }

  private async readSample(): Promise<void> {
    if (!this.loupeEnabled || !this.point) return;
    this.sampling = true;
    const revision = this.revision;
    const loupeSession = this.loupeSession;
    try {
      const result = await sampleOverlayPixels(this.point.x, this.point.y);
      if (loupeSession !== this.loupeSession) return;
      this.grid.src = `data:image/png;base64,${result.pngBase64}`;
      this.grid.hidden = false;
      if (revision !== this.revision) return;
      this.sample = result;
      this.reading.textContent = result.hex.toUpperCase();
    } catch {
      if (revision !== this.revision) return;
      this.grid.hidden = true;
      this.reading.textContent = "无法读取像素";
    } finally {
      this.sampling = false;
      if (revision !== this.revision) this.scheduleSample();
    }
  }

  private renderRuler(): void {
    const point = this.point;
    const origin = this.origin;
    this.ruler.hidden = !point || !origin;
    if (!point || !origin) return;
    const dx = point.x - origin.x;
    const dy = point.y - origin.y;
    const distance = Math.hypot(dx, dy);
    this.line.style.left = `${origin.x}px`;
    this.line.style.top = `${origin.y}px`;
    this.line.style.width = `${distance}px`;
    this.line.style.transform = `rotate(${Math.atan2(dy, dx)}rad)`;
    this.label.textContent = `${Math.round(Math.abs(dx))} × ${Math.round(Math.abs(dy))} pt · 距离 ${Math.round(distance)} pt`;
    this.place(this.label, point, 178, 28);
  }

  private place(element: HTMLElement, point: Point, width: number, height: number): void {
    const viewport = this.viewport();
    const margin = 8;
    const gap = 18;
    const left = point.x + width + gap + margin > viewport.width ? point.x - width - gap : point.x + gap;
    const top = point.y + height + gap + margin > viewport.height ? point.y - height - gap : point.y + gap;
    element.style.left = `${Math.max(margin, Math.min(left, viewport.width - width - margin))}px`;
    element.style.top = `${Math.max(margin, Math.min(top, viewport.height - height - margin))}px`;
  }
}
