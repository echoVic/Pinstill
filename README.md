# Pinstill · 钉图

简体中文 | [English](README.en.md)

**截屏 → 就地标注 → 复制 / 贴图 / 晾图 / 保存**，一条快捷键全部完成。
纯 Go + 系统 WebView，无 Electron、无 cgo；截图只在本机处理，不上传任何内容。

[![CI](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml/badge.svg)](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)
![macOS](https://img.shields.io/badge/platform-macOS%2013%2B-black)
![Windows](https://img.shields.io/badge/platform-Windows%20(cross--compiled)-lightgrey)

![选区就地标注](outputs/pinstill-overlay.jpg)

## 下载

从 [Releases](https://github.com/echoVic/Pinstill/releases) 下载 macOS 版，需要 macOS 13 或以上。Windows 截图后端（GDI）已经交叉编译通过，但尚未真机验证，暂不提供正式安装包。

支持自动更新的版本会在后台检查新版，也可以从托盘或 Pinstill 菜单选择「检查更新…」。默认由你确认后才下载安装；`v0.1.2` 及更早版本需手动升级一次。[维护者发版流程](docs/releasing.md)

## 使用

1. 启动后常驻菜单栏；按 **⌘⇧1**（或点托盘「截取区域」）进入选区。
2. 拖选区域，松手即完成截屏并浮出标注工具栏；标注后按 Enter 或双击画布复制，Esc 取消。
3. 拖动选区的边或角可以调整范围，松手更新截图。点「滚动截图」进入长截图。
4. 点「贴图」创建置顶窗口，点「晾图」把截图挂到桌面晾绳。复制或保存后可在右下角的截图卡片上继续操作。
5. 菜单栏的「贴图管理…」可以整理置顶贴图，「显示 / 隐藏桌面晾绳」可以收起或打开晾绳，「历史与设置」可以查看历史和偏好。

快捷键和「截图后」的行为（标注 / 复制 / 贴图）都可以在设置里改。

框选时用方向键微调选区端点，按住 Shift 每次移动 10 点。按 Z 打开放大镜，读数出现后按 C 复制颜色，按 M 从当前光标位置开始测距。截图后按 ⌥ + 方向键移动选区，加 Shift 调整宽高。跨屏选区可在框选时微调。

标注时按 V 进入「选择标注」。拖动已有标注可调整位置，双击文字或标签可修改内容，选中后也可改颜色或按 Delete 删除。用 ⌘Z 撤销，⌘⇧Z 重做。

## 功能

- **截图与标注**支持区域截图、整屏截图、遮罩、十字线和尺寸角标。macOS 可以跨显示器框选和标注，跨屏截图会缩放预览。标注支持矩形、圆、直线、箭头、画笔、文字、标签、马赛克、高亮和平铺水印，提供 40 色调色板、HEX 输入和文字样式。
- **截图卡片**在区域截图、整屏截图或长截图复制或保存后出现，提供复制、保存、贴图和晾图。悬停时保留卡片，离开后约 6 秒收起。
- **贴图**可拖动、缩放、调透明度、旋转和翻转。用「贴图管理…」分组、显示或隐藏一组贴图，也可以恢复穿透贴图的鼠标点击。贴图设置会随图片恢复，最多 60 张活动贴图，更早的归档。
- **桌面晾绳**在 macOS 主屏菜单栏下方居中悬挂图片，显示时浮在普通窗口上方。截图、历史和长图预览都有「晾图」，也可以从文件或剪贴板挂图。拖动卡片可排序，点选卡片可复制、保存、创建置顶贴图或取下。图片、顺序和可见状态会随应用重启恢复。
- **历史**显示本地缩略图。大图预览支持复制、保存、贴图、晾图和删除，保留天数与容量上限生效。
- **滚动截图**进入后先手动滚动，截好后点「立即完成」。也可在控制条开启「自动滚动」（需辅助功能权限）。画面接不上时提示往回滚一点，并保留已拼接的部分。达到 120 帧、30000px 高度或 32 Mi 像素时自动完成。
- **托盘与快捷键**：菜单栏常驻；截取区域 **⌘⇧1**、固定剪贴板图片 **⌘⇧2**，可在设置里重新录制。
- **设置与权限**：7 类偏好实时生效；权限中心显示屏幕录制、辅助功能和登录启动的真实状态。

OCR、翻译和二维码识别仍是「待接入」占位，点开有说明，不会假装可用。

## 数据与权限

- 历史在 `~/Library/Application Support/Pinstill/captures/`，贴图在 `.../pins/`，晾图在 `.../clothesline/`，当前会话缓存在 `~/Library/Caches/Pinstill/session/`。
- 页面通过 `shot://localhost/...` 读取这些图片，不经过网络，也不上传任何内容。
- 首次截图需要「屏幕录制」权限；滚动截图的自动滚动需要「辅助功能」权限，未授权时自动降级为手动。
- 开发或自检时用 `PINSTILL_DATA_DIR=/tmp/pinstill-data` 隔离数据目录，不会读到真实历史。

## 开发

需要 Go 1.27+（以 `go.mod` 为准）、Node 22+ 和 npm。

```bash
npm install
npm run build:web          # 构建前端到 dist/
npm run build              # 或 ./node_modules/.bin/mygo build
npm run dev                # 开发模式，Vite 热更新 + MyGo 窗口
```

检查（与 CI 一致）：

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go vet ./...
npm test
npm run typecheck
bash work/e2e.sh --skip-build   # 24 项应用级自检；去掉 --skip-build 会先完整构建
```

`go test -tags integration ./internal/capture` 会跑真实截图后端，需要屏幕录制权限。

## 已知限制

- Windows 只有交叉编译和合成测试，混合 DPI、负坐标和多显示器仍需真机验收。
- OCR、翻译、二维码尚未接入；滚动截图对自绘滚动、动态表头和完全重复的内容可能失效。
- macOS 跨屏截图按所选显示器中的最高像素密度合成，显示器之间的空白保留为透明。滚动截图的选区需位于同一块屏幕内。
- 登录启动在未安装到 `/Applications` 的构建里会失败，设置页会如实显示。

## 参与贡献

欢迎 Issue 和 PR。提交前请保证 `gofmt -l .` 无输出、`go vet ./...` 和 `go test ./...` 通过；改前端或交互时跑一次 `bash work/e2e.sh --skip-build`。提交信息用 `feat: / fix: / docs: / perf: / test:` 前缀，说明「为什么」。

## 致谢与许可

桌面外壳 [MyGo](https://mygo.egoist.dev/)（Go + 系统 WebView），前端 Vite，图标 Lucide（ISC）。[MIT](LICENSE) © 2026 echovic。
