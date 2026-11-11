# Pinstill · 钉图

**Pinstill** (n. pin + still) is a desktop screenshot and annotation tool for macOS and Windows: capture, annotate in place, and pin shots onto your desktop.

Pinstill 是一个面向 macOS 和 Windows 的桌面截图与标注工具。它把「截取屏幕 → 在选区旁就地操作（标注 / 复制 / 贴图 / 保存）→ 把截图挂在桌面上 → 回到历史」放进同一条工作流，截图默认只保存在本机。

桌面外壳使用 [MyGo](https://mygo.egoist.dev/)（Go + 系统 WebView，纯 Go、无 cgo），界面是设计稿演化而来的 Vite 前端。

## 当前状态

真实可用（macOS 已在本机端到端验证）：

- **区域截图 + 就地完整工具栏**：全局快捷键或托盘唤起全屏选区，遮罩 + 十字线 + 尺寸角标；松手后 Go **透过透明选区直接截屏（无闪烁）**、overlay 就地把截图显示在选区里，并浮出**完整标注工具栏**——矩形/圆/直线/箭头/画笔/文字/标签/马赛克/高亮/水印、颜色与粗细、撤销，翻译 / 提取文字 / 二维码（标注「待接入」）、滚动截图（真实功能），以及复制 / 贴图 / 保存 / 取消。标注直接发生在桌面上，不用先跳窗口（Enter 复制、⌘Z 撤销、Esc 取消）
- **文字/标签**：选中工具后在画面上点击要放文字的位置，输入框就地出现直接输入；Enter 提交、Shift+Enter 换行、Esc 取消（与主流截图工具一致）
- **调色板**：应用内自绘调色板面板——40 个预设色 + 饱和度/明度方块 + 色相滑条 + HEX 输入 + 最近 8 色（持久化）；选色即时生效，Esc 只关面板、不取消截图
- **权限引导**：屏幕录制权限缺失时在选区原地提示「需要屏幕录制权限」并可一键打开系统设置，不再突然跳进主窗口
- **整屏截图**：托盘菜单「截取整个屏幕」静默截取鼠标所在显示器 → 存历史 → 按「截图后」执行（默认复制到剪贴板），不打开主窗口
- **复制 / 导出**：一键写入剪贴板；按设置里的 PNG / JPEG、质量、文件名前缀走系统保存对话框，并默认定位到设置里的「默认保存文件夹」
- **挂在桌面（贴图）**：始终置顶的无边框贴图窗口；拖动摆放、滚轮缩放、右下角手柄按比例缩放、⌘/Ctrl+滚轮调透明度、右键菜单（复制 / 保存 / 关闭 / 透明度 / 原始大小）、双击复制、Esc 关闭；多张各自独立摆放，**重启应用后自动恢复**（图片、位置、大小、透明度）
- **滚动截图（默认自动）**：选区里点「滚动截图」后 overlay 隐藏、浮出紧凑控制条；默认自动滚动并抓帧拼接，到底 / 停顿约 1.2 秒 / 达到上限时**自动完成**并弹出长图结果预览窗口（复制 / 保存 / 贴图 / 关闭）；依赖 macOS「辅助功能」权限，未授权时自动降级为手动滚动（同样自动完成）；控制条可暂停/继续、切换自动/手动、立即完成、取消，长图同样进历史
- **历史与预览**：截图自动落盘、真实缩略图卡片；点击打开大图预览（复制 / 保存 / 贴图 / 删除，←/→ 切换、Esc 关闭）、单条删除（二次确认）/ 清空；保留天数与容量上限生效。主窗口只有历史 + 偏好设置，不再有编辑器
- **托盘**：菜单栏常驻图标：截取区域 / 截取整个屏幕 / 固定剪贴板图片 / 历史与设置 / 退出
- **全局快捷键**：默认 ⌘⇧1（截取区域）、⌘⇧2（固定剪贴板图片）、⌘⇧O（提取文字，能力待接入）；可在设置里重新录制，注册失败会提示
- **设置**：7 个分类（通用与截图 / 快捷键 / 保存与历史 / 标注与水印 / 贴图与桌面 / 识别与翻译 / 权限）、localStorage 持久化，并实时推给 Go 侧生效（截图后就地标注/复制/贴图、光标、提示音、格式、默认保存文件夹、登录启动、历史保留策略、贴图透明度/边框等）
- **权限中心**：设置 →「权限」实时显示屏幕录制、辅助功能（自动滚动）、登录时启动的真实系统状态，可一键请求权限、打开对应系统设置、重新检测；主窗口在缺少屏幕录制权限时显示可关闭的引导横幅

仍是「待接入」占位（点开有明确说明，不会假装可用）：OCR 文字识别、翻译、二维码识别。

平台情况：macOS 已构建并验证；Windows 有纯 Go 的 GDI 截图后端且交叉编译通过，**尚未在 Windows 真机验证**。

## 截图

以下截图均使用合成图/自绘遮罩，不含用户桌面内容。

拖选区域后，选区里就直接显示截图并浮出完整标注工具栏（截图为合成演示内容，含一枚演示标注）：

![选区就地标注](outputs/pinstill-overlay.jpg)

把截图挂在桌面（贴图，始终置顶）：

![桌面贴图](outputs/pinstill-pin.jpg)

滚动截图：默认自动滚动，控制条可暂停 / 切手动 / 立即完成（图为达到上限时的状态）：

![滚动截图控制条](outputs/pinstill-scroll.jpg)

滚动截图完成后的长图预览窗口（复制 / 保存 / 贴图 / 关闭，Esc 关闭）：

![滚动截图结果预览](outputs/pinstill-scroll-result.jpg)

历史（主窗口只有历史 + 偏好设置）：

![历史](outputs/pinstill-history.jpg)

点击历史卡片打开大图预览（复制 / 保存 / 贴图 / 删除，←/→ 切换、Esc 关闭）：

![历史预览](outputs/pinstill-preview.jpg)

偏好设置：

![偏好设置](outputs/pinstill-settings.jpg)

设计稿快照（交互原型，作为视觉基准保留）：[outputs/pinstill-design.html](outputs/pinstill-design.html)。可以在仓库根目录运行任意静态文件服务器查看：

```bash
python3 -m http.server 8765
# 打开 http://127.0.0.1:8765/outputs/pinstill-design.html
```

## 开发

需要 Go 1.27.3+、Node.js 22+ 与 npm。

```bash
npm install

# 构建前端产物
npm run build:web

# 运行 Go 测试（含 native 截图 integration 测试需屏幕录制权限）
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go test -tags integration ./internal/capture

# 启动桌面开发环境（Vite + MyGo 窗口，支持热更新）
npm run dev

# 构建 .app / .dmg（产物在 build/darwin-arm64/）
npx mygo build
```

构建使用 `mygo.config.ts` 里配置的 Developer ID 证书签名（应用与 dmg 都会签名），这样 macOS 的屏幕录制/辅助功能授权在重新构建后仍然有效。**当前没有做公证（notarization）**：从网上下载或在其他 Mac 上首次打开时，可能需要在「系统设置 → 隐私与安全性」里点「仍要打开」，或右键 → 打开。

### 结构

```
cmd/pinstill/main.go            桌面入口：单实例、绑定服务、窗口与托盘生命周期
internal/app/                编排层：截图流程、绑定服务、事件、托盘、快捷键、设置、自检
internal/capture/            平台截图后端（macOS screencapture / Windows GDI）+ 权限 + 选择框几何
internal/store/              历史持久化：PNG + 缩略图 + index.json + 保留策略
internal/pin/                桌面贴图持久化：图片 + pins.json（位置/大小/透明度）
frontend/index.html          主窗口（历史 / 设置）
frontend/overlay.html        全屏选区覆盖层
frontend/pin.html            置顶贴图窗口
frontend/scroll.html         滚动截图控制条
frontend/result.html         滚动截图长图预览
frontend/src/                TypeScript 模块（bridge 是唯一的原生调用入口）
vite.config.ts               五页多入口构建（index/overlay/pin/scroll/result）
work/plan.md                 本轮开发的冻结接口契约与实测结论
work/e2e.sh                  端到端验收脚本（构建 + 19 项 selftest）
```

### 端到端验收

```bash
bash work/e2e.sh            # 完整构建 + 全部自检
bash work/e2e.sh --skip-build   # 用现有构建产物只跑自检
```

脚本会依次跑 Go 测试 / vet / Windows 交叉编译、前端与桌面构建，以及 19 项应用级自检（真实退出、真实截图+历史+剪贴板、overlay 渲染、选区映射像素级对比、Esc 取消、**就地标注全流程 copy/pin/cancel + afterCapture 自动动作**、**桌面贴图交互（双击/菜单/滚轮/关闭）**、**贴图重启恢复**、**滚动截图**（默认自动滚动 → 自动完成 → 结果预览 → 复制；手动模式停顿自动完成；页面驱动结果窗口断言）、**整屏截图**、**历史动作 copy/pin/remove**、主窗口视图（断言无编辑器 DOM + 历史预览开关）、贴图窗口、全局快捷键注册、设置应用、**权限中心**、**调色板面板**）。自检通过环境变量驱动（`PINSTILL_SELFTEST=quit|capture|overlay|drag|esc|flow|pinui|pinpersist-setup|pinpersist-check|scroll|scrollflow|scrollmanual|fullscreen|historyactions|uicapture|pin|history|hotkeys|settings|perms|palette|showcase|scrollshot|hold`），产物与日志在 `/tmp/pinstill-selftest-*`。

### 数据与权限

- 历史目录：`~/Library/Application Support/Pinstill/captures/`（PNG + `*.thumb.jpg` + `index.json`）
- 桌面贴图：`~/Library/Application Support/Pinstill/pins/`（`<id>.png` + `pins.json`，重启后按此恢复）
- 当前会话缓存：`~/Library/Caches/Pinstill/session/`（应用启动时清空）
- 页面通过内部自定义协议 `shot://localhost/...` 读取这些图片，不经过网络
- 首次截图需要「屏幕录制」权限：系统设置 → 隐私与安全性 → 屏幕录制；权限缺失时应用会提示并可直接跳转设置页
- 开发/自检可用 `PINSTILL_DATA_DIR=/tmp/pinstill-data` 把 captures、pins、session 全部隔离到指定目录（默认行为不变；生成 README 截图的 `PINSTILL_SELFTEST=showcase` 强制要求设置该变量，避免读到真实历史）
- 旧名 **Tupen** 的历史数据会在首次启动新版本时自动迁移：`Application Support/Tupen → Application Support/Pinstill`、`Caches/Tupen → Caches/Pinstill`，以及 WebKit 的 `dev.echovic.tupen → dev.echovic.pinstill`；bundle id 变更后，系统「屏幕录制」权限需要重新授权一次

## 已知限制

- **Windows 未真机验证**（GDI 后端仅保证纯 Go 交叉编译通过；坐标按物理像素解释，与 macOS 的 point 语义不同，HiDPI 缩放下选区可能错位，当前按「基础实现」看待）
- **OCR / 翻译 / 二维码** 尚未接入服务，UI 明确标注「待接入」
- **滚动截图**：默认自动滚动，依赖 macOS「辅助功能」权限（未授权会自动降级为手动并给出授权入口）；滚轮事件在部分 App（自绘滚动/虚拟列表）可能不生效，可切手动滚；固定表头/表尾（网页顶栏、聊天输入框）会造成拼接错位，请框选内部滚动区；连续 3 帧无变化（约 1.2 秒）会自动完成，可先「暂停」再继续；上限 120 帧 / 30000px；Windows 手动拼接引擎交叉编译通过但未真机验证
- **overlay 覆盖的是屏幕可见工作区**（macOS 的 Panel 会被 AppKit 约束在菜单栏与 Dock 之外），无法直接框选菜单栏 / Dock 区域；选区坐标按窗口实际 frame 换算，截图内容不受影响
- **登录启动** 在未安装到 `/Applications` 的构建里会返回 `Operation not permitted`，设置页会如实展示；安装后的行为待人工确认
- **导出 / 保存 / 历史另存对话框**未纳入自动化自检（模态框），导出、格式转换与保存路径逻辑由单元测试 / History.Save 绑定覆盖，建议人工点一次确认
- 贴图恢复依赖保存时导出的几何信息：显示器拔插后越界的贴图会被夹回可见屏幕，但不会自动按比例重新排版
- 贴图数量上限 60 张（防止异常状态把桌面塞满），超过时旧贴图不恢复
- 全局快捷键通过 Carbon `RegisterEventHotKey` 注册（注册成功）；合成按键无法触发 Carbon 热键，自动化只能验证注册状态，**真实按键建议人工确认一次**
- 多显示器：overlay 只出现在鼠标所在显示器；DPI/缩放不同的混合显示器组合未充分测试

## 技术方向

- 桌面框架：MyGo（https://mygo.egoist.dev/）
- 图标：Lucide（ISC，社区开源图标库）
- 目标平台：macOS、Windows
- 当前阶段：核心截图链路与桌面贴图已打通，继续补齐识别类能力与 Windows 验证
