# Tupen

Tupen 是一个面向 macOS 和 Windows 的桌面截图与标注工具原型。

它把截图、标注、OCR、翻译、二维码识别、滚动截图、固定到屏幕和剪贴板保存放进同一条工作流。当前仓库先承载交互设计稿，后续按 MyGo 生态推进跨平台桌面实现。

## 当前预览

- 设计稿：[outputs/tupen-design.html](outputs/tupen-design.html)
- 紧凑工具栏截图：[outputs/tupen-toolbar-compact.png](outputs/tupen-toolbar-compact.png)
- 设置页截图：[outputs/tupen-settings.png](outputs/tupen-settings.png)

本地预览可以在仓库根目录运行任意静态文件服务器，例如：

```bash
python3 -m http.server 8765
```

然后打开 `http://127.0.0.1:8765/outputs/tupen-design.html`。

## 技术方向

- 桌面框架：MyGo（https://mygo.egoist.dev/）
- 目标平台：macOS、Windows
- 当前阶段：交互原型与产品结构验证
