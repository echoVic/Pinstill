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

## R6.3 视觉标识收尾（2026-11-10 续）

R6 的完整替换漏了两处旧名首字母 **T** 的图形残留（文字已全换，但字形还是 Tupen 的 T）：

- 主窗口左上角品牌标记：`frontend/index.html` 的 `.brand-mark` `T` → `P`；设计稿快照 `outputs/pinstill-design.html` 同步改正
- 菜单栏托盘图标：`internal/app/media.go` 原来是「T + 选区角标」点阵图，改为 8× 超采样 + box 降采样的圆体 `P`（菜单栏 16–22pt 下边缘平滑、不再出现像素台阶）；`internal/app/media_test.go` 增加形状探针（茎顶/茎底/碗右/内孔/字形外），防止后续退化成空白或旧字形
- README 截图重新生成（editor/history/settings 里能看到品牌标记），仍为隔离数据目录下的合成内容
- 实测：菜单栏状态项渲染为圆体 **P**（模板图标随菜单栏外观着色；截屏证据 `/tmp/pinstill-menubar-sec.png`，状态项出现在副屏菜单栏 x≈2289pt）
- 验证：`gofmt`/`go vet`/`go test ./...` 全绿；`npm run build:web` + `mygo build`（Developer ID 签名，Developer ID）；完整 `work/e2e.sh --skip-build`（隔离 `PINSTILL_DATA_DIR`）ALL CHECKS PASSED

---

# Round 7：权限体验、稳定性与手感打磨（2026-11-10 晚）

背景：Round 6 更名完成、用户开始试用；本轮**不加新功能**，把「发版前」的权限体验、稳定性与手感补齐。用户试用反馈由 Lead 随时并入，契约以本节为准。

## R7.0 本轮文件所有权（写作用域互斥）

| 成员 | 写作用域 |
|---|---|
| Lead | `internal/app/`、`cmd/`、`work/`、`README.md`、`outputs/` |
| go-native | `internal/capture/`、`internal/store/`、`internal/pin/`、`internal/stitch/` |
| frontend | `frontend/` |
| verifier | 只读 |

不要改 `outputs/pinstill-design.html`；不要 git commit / push（Lead 统一处理）。
**进程约定**：用户正在试用当前构建，任何成员都**不要启动或 kill `Pinstill.app` 进程**；需要 app 级验证的步骤由 Lead 统一在集成阶段做。前端自测只用 `npm run build:web` + 浏览器 headless；Go 侧用单元测试。

## R7.1 权限中心 + 主窗口权限横幅

设置新增第 7 个分类：`id: "permissions"`、名称「权限」、排在「识别与翻译」之后。`#settingsNav` 的**每个**分类按钮都加 `data-settings-cat="<category id>"`（现有六个也补上，e2e 用）。

分类内容三行（实时状态 + 动作按钮）：

1. **屏幕录制**（截图必需）：状态为 `已授权` / `未授权` / `浏览器预览 · 无需权限`。
   动作：`请求权限`（`Capture.RequestScreenPermission`）、`打开系统设置`（`Capture.OpenPermissionSettings`）、`重新检测`（`Capture.PermissionGranted`）
2. **辅助功能**（自动滚动需要）：状态 `已授权` / `未授权` / `浏览器预览 · 无需权限`。
   动作：`打开系统设置`（`Capture.OpenScrollAccessibilitySettings`）、`重新检测`（`Capture.ScrollAccessibilityGranted`）
3. **登录时启动**：显示系统真实状态（新绑定 `Capture.LaunchAtLogin`）与偏好开关当前值；未安装/未生效时给出说明。
   动作：`打开登录项设置`（新绑定 `Capture.OpenLoginItemsSettings`，darwin 打开「系统设置 → 通用 → 登录项」，其他平台返回 `ErrUnsupported`）

冻结 DOM hooks（e2e/headless 断言用，id/属性不可改）：
- 分类按钮：`#settingsNav button[data-settings-cat="permissions"]`
- 状态文本：`#permScreenStatus`、`#permAccessibilityStatus`、`#permLaunchStatus`（文本必须包含 `已授权`、`未授权`、`浏览器预览` 三者之一）
- 按钮：`[data-perm-action="screen-request|screen-open|screen-check|accessibility-open|accessibility-check|login-open"]`

主窗口权限横幅：`#permissionBanner`（放在 `main.main` 内、`#workspaceView` 之前，默认 `hidden`）：
- 屏幕录制未授权时显示：文案「Pinstill 还差「屏幕录制」权限才能截图」+ 按钮 `[data-banner-action="grant"]`（请求权限）、`[data-banner-action="settings"]`（打开系统设置）、`[data-banner-action="dismiss"]`（本次会话隐藏）
- 已授权或浏览器预览时隐藏；`window focus` 与截图失败事件后刷新状态

新增 Go 绑定（Capture 服务，方法名冻结）：

| 方法 | 返回 | 语义 |
|---|---|---|
| `LaunchAtLogin` | `bool` | 系统登录项当前是否生效（`mygo.App.OpenAtLogin()`），只读 |
| `OpenLoginItemsSettings` | `error` | 打开「登录项」设置页（darwin）；其他平台返回「登录项设置不支持」错误（与 `capture.ErrUnsupported` 语义一致，内部实现为 `errLoginItemsUnsupported`） |

顺带修复历史缺口：前端从 R5 起就在调用 `Capture.RequestScreenPermission`，但 Go 侧从未实现该绑定（「请求权限」一直静默失败并回退到打开系统设置）。本轮补上同名绑定：`func (s *CaptureService) RequestScreenPermission() bool { return capture.RequestPermission() }`（darwin 为 CGRequestScreenCaptureAccess，会弹系统授权框；其他平台返回 true）。

## R7.2 稳定性加固（go-native）

只做审计 + 补测试/小修，不改接口：
- store：index.json 损坏 → 从 PNG 重建（现行为）补测试；目录不可写/缺失的降级路径；保留策略边界（0 天 / 超大 MB）
- pin：pins.json 损坏容忍（图片保留、索引重建为空）补测试；越界几何 clamp；最多恢复 60 张
- capture：坐标取整/负坐标/零尺寸边界补测试；`GOOS=windows go build ./...` 保持通过
- `gofmt -l` 空、`go vet ./...` 空、`go test ./...` 与 `go test -race ./internal/...` 全绿
- 发现真 bug 允许在作用域内修复并在报告写清楚（不要顺手重构）

## R7.3 手感与观感打磨（frontend）

1. 过渡动效：overlay 工具栏/属性条、滚动控制条、结果面板、toast 统一 120–180ms 缓动；尊重 `prefers-reduced-motion: reduce`
2. 按钮状态统一：pending（禁用 + 视觉反馈）、成功（短暂 ✓ 反馈）、禁用样式；复制/保存/贴图动作有明确成功反馈
3. 编辑器显示：状态行显示「适应窗口 · 实际 W×H px」；超大图不溢出容器；不改绘制引擎
4. 键盘可达性：工具栏/设置按钮 `:focus-visible` 环、`aria-pressed` 正确、Esc 关闭菜单/输入框不冲突
5. 历史卡片：hover 出现删除按钮（含确认）、空状态文案、缩略图破图兜底
6. 权限中心 UI 与既有暗色暖调一致（与 R7.1 同一成员完成）
约束：不改 Go 接口；不新增功能；四页 build:web 通过。

## R7.4 验收（各自完成前自测；Lead 集成）

- frontend：`npm run build:web` 四页 + 浏览器 headless（无 Go 桥接降级）无 console 错误；对 `data-settings-cat` / `#perm*` / `#permissionBanner` 做 DOM 断言；报告改动文件
- go-native：`gofmt`/`go vet`/`go test`/`go test -race`/Windows cross 全绿 + 新增测试清单
- Lead：新绑定 + selftest `PINSTILL_SELFTEST=perms`（进入「设置 → 权限」，断言三状态与按钮存在，打印 `SELFTEST perms: screen=... accessibility=... login=...`）；`work/e2e.sh` 增加该步骤；`mygo build` 签名；完整 e2e；README/plan；截图再生
- verifier：独立验收（只读、报告 BLOCKER/MAJOR/MINOR）

## R7.5 非目标

公证/发版、OCR/翻译/二维码、Windows 真机、多显示器覆盖、编辑器缩放平移交互（只在状态行展示「适应窗口」，不做 zoom 控件）。

## R7.6 实测结论（Lead，2026-11-10 晚）

- **权限中心（R7.1）**：设置第 7 分类「权限」上线（屏幕录制 / 辅助功能 / 登录时启动 三行实时状态 + 六个动作按钮）；主窗口未授权横幅（grant / settings / dismiss，dismiss 为本次会话内存态）。新增绑定：`Capture.RequestScreenPermission`（**补的是真 bug**：前端自 R5 起就在调、Go 侧从未实现，请求权限一直静默失败回退打开设置）、`Capture.LaunchAtLogin`、`Capture.OpenLoginItemsSettings`。`PINSTILL_SELFTEST=perms` 断言 7 分类 / 三状态含冻结词 / 六个 `data-perm-action` / 三个 `data-banner-action`；e2e 新增一步通过。
- **稳定性（R7.2，go-native）**：新增 `internal/store/recovery_test.go`、`internal/pin/recovery_test.go`、`internal/app/pins_geometry_test.go`（经 Lead 授权，仅测试文件）并扩充 capture/store/stitch 测试；修 2 个真 bug——store.Prune `retentionDays` 的 `time.Duration` 溢出（数据丢失级，极大保留天数会把 cut-off 算到未来/2211 年，已 clamp 到 100 年）、stitch 首帧高于 MaxHeight 时 `Size()`/`Result()` 与画布不一致。`gofmt`/`go vet`/`go test`/`go test -race ./internal/...`/Windows cross 全绿。
- **手感与观感（R7.3，frontend）**：四页统一 120–180ms 入场动效并尊重 `prefers-reduced-motion`；按钮 pending/✓ 状态统一（复制/导出/贴图/清空历史/权限动作）；编辑器状态行改为「适应窗口 · 实际 W×H px」，超大图按比例适配不溢出；`:focus-visible` 环、调色板 `aria-pressed`、Esc 输入框/取消冲突处理；历史卡片 hover 删除 + 两次点击确认（3.2s 复位）、空状态文案、缩略图破图兜底。headless 四页 console/errors 为空。
- **验收**：`work/e2e.sh --skip-build`（隔离 `PINSTILL_DATA_DIR`）20 段 **ALL CHECKS PASSED**；`mygo build` 签名通过（Developer ID，Developer ID）；README 截图按新 UI 再生；README/plan 更新。
- **已知遗留（不阻塞）**：`restorePins()` 本体无单测（恢复链路由 pinpersist e2e 覆盖）；store 不校验「索引合法但 PNG 被外部删除」；pin 恢复不做 PNG 解码校验；System Events 合成 Escape 偶发不达（selftest 已加「先激活 app + 3 次重试」，本轮全过）。

## R7.7 独立验收（verifier，2026-11-10 深夜）

结论 **PASS**（0 BLOCKER / 0 MAJOR / 2 MINOR / 2 NIT）。基线 `d74fcb7`，验收前后 `git status` 均为空、全程只读。

- **契约逐字核对通过**：3 个新绑定语义正确（含 mygo `OpenAtLogin` 为只读 getter 的源码核对）；7 分类、9 个冻结 DOM hooks、6 + 3 个动作与 plan 一致；浏览器降级显示「浏览器预览 · 无需权限」不误报「已授权」；用 fake `window.mygo` 模拟原生横幅显示/隐藏/focus 刷新/capture:failed/dismiss 全过；`PINSTILL_SELFTEST=perms` 两次输出逐字一致。
- **稳定性修复双向验证**：在 /tmp 副本「改坏→测试变红」实锤两个 bug 存在且测试能抓（去掉 clamp 后 `Prune(1000000)` 真清空历史；stitch 旧代码 height=300 vs capped 100），自写对抗用例（MaxInt64 retention、MaxHeight 边界/1px 帧/二次 Add 等）在修复版全过。
- **构建/测试本机复跑全绿**：gofmt/vet/test/-race、Windows amd64+arm64 cross、build:web 四页、签名构建（Developer ID，codesign valid）。
- **E2E 3 次 × 20 步骤（隔离数据目录）**：run1、run3 ALL CHECKS PASSED；run2 出现 1 次 flow pin 子步超时（见 MINOR-1）。
- **前端回归 71/71 + 原生模拟 43 checks**：坐标映射（显示缩放 0.873 下命中图片坐标）、大图适配、历史二次确认/超时复位/破图兜底、pending 不卡死、Esc、aria-pressed、focus-visible、动效时长与 reduced-motion。
- **隐私**：outputs 六张截图均合成内容；README 口径与实现一致。

遗留与处置：
- MINOR-1（e2e flow pin 子步偶发，pre-existing overlay 会话时序抖动，HEAD~1 即存在）：下轮修复（beginRegion 显式重置 overlay 会话 / action-ready 重试）。
- MINOR-2（权限「重新检测」按钮 pending 在原生读取完成前被清除）：本轮已修——`main.ts` 把 `setButtonPending(false)` 移到 `refreshPermissionCenter/refreshPermissionStatus/refreshPermissionBanner` 之后。
- NIT-1（非 darwin 登录项错误未导出为 `ErrUnsupported`）：语义一致，保留实现命名，R7.1 表格已注明。
- NIT-2（`selftestPerms` 只打印未断言 banner hidden）：本轮已修——断言 banner 显隐与屏幕录制授权状态一致。

---

# R7.8 应用内调色板面板（用户试用反馈，2026-11-10 深夜）

用户反馈「调色盘还是没有」。R5 做的「8 个预设色点 + 原生 `<input type="color">` 自定义取色 + 最近 4 色」不够：要一个**应用内自绘的完整调色板面板**，不依赖系统原生取色器（WKWebView 行为不可控），overlay 工具栏与主编辑器属性条共用同一套 UI/状态。

## 交互（冻结）

- 触发按钮：`[data-palette-toggle]`（overlay 工具栏与主编辑器属性条各一个，aria-label=「调色板」；overlay 里「+」自定义色块改为该按钮的别名，保留既有 `data-color-custom` 属性）。
- 面板：`#colorPalettePanel`，全局单例（同一时刻最多一个），暗色暖调与现有设计一致；打开时锚定在触发按钮附近并夹在窗口可视区内。
- 面板内容：
  1. 预设色网格：≥ 32 个色块，`button[data-palette-color="#rrggbb"]`（值=应用颜色，小写 hex）。
  2. 自定义区：饱和度/明度方块 `#paletteSV`（canvas，pointer 拖动取色）+ 色相滑条 `#paletteHue`（input[type=range]，0–360）+ 预览色块 `#palettePreview` + HEX 输入 `#paletteHex`（合法 6 位 hex 回车应用，非法不应用）。
  3. 最近颜色：最多 8 个 `[data-palette-recent="#rrggbb"]`，复用 `colors.ts` 的 `pinstill.recentColors` 存储（MAX_RECENT_COLORS 提到 8）。
  4. 关闭：`[data-palette-close]`；面板外点击关闭。
- 行为：
  - 选择任一颜色**立即应用**到当前工具并写入最近色（overlay 作用于 Engine，主编辑器作用于 editor 当前色）。
  - **Esc 优先级**：面板打开时 Esc 只关闭面板，不取消截图/不退出标注态；面板关闭后 Esc 行为不变。
  - overlay 退出标注态/取消截图时面板自动关闭；主窗口切视图时关闭。
- 移除对 `<input type="color">` 的依赖（可保留隐藏 input 作兜底，但主路径自绘）。
- 现有快捷色点行保留不变（仍可直接点）。

## DOM hooks（frozen，selftest/headless 断言用）

`[data-palette-toggle]`、`#colorPalettePanel`、`button[data-palette-color]`（≥32）、`#paletteSV`、`#paletteHue`、`#paletteHex`、`#palettePreview`、`[data-palette-recent]`、`[data-palette-close]`。

## 验证（Lead）

- `PINSTILL_SELFTEST=palette`：overlay 拖选进入标注态 → 点 `[data-palette-toggle]` → 断言面板可见、预设 ≥32；点第一个预设色 → 面板仍开、`#paletteHex` 值等于该色、工具栏当前色选中态更新；Esc → 面板隐藏且 overlay 仍在标注态；再开面板点 `[data-palette-close]` 隐藏；切到主窗口编辑器断言 `[data-palette-toggle]` 与面板可用。打印 `SELFTEST palette: presets=N hex=#... applied=true esc_kept_annotating=true editor=true checks=ok`。
- `work/e2e.sh` 增加 palette 步骤；完整 e2e 全绿；前端 headless 四页无 console 错误。
- 前端 build:web 四页通过；不得回归 flow/annotate/pin 既有 e2e 步骤（工具栏按钮布局变更是风险点）。

## 非目标

颜色透明度/渐变、取色吸管（屏幕取色）、自定义色板保存/命名（后续可做）。

## R7.8 实测结论（Lead，2026-11-10 深夜）

- **应用内调色板面板上线**：`frontend/src/palette.ts` 自绘面板（不依赖 WKWebView 原生取色器）——40 个预设色网格 + `#paletteSV` 饱和度/明度拖动 + `#paletteHue` 色相滑条 + `#paletteHex` HEX 输入 + `#palettePreview` 预览 + 最近 8 色（与两页共享的 `pinstill.recentColors` 复用）；overlay 工具栏与主编辑器属性条的「+」自定义色块改为 `[data-palette-toggle]`（保留 `data-color-custom`，工具栏按钮数量/布局不变）。
- **Esc 优先级正确**：面板打开时 Esc 只关面板并 `preventDefault`，不取消截图/不退出标注态；面板关闭后 Esc 行为不变。点面板外、关闭按钮、切视图、开始新截图都会收起面板。
- **验证**：`PINSTILL_SELFTEST=palette` 连过——open=true / presets=40 / picked=#e56e48 → hex 回读一致 / preview=rgb(229,110,72) / esc_kept_annotating=true / close 按钮与重开正常 / 主编辑器 editor_toggle=true、editor_panel=true、editor_presets=40 且可关闭；`work/e2e.sh` 新增 palette 步骤后完整 e2e（隔离数据目录）**21 步 ALL CHECKS PASSED**；flow/annotate/pin 等既有步骤无回归。
- README 调色板描述与 README 截图已更新；未桥接的浏览器预览同样可开关/选色/拖动/输入/持久化。
- 非目标（沿用 R7.8 契约）：颜色透明度/渐变、屏幕吸管取色、自定义色板保存命名。

---

# Round 8：移除编辑器，主窗口只做历史与配置（2026-11-10 深夜，用户拍板）

用户试用后拍板：「压根不需要编辑器这个了，一切都是在窗口里操作了；历史和配置都要在。」
即：**删除主窗口编辑器与「进入编辑器」整条链路**；标注只发生在选区工具栏（overlay）；主窗口 = 历史 + 偏好设置。

## R8.1 产品行为（冻结）

1. 主窗口导航：**历史（默认）/ 偏好设置**；不再有「截图」页、编辑画布、主窗口标注工具栏。
2. overlay 完成动作：**复制 / 贴图 / 保存 / 取消**（删「进入编辑器」）；Enter 复制、Esc 取消不变；`afterCapture` 仅 `inplace|clipboard|pin`。
3. 滚动截图控制条：**复制 / 保存 / 贴图 / 取消**（删「进编辑器」）。
4. 整屏截图（托盘/菜单）：静默截当前显示器 → 存历史 → 按 afterCapture 执行（`inplace`/`clipboard` 视为复制剪贴板，`pin` 贴图）→ toast；不打开主窗口。
5. 历史页：网格卡片（缩略图/时间/尺寸）；点击卡片打开**预览弹层**（原图 + 复制 / 保存 / 贴图 / 删除 / 关闭；Esc 关闭；←/→ 切换相邻）；卡片 hover 删除保留（两次点击确认）；顶部「截一张」、快捷键提示、清空历史；空状态引导。
6. 设置「截图后」去掉「进入编辑器」；其余设置不变。
7. 托盘菜单「显示主窗口」改名「历史与设置」，行为不变（打开主窗口，落在历史）。
8. 启动仍显示主窗口（落到历史页）。

## R8.2 Go 绑定（History 服务，名字冻结）

| 方法 | 返回 | 语义 |
|---|---|---|
| `Copy(id string)` | `error` | 历史原图写入剪贴板 |
| `Save(id string)` | `string`（路径） | 系统保存对话框保存历史原图 |
| `Pin(id string)` | `string`（贴图 id） | 历史原图固定到屏幕 |

预览图片用既有 `HistoryEntry.url`（`shot://` 原图），不新增取图接口。

移除：`capture:completed` 事件与主窗口交付路径（`captureDeliver`/`deliverPNG`）；`FinishAnnotated` 不再接受 `annotate`；`FinishScroll` 不再接受 `annotate`。

## R8.3 前端（frontend/）

- `index.html`：删除编辑器区域（`shotCanvas`/`captureToolbar`/`toolProperties`/`resultPanel`/`pinnedPreview`/右侧说明栏）与相关 CSS；主窗口改为「历史为主内容 + 设置视图」；新增预览弹层 `#historyPreview`。
- 冻结 DOM hooks（e2e/headless 用）：
  - 主导航：`button[data-view="history"|"settings"]`（不再有 `rack`；`#shotCanvas`/`#captureToolbar` 必须不存在）
  - 历史：`#historyGrid`、`.history-card`、`#historyEmpty`、`#historyPreview`、`#historyPreviewImage`、`[data-history-action="copy|save|pin|delete|close"]`
  - 保留：`#permissionBanner`/`#permScreenStatus` 等 R7.1 hooks、`#settingsNav button[data-settings-cat]`
- `main.ts`：删除 Editor 集成、`capture:completed` 处理、主窗口截图视图；历史预览接线新绑定；默认 view=history。
- `bridge.ts`：新增 `historyCopy/historySave/historyPin`；移除 `onCaptureCompleted`/`CaptureCompletedPayload`；`FinishAnnotatedAction` 去掉 `annotate`。
- `overlay.ts`/`scroll.ts`：移除 editor 动作、`afterCapture="editor"` 分支（旧 localStorage 值读取时映射为 `inplace`）。
- `settings.ts`：afterCapture 去掉 editor 选项。
- 删除主窗口编辑器相关模块 `editor.ts`、`demo.ts`；调色板面板保留给 overlay（主窗口不再有调色板）。

## R8.4 Go 清理（Lead）

- `internal/app`：删 `captureDeliver`、`EventCaptureCompleted` 及其 emit、`applyCaptureAction` 的 annotate 分支；`captureFullScreen` 走 R8.1-4 新流程；`FinishAnnotated`/`FinishScroll` 动作集合收敛；History 新绑定；托盘文案。
- selftest 重构：删除 `deliver`/`uiactions`/`demo`；改写 `drag`（PrepareSelection 后比较 session PNG 与直接截屏，不依赖编辑器）、`flow`（去掉 annotate 步骤）、`scroll`/`scrollflow`（长图尺寸改从 clipboard/history 校验）、`showcase`（去掉 editor 截图，历史用 storeCapture 播种，新增 preview 截图）、`uicapture`（断言无编辑器 DOM，拍历史/设置）；新增 `fullscreen`（整屏截图→剪贴板+历史）与 `historyactions`（Copy/Pin/Delete 经由 HistoryService）。
- `work/e2e.sh` 按新 selftest 列表重写；README 全量更新（删除编辑器相关描述与截图，补历史预览/整屏行为）；截图再生。
- 新增/修订单测：`FinishAnnotated` 动作校验、历史动作（可在 selftest 覆盖，不做模态保存的自动化）。

## R8.5 非目标

主窗口内标注、历史重新编辑、编辑窗口/独立编辑器（已按用户要求删除）、云同步。

## R8.6 实测结论（Lead，2026-11-10 深夜）

- **编辑器链路已彻底移除**：主窗口只剩「历史（默认）+ 偏好设置」；`#shotCanvas`/`#captureToolbar`/`toolProperties`/`resultPanel`/`pinnedPreview` 全部不存在；overlay 完成动作只剩复制 / 贴图 / 保存 / 取消（Enter 复制、Esc 取消不变）；滚动控制条动作同为复制 / 保存 / 贴图 / 取消；设置「截图后」只剩就地 / 复制 / 贴图；`editor.ts`/`demo.ts` 删除。
- **历史与预览**：点历史卡片打开 `#historyPreview` 大图弹层（复制 / 保存 / 贴图 / 删除 / 关闭，Esc 关闭、←/→ 切换、点 backdrop 关闭）；卡片 hover 删除（二次确认）与清空历史保留；空状态引导。新增 Go 绑定 `History.Copy/Save/Pin`（Save 走系统对话框，未自动化）。
- **整屏截图**：托盘「截取整个屏幕」改为静默截当前显示器 → 存历史 → 按「截图后」执行（inplace/clipboard → 复制剪贴板，pin → 贴图）→ toast，不打开主窗口、无编辑器。
- **Go 清理**：删 `captureDeliver`/`deliverPNG`/`capture:completed` 事件/`applyCaptureAction` annotate 分支/`FinishRegion`/`CompleteSelection` 旧绑定；`FinishAnnotated` 只接受 copy|pin|save|cancel；`FinishScroll` 同样；托盘菜单「显示主窗口」→「历史与设置」；types_test/事件契约同步。
- **验证**：`work/e2e.sh --skip-build`（隔离数据目录）**23 步 ALL CHECKS PASSED**；其中 drag 改为「页面拖选 → PrepareSelection 后与直接截屏比较」same=true；scroll/scrollflow 长图经剪贴板校验 1280×2300 / 1280×1700；新增 fullscreen（整屏 3840×2160 进剪贴板 + 历史 +1）与 historyactions（Copy/Pin/Remove，delta=1）；uicapture 断言 `views=history,settings`、`editor_dom=false`、预览打开/关闭，并截取 history/preview/settings；permissions/palette 回归通过（palette 断言主窗口无调色板）。
- README 与结构说明已更新（删除编辑器截图，新增历史预览截图），签名构建通过（Developer ID）。
- 已知：设置里少数原本只影响主窗口编辑器的项（图标文字提示、Enter 保存等）暂为惰性项；预览保存对话框未自动化。

---

# Round 9：滚动截图 2.0（对齐主流：默认自动滚动 + 自动收尾 + 结果预览）（2026-11-11 凌晨，用户反馈）

用户反馈：「滚动截图做的和主流都不一样，优化一下」。主流工具（PixPin / 微信 / QQ / CleanShot）是：选区里点长截图 → **默认自动滚动** → 到底或停止后**自动完成** → 弹出**长图预览窗口**（复制 / 保存 / 贴图 / 关闭）。现状是默认手动滚动 + 独立控制条等用户点动作 + 完成后没有预览。

## R9.1 交互（冻结）

1. 选区工具栏点「滚动截图」→ 默认请求**自动滚动**：overlay 隐藏，控制条（380×64）贴在选区旁：
   - 状态文案：「正在自动滚动… 已拼接 N 帧 · H px」/「手动滚动中…」/「已暂停」/「已达上限」
   - 按钮：`暂停/继续`、`自动/手动`、`立即完成`、`取消`
   - 辅助功能未授权：自动不可用 → **降级为手动模式开始**，控制条显示「自动滚动需要辅助功能权限」+「去授权」；授权后可切回自动。
2. **自动收尾**（不再等用户点动作）：满足任一条件 → 停止抓帧 → 拼接 → 存历史 → 打开**结果预览窗口**：
   - 连续 3 帧几乎相同（到底/停顿，约 1.2s 无新内容）
   - 达到上限（120 帧 / 30000px）
   - 用户点「立即完成」（至少 1 帧）
   - `取消`/Esc/关闭控制条 → 丢弃，无结果窗口
3. **结果预览窗口**：新窗口 `/result.html`（普通窗口，1000×760 上限，按图片比例适配、居中于选区所在显示器）：
   - 标题「滚动截图」+ 尺寸/帧数/上限提示 + 关闭按钮
   - 长图按窗口宽度等比展示、可滚动查看
   - 动作：**复制 / 保存 / 贴图 / 关闭**；Esc 关闭；复制/保存/贴图成功后自动关闭（图片已进历史）
4. 手动模式保留：用户自己滚动，停顿约 1.2s 自动收尾出预览；控制条同样可「立即完成」。

## R9.2 事件/绑定（冻结）

- `scroll:init` payload 增加 `autoDenied`；`scroll:progress` 状态 `running|paused|stalled|max`，`auto` 表示当前模式。
- 结果窗口事件 `scroll:result` payload `{url,width,height,frames,capped,historyId}`。
- Capture 服务新增：`CopyScrollResult() error`、`SaveScrollResult() (string,error)`、`PinScrollResult() (string,error)`、`CloseScrollResult()`、`SetScrollPaused(paused bool) error`（控制条暂停/继续；暂停时状态 `paused`）。
- `StartScroll(rect, auto)`：auto 不可用时**不报错**，以手动模式开始并给 `autoDenied=true`。
- `FinishScroll(action)`：`finish`（立即收尾出结果窗口）或 `cancel`（丢弃）；旧的 copy/pin/save 不再作为控制条动作。

## R9.3 前端

- `scroll.html`/`scroll.ts`：控制条重做（状态、暂停/继续、自动/手动、立即完成、取消、授权引导；不再有结果动作按钮）。
- 新增 `result.html` + `src/result.ts` + vite 入口：预览窗口（图片自适应宽度、可滚动、复制/保存/贴图/关闭、Esc、动作后关闭）。
- `overlay.ts`：滚动按钮 `StartScroll(rect, true)`。
- `bridge.ts`：结果动作包装、`onScrollResult`，更新 scroll 事件类型。
- 冻结 DOM hooks：控制条 `#scrollStatus`、`#scrollProgress`、`[data-scroll-action="pause|mode|finish|cancel"]`、`[data-scroll-permission]`；结果窗口 `#resultImage`、`#resultMeta`、`[data-result-action="copy|save|pin|close"]`。

## R9.4 Go（Lead）

- scroll.go：auto 降级、自动收尾、结果窗口（复用单例窗口，关闭即隐藏）、事件与 4 个绑定、上限/停顿文案；控制条 380×64。
- selftest：`scroll`（自动+模拟帧→自动收尾→结果窗口断言）、`scrollflow`（页面驱动）、`scrollmanual`（手动+停顿自动收尾）、`scrollshot`（结果窗口截图）；`e2e` 更新；类型契约测试更新。
- README/截图更新：滚动截图描述改为「默认自动滚动、自动完成、结果预览」，截图换控制条 + 结果窗口。

## R9.5 非目标

固定表头/表尾智能去除、横向滚动、结果窗口内编辑、窗口缩放手柄。

## R9.6 实测结论（Lead，2026-11-11 凌晨）

- **默认自动滚动 + 自动收尾 + 结果预览**已实现：`StartScroll(rect, true)` 为默认；辅助功能缺失时降级手动并在控制条给出授权入口；连续 3 帧静止（≈1.2s）、达到上限或「立即完成」都会自动收尾 → 拼接 → 存历史 → 打开 `/result.html` 结果预览窗口；预览窗口支持复制 / 保存 / 贴图 / 关闭（Esc），复制/保存/贴图成功后自动关闭。
- **新增绑定**：`Capture.SetScrollPaused`、`CopyScrollResult`、`SaveScrollResult`、`PinScrollResult`、`CloseScrollResult`；事件 `scroll:result`；`scroll:init` 增加 `autoDenied`，`scroll:progress` 增加 `paused` 状态；控制条 380×64，结果窗口 980×720 上限按显示器夹取。
- **selftest**：`scroll`（自动收尾 → 结果 1280×2600 → 剪贴板 1280×2600 → 复制后窗口关闭）、`scrollmanual`（手动模式停顿自动收尾）、`scrollflow`（页面驱动 → 结果窗口 DOM：图片 1280×2600 / 9 帧 / copy,save,pin,close / 关闭）全绿；`scrollshot` 产出控制条与结果窗口截图（用于 README，需解锁后重拍）。
- `go test`/`go vet` 全绿；`npm run build:web` 5 页通过。
- **解锁后补齐（2026-11-11 中午）**：Developer ID 签名构建（带安全时间戳）通过；完整 `work/e2e.sh --skip-build`（隔离数据目录）**22 步 ALL CHECKS PASSED**——scroll / scrollflow / scrollmanual 三步全绿（结果 1280×2600、剪贴板校验、结果窗口 DOM 四动作、手动模式自动收尾）；README 滚动截图文案更新，新增控制条 + 长图预览两张截图（`pinstill-scroll.jpg` / `pinstill-scroll-result.jpg`）。
- **顺手修复（用户反馈）**：调色板触发器在选区快速色点行里原来显示成「彩虹圆 + 小图标」，看起来像误入的 emoji；现改为中性 19px 圆角按钮 + 11px 调色板图标（hover/展开时 amber 高亮），overlay 截图已重拍。

## R9.7 用户试用反馈修复（2026-11-11 下午）

1. **主窗口左上角品牌被「红黄绿」交通灯压住**：macOS 隐藏标题栏（hidden inset）把窗口控制按钮浮在页面左上角，而历史页的 rail 顶部只有 24px 内边距，品牌标记正好被三个按钮叠住。修复：`main.ts` 在 macOS 下给 `<html>` 加 `.is-mac`，CSS 里 `.is-mac .rail{padding-top:56px}`，品牌挪到交通灯下方（Windows/浏览器不受影响）。
2. **贴图窗口的关闭按钮太大太抢**：恢复保留关闭按钮（Esc / 右键菜单照旧），但缩小为 18×18 的半透明「玻璃」小按钮（图标 10px、无边框、悬停淡入、focus-visible 细琥珀描边）；结果预览窗口右上角关闭按钮也从 26px 收到 22px 并减小图标，和整体更协调。
3. 验证：`pinui` 通过（关闭按钮点击 → pins=0/stored=0）；`npm run build:web` 5 页；签名构建（Developer ID，带时间戳）；完整 e2e 复跑。

## R9.8 用户试用反馈：默认线条与工具栏拖动（2026-11-11 傍晚）

1. **默认线条太粗**：标注默认粗细 4 → **2**（`settings.ts` 默认值、overlay 兜底、滑块初始值同步；滑条仍可在 1–10 间调整）。已有用户保存过的旧默认 4 会**一次性迁移**为 2（`pinstill.prefsWidthMigration` 标记，只迁移恰好等于旧默认 4 的值）。
2. **标注工具栏（菜单栏）可拖动**：用户澄清「拖拽指的是拖拽这个菜单栏」。工具栏第一行最左新增六点**拖动手柄** `#annotationHandle`（也可按住工具栏非按钮空白处拖动）；拖动位置夹在窗口可视区、在当前截图会话内保持（新截图重新自动摆放）；拖拽后复制/贴图/保存等动作不受影响。
3. 原「拖拽」画笔工具（移动最后一条标注）改名为 **「移动标注」**，避免与拖工具栏混淆。
4. 验证：`flow` selftest 新增 `toolbar_drag=60,40`（拖动手柄后工具栏位移正确、随后复制仍成功）；完整 e2e 全绿；README 标注工具栏描述更新；overlay 截图重拍（含手柄、粗细显示 2）。

## R9.9 设置页与实际能力对齐（2026-11-11 傍晚）

R8 删除主窗口编辑器后，设置页有若干项变成「惰性」（保存了但没人读）。本轮把它们**接线到 overlay**，而不是删掉控件：

1. 「图标文字提示」「提示出现延迟」：overlay 工具栏 tooltip 现在真正读取（关闭后悬停不再显示工具名，延迟可调 0–2s，默认 120ms）。
2. 「Enter 保存到剪贴板」：overlay 标注态的 Enter 复制行为受该开关控制（默认开）。
3. 「默认工具」：overlay 进入标注态时默认选中该工具（此前硬编码矩形）。
4. 新增「滚动截图默认自动」开关（通用与截图）：overlay 调用 `StartScroll` 时按偏好决定默认自动/手动，控制条里仍可临时切换。
5. 「常用编辑按键」改名「标注快捷键」，内容更新为 撤销 / 完成复制 Enter / 取消 Esc。
6. 清掉 `main.ts` 里已无消费者的 `--tooltip-delay` 写入。
7. OCR / 翻译 / 二维码相关偏好保持原样（功能仍是「待接入」，设置页注明后续接入）。

验证：`npm run build:web` 5 页；完整 `work/e2e.sh --skip-build` **ALL CHECKS PASSED**；签名构建（Developer ID，带时间戳）；settings 截图按新增行重拍。

---

# R10：文字样式（粗体 / 斜体 / 字号 / 字体）（2026-11-11 晚，用户反馈）

用户反馈：「看人家字体这里。我觉得粗体斜体要支持吧，如果能支持设置字体就更好了 / 默认线条都太粗了，优化下」。

## R10.1 交互（冻结）

1. 文字工具新增样式控件（位于颜色点之后、完成动作之前）：
   - **B / I** 切换按钮（`aria-pressed`），默认常规；
   - **字号** 下拉 `#textSize`：10/11/12/14/16/18/20/24/28/32pt，**默认 14pt**；
   - **字体** 下拉 `#textFont`：系统默认 / 苹方 / Helvetica / Georgia / Menlo。
2. 选中「文字」工具时显示这些控件并**隐藏线条粗细滑条**（文字的字号不再由粗细滑条推导）；切到其它工具恢复粗细滑条。
3. 文字渲染使用显式样式：默认 `400 14pt 系统默认`（此前约 22pt、500 字重，正是「默认线条太粗」的来源）；内联输入框用同一套字体/字号/粗斜体实时预览。
4. 最近使用的文字样式持久化到 `pinstill.textStyle`，下一次截图沿用；标签工具保持图标样式不变。

## R10.2 验证

- `PINSTILL_SELFTEST=textstyle`：断言文字工具下样式控件可见、粗细滑条隐藏、B/I 切换、字号=20pt、字体下拉非空、点击画布打开内联输入框并提交；测试前后保存/恢复用户 localStorage 里的文字样式，**不污染用户默认值**。
- e2e 增加该步骤（应用级自检 20 项）；完整 e2e ALL CHECKS PASSED；签名构建；README 与 overlay 截图更新（截图展示文字工具控件与示例文字）。

## R11：双击画布即复制（2026-11-11 晚，用户反馈）

用户反馈：「截图后，双击就是复制，不是一定要选择复制键」。

- overlay 标注态在**画布上双击**（350ms 内同一位置两次按下，位移 ≤8px）→ 直接完成复制，与点「复制」按钮等价；若当时有未提交的内联文字，先提交再复制。
- 普通单击行为不变（选中文字工具时单击仍是打开输入框；其它工具单击不产生图形——引擎本来就会丢弃零尺寸草稿）。
- 新一次截图会重置双击判定；双击只作用于画布（工具栏/调色板/输入框不触发）。
- 验证：新增 `PINSTILL_SELFTEST=dblclick`（选区标注态双击 → 剪贴板出现 640×420 合成图，`checks=ok`）；e2e 增加该步（应用级自检 21 项）；完整 e2e ALL CHECKS PASSED。README 的快捷键说明更新为「Enter 或双击画布复制」。

## R12：选区四角调整 + 移除「移动标注」（2026-11-11 晚，用户反馈）

用户指出：①工具栏最左那个四向箭头「移动标注」工具没什么用（还容易和"拖动工具栏"混淆）；②截图落到选区后，应该能拖 4 个角调整选区范围。

- **移除「移动标注」工具**（引擎内部移动能力保留但不再暴露在工具栏）；「默认工具」下拉同步去掉；存量设置为 drag 的用户会回落到矩形。
- **选区四角调整**：标注态在选区四个角显示 12px 拖拽手柄（nw/ne/sw/se），按住拖动实时调整选区范围与尺寸角标；松开后 overlay 通过 `PrepareSelection` **重新截取新范围**并回到标注态（原有标注重置，若有标注会提示「选区已调整 · 已重新截取（原有标注已重置）」）。手柄只在标注态出现，截图进行中隐藏；最小选区 24px。
- 双击复制、文字样式、工具栏拖动等既有行为不受影响。
- 验证：新增 `PINSTILL_SELFTEST=resize`（断言 4 个手柄；拖 SE 角 +120/+80 后 canvas 从 640×420 变为 880×580 并回到标注态）；e2e 增加该步（应用级自检 22 项）；完整 e2e **ALL CHECKS PASSED**；签名构建；README 与 overlay 截图更新（截图可见四角手柄、工具栏已无移动工具）。

**R12 修正（用户反馈）**：拖角时原来的做法是把已截的图拉伸到新选框，看起来像"放大缩小截图"，不符合主流。现改为：拖动四角期间隐藏 canvas/标注，让透明选区透出**实时屏幕**（与主流调整选区一致）；松手后重新截取新范围。未移动的点击（按下即松开）只恢复显示、不重截、不重置标注。resize 自检仍通过（640×420 → 880×580），完整 e2e 复跑 ALL CHECKS PASSED。

**R12 延迟优化（用户反馈「拖动有延迟」）**：
1. 遮罩从「单个 `.selection` 的 `box-shadow: 0 0 0 200vmax`」改为**4 块小遮罩面板**（`#maskTop/Bottom/Left/Right`，随选区定位）。原来拖动/缩放时整屏都要重绘阴影，边框跟手差；现在只重绘边缘区域，选区和四角拖动流畅。
2. 松手重截的等待：
   - **缩小**选区时不需要重截：直接把已有 canvas 本地裁剪（实测 **1–2ms**，无感）；
   - **放大**选区需要重新读屏幕（新内容不在旧图里），新增 `Capture.PrepareSelectionFast`（页面隐藏 chrome 后等一帧再截，跳过大部分合成等待），并把这段等待从空白改为显示「正在截取…」小 spinner（实测 ~245ms）。
3. `resize` 自检扩展为两段：放大（640×420 → 880×580，recapture_ms 打印）+ 缩小（880×580 → 680×460，shrink_ms 打印）；e2e 增加 shrink 断言；完整 e2e ALL CHECKS PASSED。

**R12 修正 2（用户反馈「拖的时候没立刻隐藏」）**：拖角时旧截图虽然已隐藏，但选区外的暗色遮罩还在，导致选区里亮、外面暗，看起来仍像"截图没消失"。现改为：**一按下四角手柄立即同时隐藏截图和暗色遮罩**（整屏恢复正常亮度，只剩选框/四角/尺寸角标跟着鼠标），松手后重新截取并恢复遮罩。`resize` 自检改为分步派发 press，并断言拖动中 `canvas_hidden_mid=true`、`masks_hidden_mid=true`（同时保存 `/tmp/pinstill-resize-mid.png` 供人工检查），放大/缩小时延仍为 ~244ms / ~1ms；完整 e2e ALL CHECKS PASSED。

## R13：修复工具栏 hover 提示消失（2026-11-12，用户反馈）

用户反馈「工具栏 hover 提示没了」。原因：R9.9 接入「图标文字提示」设置时，`document.documentElement.style.setProperty("--tooltip-delay", ...)` 和 `if (!tooltipsEnabled) toolbar.classList.add("no-tooltips")` 两行写在**读取偏好之前**，此时 `tooltipsEnabled` 尚未初始化（undefined）→ 每次启动都无条件加上 `no-tooltips`，所有 hover 提示被隐藏。

修复：把这两行移到偏好解析之后执行。验证：`palette` 自检新增断言（工具栏无 `no-tooltips`、`--tooltip-delay` 有效），e2e 增加 `tooltips=true` 断言；完整 e2e ALL CHECKS PASSED。

## R14：滚动截图时显示选区范围框（2026-11-12，用户反馈）

用户反馈：「滚动截图开始的时候，范围框还是要显示的，不然不知道截的是哪块」。

- 新增一个**透明、置顶、点击穿透**的细边框窗口 `frame.html`（`a.scrollFrame`）：滚动截图开始、overlay 隐藏后，用它标出正在截取的范围。
- 边框紧贴选区**外侧**：窗口尺寸 = 选区 + 每边 2px，`frame.html` 只画 2px 琥珀色描边 → 描边像素永远在截图区域之外，**不会进入长图**。
- `SetIgnoreMouseEvents(true)` 点击穿透，手动滚动仍能滚到底下 App；会话结束（完成/取消/失败/预览打开）立即隐藏。
- 验证：`scroll`/`scrollmanual` 自检断言会话中 `frame_visible=true frame_size=524x424`（选区 520×420 + 2px 边框）、结束后 `frame_hidden=true`；e2e 增加两处断言；完整 e2e **ALL CHECKS PASSED**；README 更新。
