# 发布 macOS 签名更新

安装包和更新一起发布到 [GitHub Releases](https://github.com/echoVic/Pinstill/releases)。Pinstill 使用 MyGo 的 Ed25519 更新签名校验，以及 macOS Developer ID 签名和公证；两种签名用途不同，发布时均需配置。

1. 修改 `mygo.config.ts` 中的版本号，把 `CHANGELOG.md` 的待发布条目放在对应的版本标题下，例如 `## 0.1.3`。应用从打包元数据读取版本号，无需在 Go 代码中更新。
2. 在维护机上配置 `MACOS_SIGNING_IDENTITY`（Developer ID Application）和 `MACOS_NOTARY_PROFILE`（已保存的 `notarytool` 凭据）。更新签名私钥保存在仓库之外；请用密码管理器备份，并使用同一把私钥为后续版本签名。丢失或更换私钥会导致已安装版本拒绝更新。
3. 从仓库根目录构建 Universal 安装包和更新包：

   ```bash
   : "${MACOS_SIGNING_IDENTITY:?请配置 Developer ID Application}"
   : "${MACOS_NOTARY_PROFILE:?请配置 notarytool 凭据}"
   key_file="$HOME/Library/Application Support/mygo/update-keys/pinstill/mygo-update.key"
   test -s "$key_file" && \
     env MACOS_SIGNING_IDENTITY="$MACOS_SIGNING_IDENTITY" \
       MACOS_NOTARY_PROFILE="$MACOS_NOTARY_PROFILE" \
       MYGO_UPDATER_PRIVATE_KEY="$(cat "$key_file")" \
       npm run build -- -platform darwin/universal
   ```

   没有更新签名私钥时，普通本地构建仍可打包，但不会生成签名更新包。

4. 确认 `build/darwin-universal/` 中的 `.dmg`、`pinstill-<版本>-darwin-universal.tar.gz`、`update-darwin-universal.json` 和清单引用的增量包（若有）齐全。对 `.app` 运行 `codesign --verify --deep --strict`、`spctl -a -vv -t exec`，并用 `xcrun stapler validate` 检查安装包公证票据。
5. 将这些文件按原名上传到同一个 `v<版本>` 草稿 Release，确认清单引用的所有文件都已上传，再发布为最新稳定版。不要只上传 `.dmg`，否则已安装的应用找不到更新。Universal 构建读取 `update-darwin-universal.json`，不要用单架构清单替代。

`v0.1.2` 及更早版本不含更新器。用户需先手动安装首个启用更新的版本。发布后应使用此前已启用更新的版本，实际验证「检查更新… → 安装并重启 → 版本号变更」，并确认历史和贴图保留。

## 更新品牌图标

应用和开发版图标从 `frontend/src/brand.svg` 导出，菜单栏使用单独的黑色透明模板：

```bash
rsvg-convert frontend/src/brand.svg -o resources/icon.png
cp resources/icon.png resources/icon-dev.png
rsvg-convert internal/app/tray-icon.svg -o internal/app/tray-icon.png
```

将 `resources/icon.png` 同步到 `echovic-apps/assets/pinstill/icon.png`，在官网仓库运行 `python3 tools/make_images.py pinstill`，更新 favicon、触屏图标和双语分享图。
