# Tupen

Tupen 是一个面向 macOS 和 Windows 的桌面截图与标注工具原型。

它把截图、标注、OCR、翻译、二维码识别、滚动截图、固定到屏幕和剪贴板保存放进同一条工作流。当前仓库已开始接入 MyGo：设计稿作为 Vite 前端运行在桌面窗口中，后续按平台后端接入真实截图、剪贴板和权限能力。

## 当前预览

- 设计稿：[outputs/tupen-design.html](outputs/tupen-design.html)
- MyGo 前端入口：[frontend/index.html](frontend/index.html)
- Go 桌面入口：[cmd/tupen/main.go](cmd/tupen/main.go)
- 紧凑工具栏截图：[outputs/tupen-toolbar-compact.png](outputs/tupen-toolbar-compact.png)
- 设置页截图：[outputs/tupen-settings.png](outputs/tupen-settings.png)

本地预览可以在仓库根目录运行任意静态文件服务器，例如：

```bash
python3 -m http.server 8765
```

然后打开 `http://127.0.0.1:8765/outputs/tupen-design.html`。

## 开发

```bash
npm install
npm run build:web
GOTOOLCHAIN=auto go test ./...
```

需要启动桌面开发环境时运行 `npm run dev`。它会启动 Vite，并由 MyGo 打开桌面窗口。

## 技术方向

- 桌面框架：MyGo（https://mygo.egoist.dev/）
- 图标：Lucide（ISC，社区开源图标库）
- 目标平台：macOS、Windows
- 当前阶段：交互原型与产品结构验证
