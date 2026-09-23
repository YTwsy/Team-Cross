# v0.2.3 正式版发布验证 · 2026-09-23

## 来源与主线

本版包含 `v0.2.2` 之后合入的资源库顺序、分享范围吸顶操作、整页阅读、多材料切换和批注排版修改。发布准备提交为 `02af144c921211d316ec49f88dd5691039b2ba4f`，包含版本入口、发行说明和本地开发构建默认版本。PR [#27](https://github.com/YTwsy/Team-Cross/pull/27) 的 [CI run 35819184703](https://github.com/YTwsy/Team-Cross/actions/runs/35819184703) 通过 Go test/vet、相关 race test、Web check/test/build、嵌入资源一致性、Homebrew 定义测试和 arm64 CLI 交叉编译；随后以 merge commit `95bc31f9b9b998f1f32c02dec0a305fa05b37b94` 合入 `main`。

Annotated tag `v0.2.3` 的 tag object 为 `625da8e87ad709695185213409d209b67f2a088a`，解引用后精确指向发布准备提交 `02af144...`。该提交是上述 `main` merge commit 的第二父提交，发布前已核对 `origin/main` 可达性；没有移动既有 tag。

## 干净候选与正式流水线

在干净的 `02af144...` checkout 本地执行 `make release VERSION=0.2.3`、`make verify-release VERSION=0.2.3` 和 `make verify-homebrew VERSION=0.2.3`，全部通过。候选 `release.json` 记录 `dirty=false`、`buildNumber=102`、arm64、macOS 14.0+、未使用 Developer ID 签名或 Apple 公证。安装检查包括 DMG 挂载、App 图标和 ad-hoc 签名完整性、CLI/App 版本、隔离生命周期、Formula/Cask 安装与升级互斥。候选 DMG 的 SHA-256 为 `e188b8579a378bfb67e72ee1be763e72b79930085c176db54adcc982bf6f94f2`；它是本地构建，不是公开 Release 资产，公开资产摘要见下表。

[Tag 发布流水线 35821598362](https://github.com/YTwsy/Team-Cross/actions/runs/35821598362) 在 GitHub 托管 `macos-15` arm64 runner 上全部成功。Build job `107054425367` 重新执行工程门槛、arm64 CLI/App/DMG 构建、安装与生命周期、隔离 Homebrew、manifest/checksum，并为 CLI 和 DMG 分别生成 provenance；Publish job `107055851501` 下载复核资产与发行说明后创建 GitHub Release；Homebrew job `107055899703` 完成后续公共渠道发布。

[GitHub Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.3) 于 `2026-09-23T05:21:37Z` 创建，为非 Draft、非 Pre-release；`releases/latest` 精确返回 `v0.2.3`。公开 `release.json` 记录 `version=0.2.3`、`commit=02af144...`、`buildNumber=102`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false`、`notarized=false`。

| 公开资产 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.2.3-darwin-arm64.tar.gz` | 8,249,840 | `52840365560fa3ab09ecfeea94ef23950ad11e9da62eca5dcb775912bfac23f9` |
| `Team-Cross-0.2.3-arm64.dmg` | 10,571,493 | `9a32b204139eb25d4ce068abee02e3a181c59dc065af67239202f9ec695b9dad` |
| `SHA256SUMS` | 195 | `5870b73b70fc4e5a1c578391c3860cf1be5093865bb28f6b74d699c7160da76e` |
| `release.json` | 462 | `3d09aac85ac5ae128215b84665ae00028abecfcfb1d199b529c1417148e12034` |

独立下载四份公开资产后，`shasum -a 256 -c SHA256SUMS` 对 CLI 和 DMG 均通过；`release.json`、GitHub asset digest 和稳定 Homebrew 元数据中的来源提交与摘要一致。在主机环境对两份资产分别执行 `gh attestation verify`，限定仓库 `YTwsy/Team-Cross`、workflow `.github/workflows/release-unsigned.yml`、source ref `refs/tags/v0.2.3`、source digest `02af144...` 并拒绝 self-hosted runner，均得到一份有效 attestation。受限沙箱内的本地 Sigstore verifier 无法初始化，主机环境重试通过；托管 Homebrew workflow 也独立完成相同来源证明复核。

保留的 workflow artifact 为 `Team-Cross-0.2.3-unsigned-release`，ID `10733962502`，记录的过期时间为 `2026-10-07T05:21:15Z`。

## 稳定 Homebrew 通道

Homebrew 发布从公开 Release 重新下载资产，复核 checksum、manifest 与两份 attestation，并通过公共资源的隔离 Formula/Cask 安装验证。随后创建 tap [PR #9](https://github.com/YTwsy/homebrew-teamcross/pull/9)；[PR CI run 35822221143](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35822221143) 的 `Verify public Formula and Cask` 通过，受保护自动合并生成 tap commit `85afa9089b435d72cad28267129eb014fa3e85a4`。[公共 main smoke run 35822340740](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35822340740) 通过后，发布流水线将 Homebrew 状态、PR 和 commit 写回 Release。

公开 `Versions/stable.json` 记录 `version=0.2.3`、tag `v0.2.3`、来源提交 `02af144...`，CLI/DMG URL 和 SHA 与 Release 一致；稳定 Formula 为 `Formula/teamcross.rb`，Cask 为 `Casks/team-cross.rb`。Release 正文包含经公共安装 smoke 验证的 `brew install YTwsy/teamcross/teamcross` 与 `brew install --cask YTwsy/teamcross/team-cross`。

## 签名状态与未覆盖范围

本版只提供 arm64、macOS 14.0+ 产物，使用 ad-hoc 签名，未使用 Developer ID 签名或 Apple 公证。Release、provenance 与 Homebrew 安装成功不代表 Gatekeeper 首次批准。

本次发布没有新增真实模型、两台 Mac LAN、跨网络 Tailcat 或持续 DERP 验收。菜单栏热键呼出和固定浮窗的真实桌面操作仍未单独完成电脑控制验收；Claude Code 原生 TUI 与 Tailcat 继续保持实验性边界。
