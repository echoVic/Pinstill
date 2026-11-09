from pathlib import Path
p=Path('outputs/tupen-design.html')
s=p.read_text()
css='''
/* Capture editor: full, grouped toolbar. */
.workspace{grid-template-columns:minmax(0,1fr) 210px}.main{max-width:1560px}.top{align-items:center}.title{font-size:28px;letter-spacing:-.035em}.eyebrow{display:none}.sub{font-size:13px}.screen{display:block;padding:26px 22px 24px}.stage{min-height:640px}.stage::after{display:none}.stage-head .stage-title{font-weight:500}.rack{height:72px}.capture-editor{max-width:920px;margin:auto}.capture-info{display:flex;justify-content:space-between;gap:10px;font-size:12px;color:#b8b1a5;margin:0 0 10px}.capture-frame{position:relative;outline:1px solid #f3a24e;background:#f4f1eb}.capture-frame:before,.capture-frame:after{content:'';position:absolute;width:7px;height:7px;background:#f3a24e;z-index:1}.capture-frame:before{left:-4px;top:-4px}.capture-frame:after{right:-4px;bottom:-4px}#shotCanvas{display:block;width:100%;height:auto;touch-action:none;cursor:crosshair}.capture-toolbar{position:relative;left:auto;bottom:auto;transform:none;display:block;padding:8px;margin:14px 0 0;border-radius:12px;background:#faf7f0;color:#36332c;box-shadow:0 12px 28px #0003;z-index:3}.capture-toolbar.hidden{display:none}.toolbar-group{display:flex;gap:2px;flex-wrap:wrap;align-items:center}.toolbar-group+.toolbar-group{border-top:1px solid #ded8cd;margin-top:6px;padding-top:7px}.capture-toolbar button{display:flex;flex:1 0 43px;min-width:43px;flex-direction:column;align-items:center;gap:5px;padding:8px 5px;font-size:11px;font-weight:400;border-radius:7px;line-height:1.25;white-space:nowrap}.capture-toolbar svg{width:19px;height:19px;stroke-width:1.6;flex-shrink:0}.capture-toolbar button:hover{background:#eae4d8;color:#27241e}.capture-toolbar button.active{background:#30362f;color:#fffaf0}.capture-toolbar button:disabled{opacity:.35;cursor:default}.capture-toolbar button.copy-action{background:#e8ac64;color:#2f200e;flex-grow:1.4}.capture-toolbar button.cancel-action{color:#a64136}.capture-toolbar button[aria-pressed=true]{background:#30362f;color:#fffaf0}.tool-properties{min-height:46px;display:flex;align-items:center;flex-wrap:wrap;gap:10px;padding:12px 2px 2px;font-size:12px;color:#d6d0c5}.property-name{min-width:42px;color:#f3b16d}.color-swatch{width:19px;height:19px;min-width:19px;padding:0;border:2px solid transparent;border-radius:50%;background:var(--swatch);box-shadow:inset 0 0 0 1px #ffffff30}.color-swatch.selected{outline:1px solid #e9dfd0;outline-offset:2px}.property-sep{width:1px;height:18px;background:#ffffff24;margin:0 4px}.tool-properties input[type=range]{width:75px;accent-color:#e8ac64}.tool-properties input[type=text]{background:#302e28;border:1px solid #5a554a;border-radius:5px;color:#f8f3e9;padding:5px 8px;max-width:180px;width:130px;font:inherit}.tool-properties label{display:flex;align-items:center;gap:5px}.tool-hint{margin-left:auto;color:#b6afa2;font-size:11px}.result-panel{margin:12px 0 0;padding:16px;background:#292820;border-radius:9px;font-size:13px;line-height:1.7;color:#e0dacd}.result-panel[hidden],.pinned-preview[hidden]{display:none}.result-panel strong{font-size:12px;color:#efb576;display:block;font-weight:500;margin-bottom:5px}.pinned-preview{position:absolute;right:18px;top:165px;width:210px;background:#f6f2e8;box-shadow:0 18px 40px #0008;z-index:6;border-radius:6px;overflow:hidden;resize:both}.pinned-preview header{display:flex;align-items:center;justify-content:space-between;padding:6px 10px;color:#38372f;font-size:11px}.pinned-preview button{border:0;background:none;color:#48463d;font-size:15px}.pinned-preview img{display:block;width:100%}.editor-status{color:#a9bc9d;font-size:11px;min-height:18px;display:block;margin-top:10px}.toolbar-note{font-size:12px;line-height:1.75;color:#c3bcae}.top .actions{flex-shrink:0}.btn{white-space:nowrap}.side-title{font-size:12px;color:#b8b1a5}.meta-row span:first-child{color:#b8b1a5}.history.show{inset:54px 0 0}.history-grid{grid-template-columns:repeat(3,minmax(0,1fr))}:focus-visible{outline:2px solid #e8ac64;outline-offset:3px} .toast{pointer-events:none;max-width:calc(100vw - 32px)}
@media(max-width:1250px){.workspace{grid-template-columns:minmax(0,1fr)}.workspace>aside{display:none}.main{padding:24px}.app{grid-template-columns:170px 1fr}.rail{padding:24px 14px}.sub{max-width:430px}}
@media(max-width:900px){.app{grid-template-columns:1fr}.rail{padding:14px 18px}.brand{margin:0 0 12px}.top{gap:16px}.main{padding:22px 16px}.screen{padding:22px 14px}.stage{min-height:0}.capture-toolbar{padding:6px}.capture-toolbar button{flex-basis:44px;padding:8px 4px}.rack{margin:0 14px}.tool-hint{width:100%;margin:0}.toolbar-group+.toolbar-group{padding-top:6px}.title{font-size:25px}.pinned-preview{width:170px}}
@media(max-width:520px){.top{align-items:start;flex-direction:column}.capture-toolbar button{flex-basis:44px}.capture-info{font-size:11px}.capture-toolbar .toolbar-group{gap:3px}.rack-label{max-width:100px}.pin{width:55px;height:42px}.stage-head{padding:15px}.top .actions{align-self:stretch;justify-content:space-between}.stage-meta{max-width:130px}.tool-properties{gap:9px}.tool-properties input[type=range]{width:55px}}
'''
s=s.replace('</style>',css+'\n</style>')
a=s.index('        <div class="screen" id="rackView">'); b=s.index('        <div class="history"',a)
s=s[:a]+'''        <div class="screen" id="rackView">
          <div class="capture-editor">
            <div class="capture-info"><span>截取完成 · 可继续标注</span><span id="captureSize">960 × 480 px</span></div>
            <div class="capture-frame"><canvas id="shotCanvas" width="960" height="480" aria-label="截图标注预览，可拖动绘制图形"></canvas></div>
            <div class="capture-toolbar" id="captureToolbar" role="toolbar" aria-label="截图后工具栏"></div>
            <div class="tool-properties" id="toolProperties"></div>
            <div class="result-panel" id="resultPanel" hidden aria-live="polite"></div>
            <span class="editor-status" id="editorStatus" role="status">设计预览 · 可切换工具，在截图上拖动试画</span>
          </div>
        </div>
        <div class="pinned-preview" id="pinnedPreview" hidden><header>固定到屏幕 · 预览<button id="unpin" aria-label="关闭贴图">×</button></header><img id="pinnedImage" alt="固定截图预览"></div>
''' +s[b:]
s=s.replace('顶部截图架</div><div class="stage-meta">最近更新 · 刚刚','截图与标注</div><div class="stage-meta">全部工具 · 直接操作')
s=s.replace('<div class="rack-label">拖到任意应用即可使用','<div class="rack-label">截图架 · 暂存待用')
s=s.replace('把截图挂起来，<br>接着用。','截取、标注，再带走。')
s=s.replace('一个安静的截图工作台：截完先收好，需要时拖走。适合文档、设计和每天都在复制粘贴的人。','截图后就地编辑，一步复制、下载或固定到屏幕。')
a=s.index('      <aside>\n',s.index('<div class="workspace">')); b=s.index('      </aside>',a)
s=s[:a]+'''      <aside>
        <div class="side-block"><div class="side-title">随截随用</div><div class="toolbar-note">松开鼠标后，工具栏出现在选区下方。靠近屏幕底部时，移到选区上方。</div></div>
        <div class="side-block"><div class="side-title">画好再带走</div><div class="toolbar-note">第一排放绘制与标注；第二排放识别、撤销和完成操作。每个入口直接可见。</div></div>
        <div class="side-block"><div class="side-title">常用按键</div><div class="meta-row"><span>撤销</span><span>⌘ / Ctrl Z</span></div><div class="meta-row"><span>保存到剪贴板</span><span>Enter</span></div><div class="meta-row"><span>取消截图</span><span>Esc</span></div></div>
        <div class="side-block"><div class="side-title">设计预览</div><div class="toolbar-note">可试画、撤销与下载。翻译、文字及二维码识别、滚动截图和固定到屏幕展示交互状态。</div></div>
''' +s[b:]
a=s.index('<script>');s=s[:a]+'''<script src="toolbar.js"></script>
</body>
</html>
'''
p.write_text(s)
