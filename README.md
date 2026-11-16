# Pinstill · 钉图

简体中文 | [English](README.en.md)

**截屏 → 就地标注 → 复制 / 贴图 / 保存 → 回到历史**，一条快捷键全部完成。
纯 Go + 系统 WebView，无 Electron、无 cgo；截图只在本机处理，不上传任何内容。

[![CI](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml/badge.svg)](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)
![macOS](https://img.shields.io/badge/platform-macOS%2013%2B-black)
![Windows](https://img.shields.io/badge/platform-Windows%20(cross--compiled)-lightgrey)

![选区就地标注](outputs/pinstill-overlay.jpg)

## 下载

从 [Releases](https://github.com/echoVic/Pinstill/releases) 下载 macOS 版，需要 macOS 13 或以上。Windows 截图后端（GDI）已经交叉编译通过，但尚未真机验证，暂不提供正式安装包。

## 使用

1. 启动后常驻菜单栏；按 **⌘⇧1**（或点托盘「截取区域」）进入选区。
2. 拖选区域，松手即完成截屏并浮出标注工具栏；标注后按 Enter 或双击画布复制，Esc 取消。
3. 拖选区四角可以调整范围，松手重新截取；点「滚动截图」进入长截图。
4. 点「贴图」把截图钉在桌面，点「保存」走系统保存对话框，点「复制」进剪贴板。
5. 托盘 →「历史与设置」查看历史、贴图和全部偏好。

快捷键和「截图后」的行为（标注 / 复制 / 贴图）都可以在设置里改。

## 功能

- **截图与标注**：区域截图、整屏截图，遮罩、十字线和尺寸角标；松手后截图直接显示在选区里，完整标注工具栏浮在旁边。矩形、圆、直线、箭头、画笔、文字、标签、马赛克、高亮和平铺水印，支持撤销、40 色调色板、HEX 输入和文字样式。
- **贴图**：始终置顶的无边框窗口，可以拖动、缩放、调透明度，右键有复制 / 保存 / 透明度 / 原始大小；多张各自摆放，重启后自动恢复（最多 60 张活动贴图，更早的归档）。
- **历史**：截图自动落盘，显示真实缩略图；大图预览支持复制、保存、贴图和删除，保留天数与容量上限生效。
- **滚动截图**：自动滚动抓帧拼接，达到 120 帧、30000px 高度或 32 Mi 像素时自动完成，也可以暂停、切手动或立即完成。需要辅助功能权限，未授权时降级为手动滚动。
- **托盘与快捷键**：菜单栏常驻；截取区域 **⌘⇧1**、固定剪贴板图片 **⌘⇧2**，可在设置里重新录制。
- **设置与权限**：7 类偏好实时生效；权限中心显示屏幕录制、辅助功能和登录启动的真实状态。

OCR、翻译和二维码识别仍是「待接入」占位，点开有说明，不会假装可用。

## 数据与权限

- 历史在 `~/Library/Application Support/Pinstill/captures/`，贴图在 `.../pins/`，当前会话缓存在 `~/Library/Caches/Pinstill/session/`。
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
- overlay 覆盖屏幕可见工作区，不能直接框选菜单栏和 Dock；多显示器与混合缩放未充分测试。
- 登录启动在未安装到 `/Applications` 的构建里会失败，设置页会如实显示。

## 参与贡献

欢迎 Issue 和 PR。提交前请保证 `gofmt -l .` 无输出、`go vet ./...` 和 `go test ./...` 通过；改前端或交互时跑一次 `bash work/e2e.sh --skip-build`。提交信息用 `feat: / fix: / docs: / perf: / test:` 前缀，说明「为什么」。

## 致谢与许可

桌面外壳 [MyGo](https://mygo.egoist.dev/)（Go + 系统 WebView），前端 Vite，图标 Lucide（ISC）。[MIT](LICENSE) © 2026 echovic。
