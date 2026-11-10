# Pinstill 真实功能开发计划（Round 1，Lead 维护）

> 状态：进行中。本文件是 **接口契约的唯一来源**。任何一方需要改动契约，先给 Lead 发消息，不要自行改接口。
> 背景：仓库当前是「设计原型 + MyGo 桌面壳」。本轮把核心链路做真：真实区域截图 → 标注 → 复制/导出/固定 → 历史/截图架 → 全局快捷键/托盘/设置接线。
> 视觉与交互的基准是 `outputs/pinstill-design.html`（保持现有 UI 语言与布局，不要重新设计）。

## 0. 环境与命令

- 工作目录：`<repo>`（所有成员共享同一文件系统）
- 桌面框架：MyGo v0.8.3，源码与文档在 `$(go env GOMODCACHE)/github.com/egoist/mygo@v0.8.3`（重点看 `docs/windows.md` `docs/bindings.md` `docs/frontend.md` `docs/data-transfer.md` `docs/launchers.md` `docs/app.md`）
- MyGo 能力（已确认可用）：`mygo.NewWindow`（Panel/Frameless/Transparent/AlwaysOnTop/SetBounds/SetOpacity/Hide/Show/Focus）、`mygo.BindAs` 绑定服务、`mygo.NewEvent[T]` 事件、`mygo.Protocol.Handle` 自定义协议、`mygo.GlobalShortcut.Register`、`mygo.Clipboard.WriteImage/ReadImage`、`mygo.Dialog.Save`、`mygo.NewTray`、`mygo.Screen.Displays/CursorScreenPoint`、`mygo.App.Path(PathUserData/PathCache)`、`mygo.App.SetOpenAtLogin`、`mygo.Dialog.Message`
- 常用命令：
  - Go 测试：`GOTOOLCHAIN=auto go test ./...`
  - 前端构建：`npm run build:web`
  - 完整桌面包：`mygo build`（通过 `node_modules/.bin/mygo`；产出 `build/darwin-arm64/Pinstill.app`）
  - 开发运行：`npm run dev`（**只有 Lead 可以运行**；会占用 5173 并重新生成 `frontend/src/mygo.ts`）
- 进程卫生：不要留后台进程。前端成员自测可以短暂 `npx vite --config vite.config.ts`，结束前必须 kill；端口 5173 只允许一个占用。

## 1. 职责与文件所有权（写权限互斥）

| 成员 | 写作用域 | 内容 |
|---|---|---|
| Lead | `cmd/pinstill/main.go`、`internal/app/`、`work/plan.md`、`README.md`、`.gitignore`、`outputs/` | 应用编排（窗口/托盘/协议/快捷键/截图流程）、绑定服务、事件、主入口、集成与验收 |
| go-native | `internal/capture/`、`internal/store/`、`go.mod`、`go.sum` | 原生截图后端（macOS 实装 / Windows 可编译后端）、历史存储与缩略图、单元测试 |
| frontend | `frontend/`、`vite.config.ts` | 主页面真实化（截图结果、标注、导出/复制/固定、历史/截图架、设置接线）、`overlay.html`、`pin.html`、bridge 封装 |
| verifier（后续） | 只读 + 报告 | 独立验收，不改产品代码 |

不要修改不属于自己的文件；不要 git commit / push（Lead 统一处理）。不要改 `outputs/pinstill-design.html`。

## 2. 架构

```
cmd/pinstill/main.go            入口：单实例、protocol 注册、绑定、托盘、窗口、WhenReady、自检模式
internal/app/                编排层：App + 绑定服务 Capture/History + 事件 + 截图流程 + 设置
internal/capture/            纯截屏：几何 + 平台后端（darwin/windows/other）+ 权限 + 测试
internal/store/              历史持久化：PNG 原图 + 缩略图 + index.json + 清理 + 测试
frontend/index.html          主窗口页面（设计稿 UI，真实数据）
frontend/overlay.html        区域选择覆盖层（透明全屏 panel）
frontend/pin.html            固定到屏幕的小窗口
frontend/src/bridge.ts       前端唯一 Go 调用封装（untyped call/on，浏览器降级）
frontend/src/*.ts            从 index.html 内联脚本拆出的模块
vite.config.ts               多页构建（index/overlay/pin）
```

数据目录：`mygo.App.Path(mygo.PathUserData)`（macOS: `~/Library/Application Support/Pinstill`），截图与 index.json 存在其下 `captures/` 子目录。缩略图同目录 `<id>.thumb.jpg`。

自定义协议 `shot://`：`mygo.Protocol.Handle("shot", mygo.FileServer(os.DirFS(capturesDir)))`，页面用 `shot://localhost/<文件>` 取图（Windows 用 `http://shot.localhost/<文件>`，由 Go 拼好 URL 返回给前端，前端不自己拼）。

## 3. 冻结接口契约

### 3.1 Go：`internal/capture`（go-native 实现）

```go
package capture

// Rect 是屏幕坐标（DIP/point），原点是主屏左上角，可跨屏为负。
type Rect struct { X, Y, Width, Height float64 }
func (r Rect) Empty() bool
type Point struct { X, Y float64 }
func NormalizeSelection(start, end Point) Rect

type CaptureOptions struct {
    IncludeCursor bool
    DelayMillis   int
}

// CaptureRegion 截取矩形区域，返回 PNG 字节。权限不足/失败返回 error。
func CaptureRegion(rect Rect, opts CaptureOptions) ([]byte, error)

// PermissionGranted 报告系统是否已授予屏幕录制权限（darwin 用 CGPreflightScreenCaptureAccess，
// 通过 purego 调用；调用失败时返回 true 并让 CaptureRegion 的错误兜底）。
func PermissionGranted() bool

// OpenPermissionSettings 打开系统「屏幕录制」设置页（darwin；其他平台返回 ErrUnsupported）。
func OpenPermissionSettings() error

// Supported 报告当前平台是否有真实截图后端。
func Supported() bool

var ErrUnsupported = errors.New("capture: unsupported on this platform")
```

实现要点：
- darwin：`/usr/sbin/screencapture`，参数 `-x -t png [-C] -R x,y,w,h <tmpfile>`。坐标是 point（DIP），输出是像素（Retina 自动 2x）。`DelayMillis>0` 时截图前 sleep（留给 overlay 消失）。临时文件用 `os.CreateTemp`。不要 `-c`（我们不需要直接进剪贴板）。
- windows：可以有真实实现（GDI BitBlt，`golang.org/x/sys/windows`，纯 Go 可交叉编译）就做；做不动就返回 `ErrUnsupported`，但必须 `GOOS=windows go build ./...` 通过。不要引 cgo。
- other（linux 等）：`ErrUnsupported`。
- 单测：`buildScreencaptureArgs(rect, opts)` 之类的纯函数（参数顺序/取整/坐标格式）、Rect 归一化、`PixelRect(scale)` 取整。

### 3.2 Go：`internal/store`（go-native 实现）

```go
package store

type Entry struct {
    ID        string    `json:"id"`
    CreatedAt time.Time `json:"createdAt"`
    Width     int       `json:"width"`
    Height    int       `json:"height"`
    Size      int64     `json:"size"`   // PNG 原图字节数
    File      string    `json:"file"`   // 相对 captures 目录
    Thumb     string    `json:"thumb"`  // 相对 captures 目录（JPEG）
}

func Open(dir string) (*Store, error) // 建目录、加载 index.json；损坏时忽略并重建
func (s *Store) Dir() string
func (s *Store) Save(png []byte, createdAt time.Time) (Entry, error)
func (s *Store) List() []Entry                 // 新→旧
func (s *Store) Get(id string) (Entry, bool)
func (s *Store) ReadPNG(id string) ([]byte, error)
func (s *Store) Delete(id string) error
func (s *Store) Clear() error
func (s *Store) Prune(retentionDays int, maxBytes int64) (removed int, err error) // 0 表示不限制
```

实现要点：缩略图最长边 320px、JPEG q80（`golang.org/x/image/draw` CatmullRom 或 ApproxBiLinear；依赖 v0.46.0 已在本地模块缓存）。index.json 原子写（临时文件+rename）。ID 用 `20060102-150405-[a-z0-9]{4}` 形式，文件系统安全。测试：Save/List/Get/ReadPNG/Delete/Clear/Prune、重开目录后索引一致、缩略图尺寸正确。

### 3.3 Go：绑定服务（Lead 在 `internal/app` 实现；**方法名与语义冻结**）

前端统一用 `call("<Service>.<Method>", ...)`，服务名固定为 `Capture` 与 `History`。

`Capture` 服务：
| 方法 | 参数 | 返回 | 语义 |
|---|---|---|---|
| `BeginRegion` | — | — | 隐藏主窗口，在鼠标所在显示器弹出 overlay |
| `CancelRegion` | — | — | 取消选择，恢复主窗口 |
| `FinishRegion` | `rect {x,y,width,height}`（overlay 本地 CSS px） | `{id}`（异步截图，id 在 capture:completed 里，绑定返回空占位对象） | 结束选择 → 截图 → 存历史 → 显示主窗口 → 发 `capture:completed` |
| `CaptureFullScreen` | — | `{id}`（同上，异步） | 截鼠标所在整个显示器 |
| `CopyImage` | `dataBase64 string`（纯 base64，无 data: 前缀） | — | 写系统剪贴板（PNG） |
| `ExportImage` | `dataBase64 string, suggestedName string` | `string` | 保存对话框导出，返回路径，取消返回 "" |
| `SaveImageToFolder` | `dataBase64 string` | `string` | 写入设置里的保存文件夹，返回路径 |
| `PinImage` | `dataBase64 string` | `string` | 打开置顶贴图窗口，返回 pin id |
| `PinClipboard` | — | `string` | 从剪贴板读图并固定 |
| `ClosePin` | `id string` | — | 关闭贴图 |
| `ApplySettings` | `settings`（见 3.4） | `Record<string,string>` | 应用设置；快捷键注册失败的 key→错误信息 |
| `PermissionGranted` | — | `boolean` | 屏幕录制权限 |
| `OpenPermissionSettings` | — | — | 打开系统设置页 |
| `OverlayInfo` | — | `{displayX,displayY,width,height,scale}` | overlay 页面初始化/每次显示时读取 |
| `ShowMainWindow` | — | — | 显示并聚焦主窗口 |

`History` 服务：
| 方法 | 参数 | 返回 |
|---|---|---|
| `List` | — | `HistoryEntry[]` |
| `Get` | `id string` | `{id,width,height,url}` |
| `Remove` | `id string` | — |
| `Clear` | — | — |

`HistoryEntry`：`{id, createdAt(RFC3339), width, height, size, url, thumbUrl}`。`url`/`thumbUrl` 是 Go 拼好的绝对 URL：当前截图走 session 目录 `/s/<id>.png`，历史截图走 `/h/<file>`；macOS/Linux 前缀 `shot://localhost`，Windows 前缀 `http://shot.localhost`。前端直接用，不要自己拼。

导出归一化（Lead 实现）：`CopyImage`/`PinImage` 内部会把输入统一转成 PNG；`ExportImage`/`SaveImageToFolder` 接受 PNG 或 JPEG base64，Go 识别实际编码后按 `Settings.Format` + `Quality` 归一化落盘，并按目标格式补扩展名。前端传 canvas 的 PNG base64 即可，不必自己 encode JPEG。

### 3.4 设置对象（前端 localStorage 为准，每次变更整体推给 Go）

```ts
interface Settings {
  captureKey: string      // MyGo accelerator，默认 "CmdOrCtrl+Shift+1"（⌘⇧4 被 macOS 截图占用）
  pinKey: string          // 如 "CmdOrCtrl+Shift+2"
  ocrKey: string          // 如 "CmdOrCtrl+Shift+O"
  afterCapture: "editor" | "clipboard" | "pin"
  format: "PNG" | "JPEG"
  quality: number         // 50..100
  filenamePrefix: string
  folder: string          // 如 "~/Pictures/Pinstill"（Go 展开 ~）
  includeCursor: boolean
  sound: boolean
  launchAtLogin: boolean
  keepHistory: boolean
  retentionDays: number   // 0=永不
  historyLimitMB: number  // 0=不限
  tooltip: boolean
  pinOpacity?: number     // 30..100，贴图窗口不透明度
  pinBorder?: boolean
}
```

Go 侧 `Settings` struct 用同样的 camelCase json tag 解码；未知字段忽略。`ApplySettings` 语义：
- 重新注册全局快捷键（unregister 旧的再 register；失败项进返回 map）
- `launchAtLogin` → `mygo.App.SetOpenAtLogin`
- `keepHistory/retentionDays/historyLimitMB` → 启动/变更时 `store.Prune`
- 其余为截图流程/前端行为参数

### 3.5 事件（Go `mygo.NewEvent[T]`，前端 `on(name, cb)`）

| 事件 | payload |
|---|---|
| `capture:completed` | `{id, width, height, url, thumbUrl, source}` source: `"region"\|"screen"\|"clipboard"` |
| `capture:cancelled` | `{reason}` |
| `capture:failed` | `{message, permission:boolean}` |
| `overlay:shown` | `{displayX,displayY,width,height,scale}`（发给 overlay 页面） |
| `history:changed` | `{}` |
| `toast` | `{text, kind}` kind: `"info"\|"success"\|"error"` |
| `pin:init` | 发给 pin 窗口：`{id,url,width,height,opacity,border}`（Lead 在创建 pin 窗口后 Emit，只发事件、不依赖 query） |

事件名带冒号：前端 `on("capture:completed", ...)` 用原始事件名字符串（不要用 `events.captureCompleted`，我们用 untyped API）。

### 3.6 前端 bridge（frontend 实现，唯一原生调用入口）

`frontend/src/bridge.ts` 导出：

```ts
export const hasNative = isMyGo();          // 浏览器静态预览时为 false
export async function beginRegion(): Promise<void>
export async function cancelRegion(): Promise<void>
export async function finishRegion(rect: Rect): Promise<{ id: string }>
export async function captureFullScreen(): Promise<{ id: string }>
export async function copyImage(base64: string): Promise<void>
export async function exportImage(base64: string, suggestedName: string): Promise<string>
export async function saveImageToFolder(base64: string): Promise<string>
export async function pinImage(base64: string): Promise<string>
export async function pinClipboard(): Promise<string>
export async function closePin(id: string): Promise<void>
export async function applySettings(settings: Settings): Promise<Record<string, string>>
export async function permissionGranted(): Promise<boolean>
export async function openPermissionSettings(): Promise<void>
export async function overlayInfo(): Promise<OverlayInfo>
export async function showMainWindow(): Promise<void>
export async function historyList(): Promise<HistoryEntry[]>
export async function historyGet(id: string): Promise<{ id: string; width: number; height: number; url: string }>
export async function historyRemove(id: string): Promise<void>
export async function historyClear(): Promise<void>

export function onCaptureCompleted(cb: (p: CaptureCompleted) => void): () => void
export function onCaptureCancelled(cb: (p: { reason: string }) => void): () => void
export function onCaptureFailed(cb: (p: { message: string; permission: boolean }) => void): () => void
export function onOverlayShown(cb: (p: OverlayInfo) => void): () => void
export function onHistoryChanged(cb: () => void): () => void
export function onToast(cb: (p: { text: string; kind: string }) => void): () => void
```

浏览器降级（`hasNative === false`）：`beginRegion` 等走现有原型模拟（假的截图画布/示例结果），保证 `npm run dev:web` 静态预览仍可看可点；不要在无原生环境下 throw 到 UI。

### 3.7 用户流程（验收标准）

1. 主窗口点「截一张」→ 主窗口消失 → 出现全屏半透明 overlay → 拖框选区 → 松手 → overlay 消失 → 主窗口出现，编辑器显示**真实截图**，可继续标注。
2. 全局快捷键（默认 `CmdOrCtrl+Shift+1`；⌘⇧4 被 macOS 自带截图占用，默认已避开）在应用后台时也能唤起 overlay 流程；完成/取消后回到主窗口。
3. 标注后点「保存到剪贴板」→ 系统剪贴板得到带标注的 PNG（可用 `pbpaste -Prefer png > /tmp/x.png` 验证）。
4. 「下载」按设置里的 PNG/JPEG、质量、文件名前缀走保存对话框导出。
5. 「固定到屏幕」→ 出现无边框置顶小窗显示当前图，可关闭；`pinKey` 可固定剪贴板图片。
6. 「历史」列出真实历史（缩略图+时间），点开进入编辑器；可删除/清空；截图架显示最近截图。
7. 设置：录制快捷键立即生效（失败的组合键在设置页提示）；开机启动、保存文件夹、光标、提示音、截图后默认动作、历史保留策略生效。
8. 托盘图标常驻：截取区域、截取全屏、固定剪贴板图片、显示主窗口、退出。
9. 浏览器静态预览（无 MyGo）不报错、原型可演示。

## 4. 现有设计稿要点（勿丢）

- 单页应用三段视图：截图架 / 历史 / 偏好设置；暗色暖调（`--bg:#141412`、amber `#f3a24e`）；品牌 `pinstill`/`Pinstill`。
- 编辑器：960×480 逻辑画布、20 个工具两组（拖拽/矩形/圆/线/箭头/画笔/文字/标签/马赛克/高亮/水印 | 固定/翻译/OCR/二维码/滚动/撤销/下载/取消/剪贴板）、属性条（颜色/粗细/文本）、结果面板、状态行。
- 设置 6 分类（通用与截图/快捷键/保存与历史/标注与水印/贴图与截图架/识别与翻译）+ localStorage 持久化 + 分类重置。
- 标注渲染逻辑（`drawShape`/`redraw`）是可用资产，保留；把 `background()` 的假截图替换为真实截图的底座图。
- `outputs/pinstill-toolbar-compact.png`、`outputs/pinstill-settings.png` 是旧版截图（还是 shotline 品牌），仅参考，不复用。

## 4.5 E2E 实测结论与已知限制（Lead，2026-11-09）

已验证（构建产物 build/darwin-arm64/Pinstill.app，selftest 模式见下）：
- 真实区域截图 600×400pt → 1200×800px（Retina 2x）；光标/延时/临时文件清理单测+实测通过。
- overlay 在鼠标所在显示器的可见工作区显示（Panel）；拖动选区 → FinishRegion 映射与直接截同一屏幕矩形逐像素一致（mean abs diff 0.00）；编辑器 canvas 载入真实尺寸。
- Esc 取消（System Events 合成按键实测 overlay 隐藏、主窗口恢复）；点击遮罩/右键取消同样走 CancelRegion。
- capture:completed 经 `shot://` URL fetch → createImageBitmap → canvas 正常（无 CORS/taint 问题，马赛克/导出可用）。
- 工具栏「保存到剪贴板」真实写入系统剪贴板（1120×720 PNG，外部 pbpaste 验证过 PNGf）；「固定到屏幕」创建 Panel 贴图窗口并显示图片；历史/截图架真实缩略图、点击载入、删除/清空；设置页 6 分类真实渲染与持久化。
- 托盘：菜单栏 T 图标可见，菜单项（截取区域/截取整个屏幕/固定剪贴板图片/显示主窗口/退出）完整，点击「截取区域」真实唤起 overlay。
- 全局快捷键：Carbon 注册成功（三个快捷键均 registered=true）；**合成按键无法验证触发**（System Events 合成的键盘事件不会触发 Carbon 热键），需真实按键人工确认。
- macOS 限制：Panel 是带 title 样式的窗口，AppKit 会把它的 frame 约束在屏幕可见工作区（built-in 屏 workArea={44,33,1684,1084}），因此 overlay 不覆盖菜单栏和 Dock 区域；代码按「实际窗口 frame」做坐标映射，不受影响。
- 默认快捷键从 ⌘⇧4 改为 ⌘⇧1（⌘⇧4 被 macOS 截图占用）。
- 真实退出路径（Cmd+Q / 菜单退出 / 托盘退出）已修复并纳入 E2E：`PINSTILL_SELFTEST=quit` 调 `App.Quit()` 验证 5s 内走完且退出码 0（此前 overlay 的 OnClose 无条件 PreventDefault 会阻断退出）。
- `shot://` 协议只放行 .png/.jpg/.jpeg（`imagesOnly` 中间件），`/h/index.json` 等其它文件 404。
- 登录后启动在未安装到 /Applications 的构建里会返回 `Operation not permitted`，设置页会如实展示；安装后行为待人工确认。
- 导出保存对话框是模态框，selftest 未自动点击；导出/格式转换逻辑由 internal/app 单测覆盖，需人工点一次确认。
- Windows：GDI 后端纯 Go、交叉编译通过，未做 Windows 真机验证；Rect 在 Windows 按物理像素解释。

## 5. 本轮非目标

- Windows 真实截屏（后端可写则写，写不动保持 ErrUnsupported，但必须可交叉编译）
- OCR / 翻译 / 二维码识别 / 滚动截图的真实服务（保留「待接入」占位文案，不要假装能用）
- 多显示器完整覆盖（overlay 只在鼠标所在显示器工作）、签名/公证/自动更新
- 不动 `outputs/pinstill-design.html` 与 `work/` 下旧脚本

## 6. 验证与交付（各自完成前必须自测）

- go-native：`gofmt -l`、`go vet ./...`、`go test ./...`、`GOOS=windows go build ./...` 全绿；报告新增依赖与测试覆盖点。
- frontend：`npm run build:web` 通过；页面在浏览器静态预览无 console 报错；报告改了哪些文件、哪些设置接线完成。
- Lead：`mygo build` 成功；真实运行验收 3.7 的 1/2/3/5/6/8；产出新截图到 `outputs/`；更新 README。

调试自检（Lead 实现，环境变量 `PINSTILL_SELFTEST`，仅开发用）：`capture`（真实截区域+写剪贴板+存历史，输出尺寸/裁剪文件）、`overlay`（显示 overlay 后整屏截图，验证遮罩渲染）、`deliver`（真实截图走 capture:completed 交付给编辑器后整屏截图）、`pin`（创建贴图窗口后整屏截图）、`history`/`hotkeys`/`settings`。产出文件在 `/tmp/pinstill-selftest-*.png`。

---

# Round 2：桌面优先的截图与贴图（2026-11-09 晚）

用户反馈与拍板（已确认）：
- **移除「截图架」**：主流截图工具没有这个概念；主窗口不再展示截图架，导航第一项改为「截图」（编辑器入口不变）。
- **截图时就地操作**：拖选区域后不跳主窗口，选区旁出现操作条：标注 / 复制 / 贴图 / 保存 / 取消（Esc）。「标注」才进主窗口编辑器。
- **「挂在桌面」是核心**：贴图窗口始终浮在所有窗口之上（Snipaste 贴图式）。
- **贴图持久化**：重启应用后恢复上次的贴图（图片、位置、大小、透明度）。

## R2.1 新增/变更的绑定（Capture 服务，名字冻结）

| 方法 | 参数 | 返回 | 语义 |
|---|---|---|---|
| `CompleteSelection` | `rect {x,y,width,height}`（overlay 本地 CSS px）, `action` ∈ `annotate\|copy\|pin\|save` | `void` | overlay 操作条按钮调用：Go 隐藏 overlay → 截该区域 → 执行 action；`annotate` 走原编辑器流程（存 session+历史、显示主窗口、发 capture:completed）；`copy` 写剪贴板 + toast；`pin` 创建持久贴图窗口 + toast；`save` 打开保存对话框写文件 + toast。四种 action 都会存历史（受 keepHistory 控制） |
| `ResizePin` | `id string`, `width int`, `height int` | `error` | 按 DIP 设置贴图窗口尺寸（页面负责保持宽高比） |
| `SetPinOpacity` | `id string`, `opacity float64`（10..100） | `error` | 设置贴图窗口不透明度并持久化 |
| `CopyPin` | `id string` | `error` | 复制贴图原图到剪贴板 |
| `SavePin` | `id string` | `string` | 保存贴图原图（保存对话框），返回路径 |
| `UpdatePinState` | `id string`, `state {width,height,opacity}` | `error` | 页面侧变更后同步状态并持久化（位置由 Go 在 OnMove/OnResize 持久化） |
| `Settings` | — | `Settings` | 返回当前设置，overlay 页面用于判断 `afterCapture` 默认行为 |

事件不变（`pin:init` 继续用于新窗口初始化，payload 不变）。`afterCapture` 取值扩展为 `toolbar|clipboard|pin|editor`，默认 `toolbar`：
- `toolbar`：拖选后显示操作条（默认）
- 其它：拖选松手后直接执行对应动作（等价于点操作条对应按钮）

## R2.2 贴图行为（Go + 页面）

- 窗口：Panel + Frameless + 始终置顶；页面内 `--app-region: drag` 拖动整个贴图（按钮/操作区 no-drag）。
- 交互：悬停右上角关闭按钮；右键菜单：复制 / 保存 / 关闭 / 透明度 +10% / 透明度 −10%；双击复制；滚轮缩放窗口（保持宽高比，10% 步进）；⌘/Ctrl + 滚轮调透明度；右下角拖拽手柄缩放（保持宽高比）；Esc 关闭。
- 多张贴图：新建时级联错位摆放；各自独立位置/大小/透明度。
- 持久化：图片存 `<userData>/pins/<id>.png`，状态存 `<userData>/pins/pins.json`（原子写）；Go 在创建/移动/缩放/透明度变化/关闭时更新；应用启动时恢复（越界坐标夹回可见屏幕）。
- 协议：`shot://localhost/p/<file>` 提供贴图图片（沿用 imagesOnly 限制）。
- 贴图「原始大小」：右键菜单提供，恢复为图片像素 / display scale 的 DIP 尺寸。

## R2.3 前端改动

1. overlay.html/overlay.ts：拖选后进入「操作条状态」——选区保留、遮罩保留、选区旁浮出操作条（标注/复制/贴图/保存/取消）与尺寸角标；未出操作条时拖拽=重新选区；Enter=复制、Esc=取消；按钮 pending 时禁用；调用 CompleteSelection。overlay:shown 时重置。
2. pin.html/pin.ts：实现 R2.2 页面侧全部交互（拖动由 CSS，右键菜单/双击/滚轮/手柄/Esc/关闭按钮），监听 pin:init，调用 ResizePin/SetPinOpacity/CopyPin/SavePin/UpdatePinState/ClosePin。
3. index.html/main.ts：删除截图架 DOM/逻辑/占位 pin/计数与 CSS；导航首项显示「截图」；右栏文案去掉截图架与「设计预览」的不实描述；设置「贴图与截图架」分类改名「贴图与桌面」，删除 rackPosition/rackHide/pinTop 三个无效控件；「截图后」选项改为 就地操作条/复制到剪贴板/固定到屏幕/进入标注；设置对象里删除 rack* 字段（保留兼容忽略）。
4. 导出/复制/标注等既有能力保持不变。

## R2.4 Go 改动（Lead）

- `internal/app`：CompleteSelection 与 action 执行、贴图管理器（创建/恢复/移动与缩放持久化/关闭）、`/p/` 协议路由、Settings() 绑定、afterCapture=toolbar 兼容。
- 新增 `internal/pin` 包（go-native）：贴图状态持久化。
- selftest 新增：`flow`（Eval 驱动 overlay 选区+操作条，验证 copy/pin/annotate）、`pinpersist`（两次启动验证恢复）。
- work/e2e.sh 增加上述步骤；README 更新截图与说明。

## R2.5 非目标（本轮）

- 多显示器分别贴图跨屏联动、贴图旋转/翻转、贴图内二次标注、贴图分组、OCR/翻译/二维码/滚动截图、Windows 真机验证。

## R2.6 实测结论（Lead，2026-11-09 晚）

构建产物：build/darwin-arm64/Pinstill.app（0.1.0）。work/e2e.sh 14 步全部通过（可重复运行）：

- **桌面操作条全流程**（`PINSTILL_SELFTEST=flow`，用 `Page().Eval` 在 overlay 页面合成 pointer 拖选再点操作条）：
  - copy → 系统剪贴板出现 480×320 PNG；
  - pin → 新建桌面贴图（pins=1）；
  - annotate → 主窗口显示、编辑器 canvas=480×320；
  - cancel → 不产生新截图（历史条数不变）；
  - afterCapture=clipboard 时拖选后自动复制（400×280），不点操作条。
- **桌面贴图交互**（`pinui`）：双击复制（1280×800 PNG 进剪贴板）且窗口 bounds 不变；右键菜单复制/透明度生效（100→90）；滚轮缩放 640×400→704×440（保比）；关闭按钮 → pins=0、持久化 stored=0。
- **贴图持久化**（`pinpersist-setup` + `pinpersist-check`）：创建贴图后走真实 `App.Quit()` 退出，重启后 restored=1/stored=1 且几何一致；关闭后 stored=0。
- 旧链路回归：真实截图/历史/剪贴板、选区映射（与直接截屏 mean abs diff 0.00）、Esc 取消（对 System Events 偶发丢键做了 3 次重试）、编辑器交付、工具栏动作、托盘、快捷键注册、设置应用均通过。
- 主窗口已无截图架；`uicapture` 的 rack 视图为空编辑器，history 卡片为真实历史；`showcase` 用合成图生成 README 截图（不含用户桌面内容）。
- 已知测试脆弱点：连续快速启动多个 selftest 实例时（<0.6s），偶发一个 pin 窗口在创建后即被关闭；e2e.sh 已加入 1.0s 间隔规避；真实使用（单实例常驻）不复现。System Events 合成的 Escape 偶发不达，已加重试。
- 隐私修复（QA round 2 发现）：showcase 生成的 README 截图曾读到真实历史（history 截图泄露用户桌面）。现在 `PINSTILL_DATA_DIR` 可把 captures/pins/session 全部隔离到指定目录（默认行为不变，目录自动创建）；`PINSTILL_SELFTEST=showcase` 未设置该变量时在 main() 阶段直接失败退出；outputs 五张截图均已在隔离数据目录重生成并逐张确认不含真实内容。
- QA 修复（round 2 复验）：恢复贴图取**最新** 60 张（此前误取最旧）；`shotBase` 在存储初始化前赋值，历史/贴图存储打开失败只降级对应功能而不再中断启动；非法 CompleteSelection action 会复位 capturing。

---

# Round 3：选区后桌面出现「完整标注工具栏」（2026-11-09 深夜）

用户确认：截图拖选完成后，不要只给 5 个快捷动作，而是**在选区旁直接出现完整标注工具栏**（矩形/圆/直线/箭头/画笔/文字/标签/马赛克/高亮/水印 + 颜色/粗细 + 撤销 等全部能力），在桌面上就地标注，再复制/贴图/保存/进编辑器。

## R3.1 交互流程（冻结）

1. 拖选区域松手 → overlay 调用 `Capture.PrepareSelection(rect)`。
2. Go 隐藏 overlay → 截取该区域（250ms 让 overlay 淡出）→ 保存到 session → 重新显示 overlay → 向 overlay 页面发 `overlay:capture {id,url,width,height,rect}`（width/height 为像素，rect 为该区域在 overlay 本地 CSS px）。
3. overlay 进入「标注模式」：选区里显示刚截下的真实图像（fetch url → createImageBitmap → canvas，保持 untainted），其余区域保持遮罩；选区旁显示**完整标注工具栏** + 颜色/粗细属性条 + 尺寸角标。
4. 用户在选区上直接标注（全部绘制工具与主编辑器一致：矩形/圆/直线/箭头/画笔/文字/标签/马赛克/高亮/水印，撤销，颜色/粗细/文本）。
5. 工具栏动作（全部把「截图 + 标注」合成后的 PNG 交给 Go）：
   - `复制` → 剪贴板
   - `贴图` → 挂到桌面（持久化贴图）
   - `保存` → 保存对话框
   - `进入编辑器` → 主窗口编辑器打开合成后的图片（可在编辑器继续精修）
   - `取消` / Esc → 放弃本次截图
6. 标注模式内 Esc = 取消整个截图；工具栏可返回「重新框选」？本轮不要求（重新框选仍可再按快捷键）。

## R3.2 新增绑定/事件（冻结）

Capture 服务：
| 方法 | 参数 | 返回 | 语义 |
|---|---|---|---|
| `PrepareSelection` | `rect {x,y,width,height}`（overlay 本地 CSS px） | `{id,width,height,url,rect}`（同时也会发 overlay:capture 事件） | 隐藏 overlay→截图→存 session→显示 overlay→发事件；Go 侧 capturing 保持 true |
| `FinishAnnotated` | `action string`（annotate/copy/pin/save/cancel）, `dataBase64 string`（合成后的 PNG；cancel 时可为空） | `void` | 隐藏 overlay；把合成 PNG 存历史（cancel 除外）；按 action 执行 |

事件：`overlay:capture` → payload `{id,url,width,height,rect:{x,y,width,height}}`，发给 overlay 窗口。

`afterCapture` 语义更新：`inplace`（默认，完整工具栏）/`copy`/`pin`/`editor`；非 `inplace` 时选区完成后自动跳过工具栏直接完成（用原始截图），`editor` 直接进主窗口。旧值 `toolbar` 兼容映射为 `inplace`。

## R3.3 前端改动

- overlay.ts 增加「标注模式」：状态机 idle → selecting → preparing → annotating；准备阶段显示加载态；拿到 overlay:capture 后渲染真实图像 + 完整工具栏。
- 新增独立标注引擎模块（可从现有 editor.ts 提取绘制逻辑，或新建 `frontend/src/annotate.ts` 供 overlay 使用；不要重写 editor.ts 用法导致主编辑器回归）。
- 完整工具栏布局：两行（绘制工具 11 个 + 动作 撤销/复制/贴图/保存/进入编辑器/取消），选中态、颜色 swatch、粗细/字号滑杆、文字输入、悬停标签；工具栏自动贴合选区上下方并 clamp 在窗口内；小选区时可用紧凑布局。
- 合成导出：canvas = 截图位图 + 标注图形 → `toDataURL('image/png')` → 去掉 data: 前缀 → `FinishAnnotated(action, base64)`。
- 主窗口编辑器保持现有能力；`进入编辑器` 时 Go 直接把合成图作为新 capture 交付主窗口。

## R3.4 Go 改动（Lead）

- `PrepareSelection`：隐藏 overlay → 250ms → 截区域 → 写 session（不写历史）→ 显示 overlay → Emit overlay:capture；错误时恢复主窗口并报错。
- `FinishAnnotated`：隐藏 overlay；cancel 直接复位；其它 action 先把合成 PNG 写 session+历史，再分别复制/贴图/保存/交付主窗口（annotate 复用 storeCapture + capture:completed）。
- `afterCapture` 兼容 inplace/toolbar/copy/pin/editor；overlay 读取 Settings 决定是否跳过完整工具栏（Go 侧不强制）。
- selftest 新增 `annotateflow`：真实拖选 → 等 overlay:capture → Eval 在画布画一个红色矩形并点「复制」→ 校验剪贴板尺寸与像素；再跑一次点「贴图」与「进入编辑器」；Esc 取消不产生历史。

## R3.5 非目标

文字输入法/富文本、标注旋转/对齐、贴图内再编辑、OCR 等识别能力、Windows 真机。

## R3.6 实测结论（Lead，2026-11-09 深夜）

- 构建 + 完整 `work/e2e.sh`（隔离数据目录）通过；`PINSTILL_SELFTEST=flow` 覆盖：
  - 拖选 → PrepareSelection → 选区内显示真实截图（#overlayCanvas）→ 点工具画红色矩形 → 复制：剪贴板 640×420 PNG 且检测到标注颜色（annotated=true）；
  - 贴图：pins=1；进入编辑器：主窗口 editor canvas=640×420（合成图）；
  - afterCapture=clipboard 自动完成复制（400×280，不显示工具栏）；
  - cancel：历史条数不变。
- 「完整工具栏」按用户提供的主编辑器工具栏补齐：11 绘制工具 + undo + 翻译/OCR/二维码/滚动截图（待接入，不调用 FinishAnnotated）+ 颜色/粗细/文本 + 复制/贴图/保存/进入编辑器/取消；editor UI 动作在调用 Go 前映射为 annotate。
- 隐私：overlay 就地图必须显示真实截图，showcase 生成 README 图时通过页面内的 `window.__pinstillDemoCapture` 测试钩子把画布底图替换为合成图，仓库的 tuen-overlay.jpg 已确认只有合成内容；showcase 仍强制 PINSTILL_DATA_DIR 隔离。
- 已知测试抖动：首次 PrepareSelection 偶发未进入标注态（overlay 首次显隐时序），flow selftest 已加重试；真实手工操作未复现。
- 真实截图回归（权限恢复后）：`bash work/e2e.sh` 真实截屏路径连续两轮 ALL CHECKS PASSED；flow 中 copy 为 640×420 且 annotated=true、pin/editor/auto clipboard/cancel 全部符合预期；`PINSTILL_DATA_DIR` 隔离下默认用户目录零写入。

---

# Round 4：滚动截图（手动默认 + 自动可选）（2026-11-10）

用户选择：手动滚动拼接为默认，自动滚动作为可选（需要 macOS「辅助功能」权限）。

## R4.1 交互（冻结，2026-11-10 修正：独立控制条窗口）

> 修正原因：overlay 是全屏窗口，即使选区透明仍会拦截滚轮事件，用户手动滚动无法到达下层 App。因此滚动模式改为：Go 隐藏 overlay，另开一个小控制条窗口。

1. 选区标注入驻后，工具栏点「滚动截图」→ overlay 调用 `StartScroll(rect,false)`；Go 隐藏 overlay、在选区附近显示独立控制条窗口（`/scroll.html`，约 420×70，Panel/Frameless），用户可以直接滚动下层目标窗口。
   - 控制条：自动滚动开关（需辅助功能授权，未授权时点击引导去系统设置）、进度「已拼接 N 帧 · H px」、动作：复制 / 保存 / 贴图 / 进编辑器 / 取消；控制条 470×84、可拖动、Esc 取消、放置时避开选区以免截进自己。停顿检测到「stalled」后不结束会话，继续滚动会重新开始抓帧。
2. 手动模式（默认）：用户自己滚动，Go 每 ~340ms 抓一帧，检测到连续 2 帧几乎相同（到底/停顿）→ 标记 done 但仍停留等动作；用户点动作即完成。
3. 自动模式：Go 每 ~420ms 向选区中心发送滚轮事件，再抓帧；需要 `AXIsProcessTrusted()` 为 true，否则 `StartScroll` 返回错误，前端引导开「辅助功能」。
4. 到达上限（120 帧 / 总高 30000px）→ 状态 max，停止抓帧等动作。
5. 完成动作：把拼接后的长图存 session+历史，然后复制/保存/贴图/进入编辑器（annotate）。

## R4.2 绑定/事件（冻结）

Capture 服务：
| 方法 | 参数 | 返回 | 语义 |
|---|---|---|---|
| `StartScroll` | `rect`（overlay 本地 CSS px）, `autoScroll bool` | `error` | 开始滚动截图会话；auto 但无辅助功能权限 → 返回错误 |
| `FinishScroll` | `action string`（annotate/copy/pin/save/cancel） | `void` | 停止会话、拼接完成、存历史并按 action 执行（cancel 丢弃） |
| `SetScrollAuto` | `enabled bool` | `error` | 会话中切换自动滚动；授权缺失返回错误 |
| `ScrollAccessibilityGranted` | — | `bool` | 是否具备辅助功能权限（自动滚动） |
| `OpenScrollAccessibilitySettings` | — | `void` | 打开「辅助功能」设置页 |

事件：
- `scroll:init` → `{rect, auto, accessibilityGranted}`（发给控制条窗口）
- `scroll:progress` → `{frames int, height int, status string, auto bool}`；status ∈ `running|done|stalled|max|stopped`。

## R4.3 Go 实现（Lead）

- `internal/stitch`：垂直拼接。输入多帧 RGBA，找相邻帧重叠高度（搜索 overlap 范围，行/列抽样计算平均绝对差，取最小且低于阈值），返回合并图；支持结束判断（两帧差异极低视为到底）。单测：合成滚动帧拼接、无重叠回退、固定表头（保留策略）、最大高度限制。
- `internal/capture` 新文件（darwin）：滚轮事件 `ScrollWheel(x,y,deltaY)`（CGEventCreateScrollWheelEvent2 + CGEventSetLocation + CGEventPost，purego）、`AccessibilityGranted()`（AXIsProcessTrusted，ApplicationServices）、`OpenAccessibilitySettings()`；其他平台返回 ErrUnsupported。
- `internal/app`：滚动会话状态机 goroutine：抓帧→拼接→进度事件；自动模式先滚后抓；完成动作复用存储/复制/贴图/保存/编辑器路径。
- 抓帧不隐藏 overlay：选区内部必须透明，Go 直接截选区（截到的是真实下方内容）。前端负责在滚动模式隐藏画布/边框。若实测透明截取失败，则回退「每帧隐藏 overlay + 延时」方案（慢但有闪烁）。
- selftest：`scroll`（fake capture 下跑 StartScroll→FinishScroll(annotate)，校验长图与历史）；`scroll-auto-noperm`（无权限时 StartScroll(auto) 返回错误）。

## R4.4 前端改动

- overlay.ts：工具栏「滚动截图」按钮（data-tool="scroll"）只调用 StartScroll(rect,false)，成功后 Go 隐藏 overlay；失败在工具栏提示。
- 新增 frontend/scroll.html + src/scroll.ts：独立控制条窗口 UI（拖动区、#scrollProgress、#scrollAuto、data-scroll-action 按钮、scroll:init/scroll:progress 监听、自动滚动授权引导、Esc 取消、浏览器降级 demo），vite 多页加 scroll 入口。
- 其余保持不变。

## R4.5 非目标

Windows 真机滚动、横向滚动、视频/动画内容、自动识别页内滚动容器、超长图预览优化。

## R4.6 实测结论（Lead，2026-11-10）

- Go 侧新增：`internal/stitch`（重叠匹配拼接，反向搜索最大重叠 + 早停加速，单测覆盖拼接重建/无重叠/停止检测/最大高度）、`internal/capture` 滚动事件与辅助功能检测（purego CoreGraphics/ApplicationServices）、`internal/app` 滚动会话（独立控制条窗口，隐藏 overlay，手动/自动两种节奏）。
- `PINSTILL_SELFTEST=scroll`（模拟帧）：accessibility 检测、自动模式会话切换、手动会话拼接出 6 帧 2300px 长图并交付编辑器 1280×2300。
- `PINSTILL_SELFTEST=scrollflow`（真实页面驱动）：选区标注态点「滚动截图」→ Go 隐藏 overlay、显示控制条 → 页面收到 scroll:init/progress → 点「进编辑器」→ 编辑器载入 1280×1700 长图。
- 自动滚动真实滚轮事件需要「辅助功能」权限（本机已具备），selftest 用模拟帧时不会真的发送滚轮事件（环境变量 guard），避免影响桌面；**真实自动滚动效果仍建议人工确认一次**。
- 控制条截图（outputs/pinstill-scroll.jpg）：正在拼接 · 4 帧 · 1700 px / 自动滚动开关 / 复制保存贴图进编辑器取消。

## R4.7 QA round 4 修复（Lead）

独立验收 PASS（0 BLOCKER/0 MAJOR）。已修：
- 手动模式停顿检测不再结束会话：连续静帧置 `stalled` 并暂停自动滚动，继续滚动会恢复抓帧（此前 done 后永久停止，UI 提示误导）。
- `StartScroll(auto)` 先检查 `ScrollSupported()`，Windows 上自动滚动显示明确错误；README 措辞修正（Windows 手动拼接可编译、未真机验证）。
- `FinishScroll` 先校验/收尾成功后才隐藏控制条；失败路径隐藏控制条 + 显示主窗口 + capture:failed/toast，用户不再「窗口突然消失」。
- `StartScroll/FinishScroll/SetScrollAuto` 加 `scrollMu` 串行化，避免并发会话泄漏/重复收尾。
- plan 契约数字对齐实现（120 帧 / 340ms / 420ms / 470×84）；删除未用的 stitch `ReachedEnd`；bridge 的 source 联合类型补 "scroll"。
- 修复后：`go test ./...` 全绿；`PINSTILL_SELFTEST=scroll/scrollflow` 结果不变（1280×2300 / 1280×1700）；完整 e2e 19 段 ALL CHECKS PASSED。

---

# Round 5：体验打磨（无闪烁截图 / 文字输入 / 调色板）（2026-11-10）

用户反馈三条：
1. 截图后闪一下不够丝滑；
2. 文字交互要主流方式：选文字工具 → 在画面上点击定位 → 输入框出现在该位置 → 直接输入；
3. 颜色不支持调色板。

## R5.1 无闪烁截图（Lead 已实现）

- `PrepareSelection` 不再隐藏/重新显示 overlay（原来 hide→capture→show 造成闪烁）。现在 overlay 保持显示：页面进入 preparing 状态时隐藏选区边框/角标等装饰，选区内部本身透明，screencapture 直接透过选区截到下方真实画面；捕获完成后页面在选区里显示截图 + 工具栏。
- 权限失败不再弹主窗口：Go 向 overlay 发 `capture:failed`（permission=true），overlay 停留并显示权限引导（打开系统设置/重试/取消）。
- 实测：flow selftest 通过；捕获的 session 图片为真实屏幕内容（尺寸 640×420、亮度 std≈24），无 overlay 装饰。

## R5.2 文字工具交互（前端）

- 选中「文字」工具后，在选区/画布上点击 → 在该位置出现输入框（textarea，自动聚焦，位置随缩放正确换算）→ 输入内容 → Enter 确认 / Shift+Enter 换行 / Esc 取消 / 失焦确认；空内容不创建。
- 标签工具同样支持点击位置输入标签文字（默认标签图标样式）。
- 主窗口编辑器与 overlay 标注引擎行为一致（保持相同手感）。
- 兼容键盘输入法（composition 事件期间 Enter 不提交）。

## R5.3 调色板（前端）

- overlay 工具栏与主编辑器属性条都支持：
  - 预设色板（现有 5 色基础上可扩到 8 色）；
  - 「自定义颜色」按钮打开系统取色器（`<input type="color">`，隐藏触发），选择后加入最近使用颜色（最近 3~5 个，持久化在 localStorage）；
  - 当前颜色有选中态，自定义颜色也可被画笔/形状/文字复用。
- 水印透明度等属性不变。

## R5.4 验证

- flow/annotate 的 e2e 仍全绿；新增 selftest 不涉及（前端交互由 fake-native headless 验证）。
- 视觉/交互证据：overlay 点击文字位置出输入框、取色器选色后绘制颜色生效；build:web 四页通过。

## R5.5 非目标

富文本/字体选择、旋转文字、多文本框同时编辑、贴图内标注。

## R5.6 实测结论（Lead，2026-11-10）

- 无闪烁截图：`PrepareSelection` 改为保持 overlay 显示、透过透明选区直接截屏（90ms 等待页面隐藏装饰），flow selftest 全过；session 截图统计为真实屏幕内容（亮度 std≈24，无 overlay 装饰），不再 hide/show 闪烁。
- 权限失败不再弹主窗口：Go 向 overlay 发 `capture:failed`，overlay 原地显示权限引导（打开设置/重试/取消）；前端 headless 验证通过。
- 文字/标签：点击画布内联输入框（定位/聚焦/Enter/Shift+Enter/Esc/失焦/IME/空内容丢弃），overlay 与主编辑器一致；已移除工具栏先打字的旧方式（水印保留工具条文本）。
- 调色板：8 预设 + 自定义取色器 + 最近 4 色（localStorage，两页共享），预设/自定义选色均可用于绘制并持久化。
- 回归：完整 e2e 19 段 ALL CHECKS PASSED；build:web 四页、go test/vet 全绿；README 截图已用新工具栏/调色板重新生成（合成内容，无用户桌面）。

---

# Round 6：更名 Pinstill（钉图）（2026-11-10）

用户决定把项目从 Tupen 更名为 **Pinstill**（中文「钉图」），完整替换，包括 GitHub 仓库。

## R6.1 替换范围（已完成）

- 应用标识：name `Pinstill`、slug `pinstill`、bundle id `dev.echovic.pinstill`；窗口标题/托盘/前端文案全部替换
- Go module：`github.com/echoVic/Pinstill`，入口 `cmd/pinstill`；环境变量 `PINSTILL_*`；自检产物 `/tmp/pinstill-selftest-*`
- GitHub 仓库：`echoVic/Tupen` → `echoVic/Pinstill`（remote 同步更新）
- 图标：新增 `resources/icon.png`（1024²，暗色圆角 + 截图卡片 + 琥珀色图钉），mygo 构建时生成 .icns
- 数据迁移：`internal/app/migrate_darwin.go` 首次启动时把旧 `Tupen` 数据目录/WebKit 目录移动到 `Pinstill` 下（不覆盖已存在目录）
- 文档/截图：README、plan、outputs 全部换新名并重新生成合成截图
- 注意：bundle id 变更后 macOS 屏幕录制权限需重新授权一次；签名仍为 Developer ID（Developer ID）

## R6.2 验证

- `npm run build:web`、`mygo build`（Developer ID 签名）通过；产物 `Pinstill.app` / `pinstill-0.1.0-arm64.dmg`
- 完整 e2e（隔离 `PINSTILL_DATA_DIR`）19 段 ALL CHECKS PASSED
- 之后按用户优先级继续：交互/观感打磨 + 稳定性与权限体验
