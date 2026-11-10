// Toolbar metadata shared by the editor, preferences and tests. The ids are
// part of the design contract (aria-pressed / data-tool attributes are read by
// the CSS and the editor).

export type ToolId =
  | "drag"
  | "rect"
  | "ellipse"
  | "line"
  | "arrow"
  | "brush"
  | "text"
  | "tag"
  | "mosaic"
  | "highlight"
  | "watermark"
  | "pin"
  | "translate"
  | "ocr"
  | "qr"
  | "scroll"
  | "undo"
  | "download"
  | "cancel"
  | "clipboard";

/** The editor toolbar, drawing tools first, actions second. */
export const TOOLS: ReadonlyArray<readonly [ToolId, string]> = [
  ["drag", "拖拽"],
  ["rect", "矩形"],
  ["ellipse", "圆形"],
  ["line", "直线"],
  ["arrow", "箭头"],
  ["brush", "画笔"],
  ["text", "文字"],
  ["tag", "标签"],
  ["mosaic", "马赛克"],
  ["highlight", "高亮"],
  ["watermark", "水印"],
  ["pin", "固定到屏幕"],
  ["translate", "翻译"],
  ["ocr", "提取文字"],
  ["qr", "识别二维码"],
  ["scroll", "滚动截图"],
  ["undo", "撤销"],
  ["download", "下载"],
  ["cancel", "取消"],
  ["clipboard", "保存到剪贴板"],
];

/** Lucide icon names used by `refreshPinstillIcons`. */
export const TOOL_ICONS: Record<string, string> = {
  drag: "move",
  rect: "square",
  ellipse: "circle",
  line: "minus",
  arrow: "arrow-up-right",
  brush: "pen-line",
  text: "type",
  tag: "tag",
  mosaic: "grid-2x2",
  highlight: "highlighter",
  watermark: "stamp",
  pin: "pin",
  translate: "languages",
  ocr: "scan-text",
  qr: "qr-code",
  scroll: "scroll-text",
  undo: "undo-2",
  download: "download",
  cancel: "x",
  clipboard: "copy",
};

/** Ids of tools that draw on the canvas (everything before "pin"). */
export const DRAW_TOOLS: ReadonlyArray<ToolId> = TOOLS.slice(0, 11).map(
  ([id]) => id,
);

/** Tools offered as the default drawing tool in preferences. */
export const DEFAULT_TOOL_CHOICES: ReadonlyArray<ToolId> = [
  "drag",
  "rect",
  "arrow",
  "brush",
  "text",
];

export function isDrawTool(id: string): id is ToolId {
  return (DRAW_TOOLS as ReadonlyArray<string>).includes(id);
}

export function toolName(id: string): string {
  return TOOLS.find(([tool]) => tool === id)?.[1] ?? id;
}
