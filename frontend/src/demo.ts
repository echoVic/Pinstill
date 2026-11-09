// The browser preview keeps the original design's demo screenshot so the
// static prototype still looks and behaves like the design. In the desktop app
// real captures replace it; this module is only used when there is no MyGo
// runtime (and by the in-page demo history entries).

const BASE_WIDTH = 960;
const BASE_HEIGHT = 480;

function roundedRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  radius = 10,
  fill?: string,
): void {
  ctx.beginPath();
  ctx.roundRect(x, y, width, height, radius);
  if (fill) {
    ctx.fillStyle = fill;
    ctx.fill();
  } else {
    ctx.stroke();
  }
}

function drawScene(ctx: CanvasRenderingContext2D): void {
  ctx.fillStyle = "#f2efe7";
  ctx.fillRect(0, 0, BASE_WIDTH, BASE_HEIGHT);
  ctx.fillStyle = "#e4e0d6";
  ctx.fillRect(0, 0, BASE_WIDTH, 40);
  ["#d37d65", "#d2ae64", "#90ab86"].forEach((color, index) => {
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(20 + index * 16, 20, 4, 0, Math.PI * 2);
    ctx.fill();
  });
  ctx.font = "12px -apple-system, sans-serif";
  ctx.fillStyle = "#6d6c63";
  ctx.fillText("团队工作台 / 设计评审", 90, 24);
  ctx.fillStyle = "#e8e3d7";
  ctx.fillRect(0, 40, 172, BASE_HEIGHT - 40);
  ctx.font = "bold 20px -apple-system, sans-serif";
  ctx.fillStyle = "#343b32";
  ctx.fillText("Fieldnotes", 24, 88);
  ctx.font = "14px -apple-system, sans-serif";
  ctx.fillStyle = "#666a60";
  ["今日工作", "项目资料", "设计参考"].forEach((label, index) =>
    ctx.fillText(label, 28, 143 + index * 44),
  );
  roundedRect(ctx, 16, 163, 138, 36, 7, "#d8d7c4");
  ctx.fillStyle = "#424c3d";
  ctx.fillText("项目资料", 28, 187);
  ctx.font = "12px -apple-system, sans-serif";
  ctx.fillStyle = "#6c7066";
  ctx.fillText("团队空间", 28, 328);
  ctx.fillText("春日设计团队", 28, 357);
  ctx.fillStyle = "#6a6f63";
  ctx.font = "12px -apple-system, sans-serif";
  ctx.fillText("项目资料  /  品牌更新", 208, 77);
  ctx.font = "bold 32px -apple-system, sans-serif";
  ctx.fillStyle = "#303a2e";
  ctx.fillText("把想法留在眼前。", 208, 128);
  ctx.font = "15px -apple-system, sans-serif";
  ctx.fillStyle = "#73776b";
  ctx.fillText("Keep your ideas in sight.", 208, 158);
  ctx.fillStyle = "#d7d6c3";
  ctx.fillRect(208, 190, 714, 1);
  ctx.font = "14px -apple-system, sans-serif";
  ctx.fillStyle = "#444d3e";
  ctx.fillText("下一步，让每一张截图都有去处。", 208, 224);
  ctx.font = "13px -apple-system, sans-serif";
  ctx.fillStyle = "#797c71";
  ctx.fillText("将参考、标注和讨论放在同一处，继续你的工作。", 208, 250);
  roundedRect(ctx, 208, 279, 214, 144, 10, "#dddcc9");
  roundedRect(ctx, 438, 279, 214, 144, 10, "#d1d7ca");
  roundedRect(ctx, 668, 279, 254, 144, 10, "#e1d3bd");
  ctx.fillStyle = "#6c765d";
  ctx.font = "13px -apple-system, sans-serif";
  ctx.fillText("视觉方向", 226, 306);
  ctx.fillText("材料与细节", 456, 306);
  ctx.fillText("今天的安排", 686, 306);
  ctx.fillStyle = "#818b6f";
  ctx.fillRect(226, 323, 74, 68);
  ctx.fillStyle = "#b3b596";
  ctx.fillRect(308, 323, 96, 68);
  ctx.fillStyle = "#a4b09b";
  ctx.fillRect(456, 323, 178, 68);
  ctx.fillStyle = "#8a7d64";
  ctx.fillText("14:00   设计评审", 686, 345);
  ctx.fillText("16:30   整理反馈", 686, 380);
  ctx.font = "11px -apple-system, sans-serif";
  ctx.fillStyle = "#777d70";
  ctx.fillText("演示截图 · 示例内容", 208, 457);
}

/** Draws the design's demo screenshot at the given size. */
export function drawDemoScene(
  ctx: CanvasRenderingContext2D,
  width = BASE_WIDTH,
  height = BASE_HEIGHT,
): void {
  ctx.save();
  ctx.scale(width / BASE_WIDTH, height / BASE_HEIGHT);
  drawScene(ctx);
  ctx.restore();
}

let cachedScene: HTMLCanvasElement | null = null;

/** Lazily renders the demo scene to an offscreen canvas. */
export function demoSceneCanvas(): HTMLCanvasElement {
  if (cachedScene) return cachedScene;
  const canvas = document.createElement("canvas");
  canvas.width = BASE_WIDTH;
  canvas.height = BASE_HEIGHT;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2D canvas unavailable");
  drawScene(ctx);
  cachedScene = canvas;
  return canvas;
}

/** Full-size demo screenshot as a data URL (browser-preview capture). */
export function demoShotDataURL(): string {
  return demoSceneCanvas().toDataURL("image/png");
}

/** A 320×160 JPEG thumbnail of a cropped demo scene, for demo history. */
export function demoThumbDataURL(variant = 0): string {
  const canvas = document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 160;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("2D canvas unavailable");
  const source = demoSceneCanvas();
  const cropWidth = 420;
  const cropHeight = 210;
  const sx = Math.min(BASE_WIDTH - cropWidth, variant * 130);
  const sy = variant === 1 ? 120 : variant === 2 ? 200 : 0;
  ctx.drawImage(source, sx, sy, cropWidth, cropHeight, 0, 0, 320, 160);
  const tints = ["#f3a24e", "#78a576", "#70a8c2"];
  ctx.globalAlpha = 0.16;
  ctx.fillStyle = tints[variant % tints.length];
  ctx.fillRect(0, 0, 320, 160);
  ctx.globalAlpha = 1;
  return canvas.toDataURL("image/jpeg", 0.82);
}
