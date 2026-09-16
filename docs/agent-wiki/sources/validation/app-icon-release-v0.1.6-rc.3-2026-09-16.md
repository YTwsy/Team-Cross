# v0.1.6-rc.3 App 图标与发布验证 · 2026-09-16

## 范围与来源

本轮只把 macOS App 图标的居中、主体占比和旧式 ICNS 打包兼容性修复发布为 `v0.1.6-rc.3`。WebGUI 图标和菜单栏状态图标是独立资源，本轮没有修改。图标内容提交 `56b827e248c554468972c906f9550b5aa70c7466` 只替换 `apps/macos/Assets/AppIcon.png`；打包提交 `00d91a87343a29a50697b0bf83a7aff8d059a2ba` 只修改 App 图标源、`Info.plist`、release 构建脚本与对应安装验证。

[PR #13](https://github.com/YTwsy/Team-Cross/pull/13) 将候选分支以 merge commit `e796bbec3c1c0cc00dfb05b0c53d7b95449dd35d` 合入 `main`。release 准备提交为 `fd493db1f8efaa71787ea706d5d6788fa89c70ff`；annotated tag `v0.1.6-rc.3` 的 tag object 为 `2cb27ef1b2dbcb7a58d0c3df9778710f093504a4`，解引用后精确指向上述 `main` merge commit。本文是发布完成后的证据记录，不反向改写已经发布的 tag 或产物。

## 图标资源与打包

发布源图为 1024×1024 RGBA PNG，并显式嵌入 `sRGB IEC61966-2.1` profile。主体按用户所指的完整非透明区域复核：含抗锯齿边缘的包围盒为 `(98, 103)–(927, 922)`，即宽 830、高 820；左右透明边距为 98/96 像素，上下为 103/101 像素。该位置与缩小后的视觉中心一致，边缘覆盖差异控制在 2 像素内，没有继续放大主体。

为避免 macOS 26 对接近透明或接近不透明的旧图标底板做不稳定分割，后续规范化只把源图中 alpha 250–254 的底板像素提升为 255，RGB、包围盒和主体位置均不变；完全不透明像素由 663 增至 631,435。透明外缘和抗锯齿边缘继续保留。Apple 对 macOS 26 旧图标的说明指出，系统会通过分割自动转换 legacy icon；因此这项调整针对的是系统识别输入，而不是重绘蓝色图形。[Apple 的 App 图标设计说明](https://developer.apple.com/videos/play/meet-with-apple/208/) 与 [Xcode App icon 配置文档](https://developer.apple.com/documentation/xcode/configuring-your-app-icon) 只支持这一机制判断，不等价于实际 macOS 26 显示验收。

打包方式参照本机已在 macOS 14/26 正常显示的 OpenSurge App：继续使用传统 ICNS，同时让 `CFBundleIconFile` 保存无扩展名资源基名 `TeamCross`。构建从同一源图生成 16、32、64、128、256、512、1024 像素对应的 10 个标准 ICNS rendition，并在调用 `iconutil` 前清除临时文件扩展属性。Apple 的 [Core Foundation keys 文档](https://developer.apple.com/library/archive/documentation/General/Reference/InfoPlistKeyReference/Articles/CoreFoundationKeys.html) 明确允许 `CFBundleIconFile` 省略扩展名。

## 本地与 CI 门槛

发布前在干净 release worktree 完成：

- `go test ./...` 与 `go vet ./...`；
- `collab`、`mcp`、`sharing`、`nativecodex`、`nativeclaude`、`service`、`cliinstall` 的 race test；
- Web check、50 项交互测试、production build 与 `internal/webassets/dist` 一致性；
- `python3 -m unittest scripts/test_homebrew_release.py` 的 9 项测试；
- `verify-release.py` 与 `verify-homebrew.py` 的干净本地 release preflight。

本地 preflight 的临时 `buildNumber=53` 只证明合并前候选的构建与安装链，不是最终公开产物编号。PR [CI run `35057082906`](https://github.com/YTwsy/Team-Cross/actions/runs/35057082906) 对 `fd493db...` 成功；合并后的 [main run `35057212680`](https://github.com/YTwsy/Team-Cross/actions/runs/35057212680) 对 `e796bbec...` 再次成功。

## tag 构建与公开 Release

[tag run `35057431793`](https://github.com/YTwsy/Team-Cross/actions/runs/35057431793) 从 `refs/tags/v0.1.6-rc.3` 运行并全部成功：

- `Build and verify arm64 release` job `104670428876` 在 4 分 18 秒内完成工程门槛、arm64 CLI/App/DMG 构建、App/CLI 生命周期、隔离 Homebrew、manifest、SHA-256 与两份 provenance；
- `Publish GitHub release` job `104671238596` 在 15 秒内复核下载产物和发行说明并创建 Pre-release；
- `Publish and verify Homebrew channel` job `104671292746` 在 4 分 33 秒内完成公开资产复核、tap PR、受保护合并、公共 tap smoke 和 Release 回写。

保存的 workflow artifact `Team-Cross-0.1.6-rc.3-unsigned-release` ID 为 `10431271936`，GitHub 记录大小为 17,720,838 字节，过期时间为 `2026-09-30T04:58:53Z`。[GitHub Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.1.6-rc.3) 名称为 `Team Cross v0.1.6-rc.3`，状态为非 Draft、Pre-release，发布时间为 `2026-09-16T04:59:18Z`。

公开资产为：

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.1.6-rc.3-darwin-arm64.tar.gz` | 7,808,883 | `5f6278195f9fb170feb9bea264d49aa032834064515e57c1fa65f8875943c98a` |
| `Team-Cross-0.1.6-rc.3-arm64.dmg` | 10,027,765 | `14e4e15b9ba7b14b7012c686434dee3ac907ad43869c1dfd1475c0f35b441039` |
| `SHA256SUMS` | 205 | `eb205daf40de73f3cb8503a52f570692b28039339b1e38cdd0cc450db9f6ec4a` |
| `release.json` | 476 | `f8b5790dec3e3711fd627fd88ffeb7491378d935abf62ec2945640f0c6840621` |

公开 `release.json` 记录 `version=0.1.6-rc.3`、`commit=e796bbec...`、`buildNumber=54`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false` 和 `notarized=false`。

## 下载后复核与 Homebrew

四个资产重新下载到新的临时目录后，`shasum -a 256 -c SHA256SUMS` 对 CLI tar 和 DMG 均返回 `OK`，两个元数据文件的摘要也与 GitHub asset digest 一致。`gh attestation verify` 对 CLI 和 DMG 均以退出码 0 通过，并严格约束仓库 `YTwsy/Team-Cross`、signer workflow `.github/workflows/release-unsigned.yml`、source ref `refs/tags/v0.1.6-rc.3`、source digest `e796bbec...`，同时拒绝 self-hosted runner。

Homebrew 发布使用 [tap PR #3](https://github.com/YTwsy/homebrew-teamcross/pull/3)：App bot 提交为 `40b79f6e06ccb1eca8f4b6c07fb77cf04816d727`，[PR check `35057828186`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35057828186) 成功后，以 merge commit `d3d8df2581f9a585a889beac81fd306cc7b02e5d` 合入。合并后的 [public tap smoke `35057931907`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35057931907) 再次从公开 URL 验证 RC Formula/Cask 安装。

公共 `Versions/rc.json` 记录 `0.1.6-rc.3`、tag、来源 `e796bbec...`、上述 CLI/DMG 摘要、`Formula/teamcross-rc.rb`、`Casks/team-cross@rc.rb` 以及 unsigned/unnotarized 边界。Release 正文已写入两个安装入口：

```sh
brew install YTwsy/teamcross/teamcross-rc
brew install --cask YTwsy/teamcross/team-cross@rc
```

## 结论与未覆盖范围

本轮证明 `v0.1.6-rc.3` 对应的居中缩放图标、明确不透明底板、传统 ICNS 打包、公开 arm64 Release、来源证明与受保护 Homebrew RC 发布链已经闭环。它没有改动 WebGUI 图标或菜单栏状态图标。

本轮没有在真实 macOS 26 Finder、Dock、Launchpad 或系统设置中安装并目视比较最终 DMG；GitHub `macos-15` 与本机 macOS 14 的构建、安装和 App 生命周期通过，不能替代 macOS 26 图标渲染验收。仍未覆盖 Developer ID 签名、Apple 公证、下载隔离后的 Gatekeeper 首次批准、Intel、真实模型、两台 Mac LAN、跨网络 Tailcat 或强制持续 DERP。这些边界没有因 Release、provenance 或 Homebrew 成功而扩大。
