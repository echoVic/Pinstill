from pathlib import Path
p=Path('outputs/tupen-design.html')
s=p.read_text()
s=s.replace('</style>',Path('work/settings.css').read_text()+'</style>')
markup='<section class="settings-view" id="settingsView" hidden aria-label="偏好设置"><div class="settings-layout"><nav class="settings-nav" aria-label="设置分类" id="settingsNav"></nav><div class="settings-content"><div id="settingsBody"></div><div class="settings-footer"><span class="settings-status" id="settingsStatus" role="status">设置已保存在此浏览器</span><button id="resetSettings" type="button">恢复本分类默认值</button></div><p class="settings-preview-note">设计预览 · 设置保存在当前浏览器。开机启动、系统快捷键、文件夹访问及识别服务将在桌面应用中接入。</p></div></div></section><div class="workspace" id="workspaceView">'
s=s.replace('<div class="workspace">',markup)
s=s.replace('第一排放绘制与标注；第二排放识别、撤销和完成操作。每个入口直接可见。','默认只显示图标，悬停时显示名称。绘制、识别和完成操作都在手边。')
start=s.index("document.querySelectorAll('[data-view]').forEach(b=>b.addEventListener")
end=s.index("document.addEventListener('keydown'",start)
s=s[:start]+"document.querySelectorAll('[data-view]').forEach(b=>b.addEventListener('click',()=>showView(b.dataset.view)));"+s[end:]
start=s.index("el('shortcut').addEventListener")
end=s.index(chr(10),start)
s=s[:start]+"el('shortcut').addEventListener('click',()=>{settingsCategory='shortcuts';showView('settings')});"+s[end:]
s=s.replace("if(['INPUT','TEXTAREA'].includes(e.target.tagName))return;","if(!el('settingsView').hidden||['INPUT','TEXTAREA','SELECT','BUTTON'].includes(e.target.tagName))return;")
s=s.replace("if(e.key==='Enter')activate('clipboard')","if(e.key==='Enter'&&preferences.enterCopy)activate('clipboard')")
s=s.replace("a.download='tupen-设计预览.png';a.href=canvas.toDataURL('image/png');a.click();notify('已发起 PNG 下载，包含当前标注');","const format=preferences.format==='JPEG'?'jpeg':'png';a.download=(preferences.filename||'Tupen')+'-'+new Date().toLocaleDateString('sv-SE')+'.'+(format==='jpeg'?'jpg':'png');a.href=canvas.toDataURL('image/'+format,preferences.quality/100);a.click();notify('已发起 '+preferences.format+' 下载，包含当前标注');")
s=s.replace("ctx.globalAlpha=.35;ctx.font=","ctx.globalAlpha=preferences.watermarkOpacity/100;ctx.font=")
s=s.replace("current=id;toolbar.querySelectorAll","current=id;if(id==='watermark')entry=preferences.watermarkText;toolbar.querySelectorAll")
s=s.replace("activate('arrow');redraw();",Path('work/settings.js').read_text()+"activate(preferences.defaultTool);redraw();")
p.write_text(s)
Path('work/check-prototype.js').write_text(s.split('<script>')[1].split('</script>')[0])
