import type { Point, Shape } from "./annotate";

export interface ShapeBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export const DEFAULT_TEXT_FONT =
  '-apple-system, "PingFang SC", "Helvetica Neue", "Segoe UI", sans-serif';

export function annotationTextFont(shape: Shape, fontSize: number): string {
  return `${shape.italic ? "italic " : ""}${shape.bold ? "700" : "400"} ${fontSize}px ${shape.fontFamily ?? DEFAULT_TEXT_FONT}`;
}

export function arrowHead(shape: Shape, scale: number): Point[] {
  const tip = { x: shape.x2, y: shape.y2 };
  const angle = Math.atan2(shape.y2 - shape.y, shape.x2 - shape.x);
  const length = (16 + shape.width) * scale;
  const wing = (offset: number) => ({
    x: tip.x - length * Math.cos(angle + offset),
    y: tip.y - length * Math.sin(angle + offset),
  });
  return [wing(-0.45), tip, wing(0.45)];
}

function endpoints(shape: Shape): Point[] {
  return [{ x: shape.x, y: shape.y }, { x: shape.x2, y: shape.y2 }];
}

function pathBounds(points: Point[]): ShapeBounds {
  let left = Infinity;
  let top = Infinity;
  let right = -Infinity;
  let bottom = -Infinity;
  for (const point of points) {
    left = Math.min(left, point.x);
    top = Math.min(top, point.y);
    right = Math.max(right, point.x);
    bottom = Math.max(bottom, point.y);
  }
  return { x: left, y: top, width: right - left, height: bottom - top };
}

function textBounds(ctx: CanvasRenderingContext2D, shape: Shape, scale: number): ShapeBounds {
  const fontSize = (shape.fontSize ?? 14) * scale;
  const lines = (shape.text ?? "").split("\n");
  ctx.save();
  ctx.font = annotationTextFont(shape, fontSize);
  let width = 0;
  let leftBearing = 0;
  let ascent = fontSize * 0.8;
  let descent = fontSize * 0.2;
  for (const line of lines) {
    const metrics = ctx.measureText(line);
    leftBearing = Math.max(leftBearing, metrics.actualBoundingBoxLeft || 0);
    width = Math.max(width, metrics.width, metrics.actualBoundingBoxRight || 0);
    ascent = Math.max(ascent, metrics.actualBoundingBoxAscent || 0);
    descent = Math.max(descent, metrics.actualBoundingBoxDescent || 0);
  }
  ctx.restore();
  return {
    x: shape.x - leftBearing,
    y: shape.y - ascent,
    width: width + leftBearing,
    height: ascent + descent + (lines.length - 1) * fontSize * 1.3,
  };
}

export function shapeBounds(ctx: CanvasRenderingContext2D, shape: Shape, scale: number): ShapeBounds {
  if (shape.kind === "brush" && shape.points?.length) return pathBounds(shape.points);
  if (shape.kind === "arrow") return pathBounds([...endpoints(shape), ...arrowHead(shape, scale)]);
  if (shape.kind === "tag") {
    const radius = (17 + shape.width) * scale;
    return { x: shape.x - radius, y: shape.y - radius, width: radius * 2, height: radius * 2 };
  }
  if (shape.kind === "text") return textBounds(ctx, shape, scale);
  return pathBounds(endpoints(shape));
}

function nearPath(point: Point, points: Point[], tolerance: number): boolean {
  for (let index = 1; index < points.length; index++) {
    const start = points[index - 1]!;
    const end = points[index]!;
    const dx = end.x - start.x;
    const dy = end.y - start.y;
    const lengthSquared = dx * dx + dy * dy;
    const progress = lengthSquared === 0 ? 0 :
      Math.max(0, Math.min(1, ((point.x - start.x) * dx + (point.y - start.y) * dy) / lengthSquared));
    if (Math.hypot(point.x - start.x - dx * progress, point.y - start.y - dy * progress) <= tolerance) return true;
  }
  return false;
}

export function hitsShape(ctx: CanvasRenderingContext2D, shape: Shape, scale: number, point: Point): boolean {
  const local = { x: point.x - (shape.dx ?? 0), y: point.y - (shape.dy ?? 0) };
  const padding = (5 + shape.width / 2) * scale;
  if (shape.kind === "brush") return nearPath(local, shape.points ?? [], padding);
  if (shape.kind === "line") return nearPath(local, endpoints(shape), padding);
  if (shape.kind === "arrow") {
    return nearPath(local, endpoints(shape), padding) || nearPath(local, arrowHead(shape, scale), padding);
  }
  if (shape.kind === "tag") {
    return Math.hypot(local.x - shape.x, local.y - shape.y) <= (17 + shape.width + 5) * scale;
  }
  const bounds = shapeBounds(ctx, shape, scale);
  const offsetX = local.x - bounds.x;
  const offsetY = local.y - bounds.y;
  if (offsetX < -padding || offsetY < -padding || offsetX > bounds.width + padding || offsetY > bounds.height + padding) return false;
  if (shape.kind === "ellipse") {
    const radiusX = bounds.width / 2;
    const radiusY = bounds.height / 2;
    return Math.hypot((offsetX - radiusX) / (radiusX + padding), (offsetY - radiusY) / (radiusY + padding)) <= 1;
  }
  return true;
}
