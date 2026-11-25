# Pinstill · 钉图

简体中文 | [English](README.en.md)

Pinstill 是免费开源的 Mac 截图工具。按 **⌘⇧1** 框选，在选区里直接标注、截长图、提取文字或识别二维码。做设计、写代码需要对照多张参考图时，可以把它们并排挂在主屏菜单栏下方。截图和识别都在本机处理。

[官网](https://pinstill.echovic.com/) · [下载最新版](https://github.com/echoVic/Pinstill/releases/latest) · [更新日志](CHANGELOG.md)

[![CI](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml/badge.svg)](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)
![macOS](https://img.shields.io/badge/platform-macOS%2013%2B-black)

![桌面晾绳把几张参考图并排挂在主屏菜单栏下方。图中为演示数据](outputs/pinstill-clothesline-mock.webp)

## 下载

下载 Universal DMG，把 Pinstill 拖进「应用程序」。支持 macOS 13 及以上的 Apple 芯片与 Intel Mac，安装包已签名并通过 Apple 公证。首次截图时在系统设置中允许「屏幕录制」。[下载 Mac 版](https://github.com/echoVic/Pinstill/releases/latest)

从 `v0.1.3` 起，Pinstill 会在后台检查新版，也可以从菜单栏图标选择「检查更新…」。默认由你确认后才下载安装。使用 `v0.1.2` 或更早版本时，用新版 DMG 替换一次应用即可。[维护者发版流程](docs/releasing.md)

## 截图与标注

1. 按 **⌘⇧1** 或从菜单栏选择「截取区域」。拖动框选，也可以单击高亮窗口。双击高亮窗口可直接复制截图。
2. 松手后出现标注工具栏，默认不预选工具。同一显示器内，拖动框内可移动整个选区，拖动四边或四角可改大小；已选标注工具时，按住 `⌥` 拖动框内仍能移动。松手后按新范围截取，原有标注会重置。区域截图也支持跨屏框选。[选区与标注演示](https://pinstill.echovic.com/#annotate)
3. 在工具栏复制、保存、贴图或晾图。默认也可按 `Enter` 或双击画布复制，按 `Esc` 取消。快捷键和截图后的默认动作可在偏好设置中修改。

- **冻结画面**　进入区域截图时冻结桌面。要截 Chrome 插件弹窗，先打开它，再按截图快捷键。菜单栏也提供「截取整个屏幕」。
- **放大、取色和测距**　框选时用方向键微调端点，按住 `Shift` 每次移动 10 点。按 `Z` 放大像素，读数出现后按 `C` 复制 HEX 色值。按 `M` 从光标位置开始测距，显示的单位为屏幕点（pt）。同一显示器内完成框选后，可用 `⌥` 加方向键移动选区，再加 `Shift` 调整宽高。[精确选区演示](https://pinstill.echovic.com/#precision)
- **编辑已有标注**　工具栏提供矩形、箭头、文字、马赛克、高亮、平铺水印等工具，默认标注色为红色，可从 40 色调色板选色或输入 HEX 色值。按 `V` 选择标注，可拖动、改色和调整线宽，双击文字或标签可编辑。用 `Delete` 删除，`⌘Z` 撤销，`⌘⇧Z` 重做。

## 贴图与晾绳

- **贴图**　点「贴图」打开独立的置顶窗口，按 **⌘⇧2** 可以把剪贴板图片贴出来。每张图可拖动、缩放、调透明度、旋转、翻转或开启鼠标穿透。从菜单栏打开「贴图管理…」可以分组、整组显隐和恢复点击。启动时恢复最多 60 张活动贴图，更早的可以在历史中恢复。[贴图管理演示](https://pinstill.echovic.com/#pin)
- **桌面晾绳**　点「晾图」把图片挂在主屏菜单栏下方。晾绳浮在普通窗口上方，多张图并排放置，可拖动换序。可以从截图工具栏、长图预览、历史、文件或剪贴板挂图。点选图片后可复制、保存、另开贴图或取下。用菜单栏的「显示 / 隐藏桌面晾绳」收起或打开，重启后会恢复图片、顺序和显隐状态。[桌面晾绳演示](https://pinstill.echovic.com/#clothesline)

## 文字提取与二维码

框选后点「提取文字」。默认识别中英文，保留换行，可复制选中文字或全文。在「偏好设置 → 识别与翻译」可以切换识别语言。

点「识别二维码」会逐条显示当前选区里的二维码内容。可以复制原文，或看过完整网址后点「在浏览器打开」，进入浏览器时会结束本次截图。两项功能都用 macOS Vision 在本机识别带标注的当前画面。按 `Esc` 关闭结果面板，可继续标注或保存。[识别功能演示](https://pinstill.echovic.com/#recognition)

![提取文字与识别二维码的结果面板。图中为演示数据](outputs/pinstill-recognition-mock.png)

## 滚动截图与历史

- **滚动截图**　在单块显示器内框选后点「滚动截图」，手动滚动到需要的位置，再点「立即完成」。授权辅助功能后，也可以在控制条开启自动滚动。如果画面接不上，Pinstill 会保留已拼接的部分，并提示往回滚一点。达到 120 帧、30,000 像素高度或 32 Mi 像素时自动完成。[长截图演示](https://pinstill.echovic.com/#scroll)
- **截图历史**　从菜单栏打开「历史与设置」。大图预览可以复制、保存、贴图、晾图或删除，保留天数和容量上限可在偏好设置中调整。

## 数据与权限

- 截图历史、贴图和晾绳图片保存在 `~/Library/Application Support/Pinstill/` 下的 `captures/`、`pins/` 和 `clothesline/`。当前选区和长图预览保存在内存中。
- 截图通过本地 `shot://localhost/...` 协议读取。图像处理与识别都在本机完成，应用检查和下载更新时连接 GitHub。
- 截图需要「屏幕录制」，自动滚动需要「辅助功能」。可在「偏好设置 → 权限」查看授权和登录启动状态。

## 开发

需要 Go 1.27+（以 `go.mod` 为准）、Node 22+ 和 npm。

```bash
npm ci
npm run build:web          # 构建前端到 dist/
npm run build              # 构建 macOS 应用
npm run dev                # 开发模式，Vite 热更新 + MyGo 窗口
```

修改 Go 服务或事件后，运行 `npm run generate` 更新前端绑定。开发时可用 `PINSTILL_DATA_DIR=/tmp/pinstill-data` 隔离历史和贴图数据。

CI 检查包含 Go 测试、Windows 交叉编译和前端构建。

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go vet ./...
GOOS=windows GOARCH=amd64 GOTOOLCHAIN=auto go build ./...
npm test
npm run typecheck
npm run build:web
```

macOS 应用级自检用 `bash work/e2e.sh`，已有本地构建时可加 `--skip-build`。运行前先退出 Pinstill。`go test -tags integration ./internal/capture` 验证真实截图后端，需要屏幕录制权限。

## 已知限制

- Windows 截图后端处于交叉编译和合成测试阶段，混合 DPI、负坐标和多显示器仍需真机验证。
- 截图翻译尚未接入。滚动截图对自绘滚动、动态表头和完全重复的内容可能失效。
- macOS 跨屏截图按所选显示器中的最高像素密度合成，显示器之间的空白保留为透明。滚动截图的选区需位于同一块屏幕内。
- 登录启动在未安装到 `/Applications` 的构建里会失败，设置页会如实显示。

## 参与贡献

欢迎 Issue 和 PR。提交前请保证 `gofmt -l .` 无输出、`go vet ./...` 和 `go test ./...` 通过；改前端或交互时跑一次 `bash work/e2e.sh --skip-build`。提交信息用 `feat: / fix: / docs: / perf: / test:` 前缀，说明「为什么」。

## 致谢与许可

桌面外壳 [MyGo](https://mygo.egoist.dev/)（Go + 系统 WebView），前端 Vite，图标 Lucide（ISC）。[MIT](LICENSE) © 2026 echovic。
