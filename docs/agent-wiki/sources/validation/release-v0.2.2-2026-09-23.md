# v0.2.2 正式版发布验证 · 2026-09-23

## 来源与主线

发布提交为 `1f66f499b200050a7e20db968a15e16aaf3bbe56`，包含材料卡片内阅读、协作阅读布局、邀请入口整理，以及 `docs/releases/v0.2.2.md` 和正式下载入口。PR [#24](https://github.com/YTwsy/Team-Cross/pull/24) 的 CI run [35751832874](https://github.com/YTwsy/Team-Cross/actions/runs/35751832874) 通过 Go test/vet、并发回归、Web check/test/build、嵌入资源一致性、Homebrew 定义测试与 arm64 CLI 交叉编译；随后以 merge commit `e5b74e73319182d9674629b0031dd7e2d7a1695e` 合入 `main`。

annotated tag `v0.2.2` 的 tag object 为 `f4e1ed8f8b612b0c4b7107a98db1b98294ce0bbd`，解引用后精确指向发布提交 `1f66f49...`。该提交是上述 `main` merge commit 的第二父提交，因此通过 `origin/main` 可达性检查；没有移动或替换既有 tag。

## tag 构建与 GitHub Release

发布流水线 [run 35752556975](https://github.com/YTwsy/Team-Cross/actions/runs/35752556975) 在 GitHub 托管 `macos-15` arm64 runner 上成功完成。Build job `106830142491` 用时 5 分 27 秒，工程门槛、release 构建、安装与生命周期、隔离 Homebrew、manifest/checksum，以及 CLI 与 DMG 两份 attestation 均通过。Publish job `106832282254` 验证下载产物和发行说明后创建 Release。

[GitHub Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.2) 于 `2026-09-22T16:18:14Z` 创建，为非 Draft、非 Pre-release、Latest。公开 `release.json` 记录 `version=0.2.2`、`commit=1f66f49...`、`buildNumber=94`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false`、`notarized=false`。

| 资产 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.2.2-darwin-arm64.tar.gz` | 8,244,552 | `82f665a9f7ee64f6300c25e3262408cdb80ae9fa229059a19076f871c92910c4` |
| `Team-Cross-0.2.2-arm64.dmg` | 10,569,109 | `6964f7590b57b9df4acbca7aea6bfe4256f7a4b0c073b0dbc603d4bcfa789f2d` |
| `SHA256SUMS` | 195 | `1951afb2a72f1ab537e498577592458cda089d25482b75eb5b65a528d9733970` |
| `release.json` | 461 | `db16cc971b750724e0e68feedab55a2052d8b1111c9f114fbb2c3122c7e5c44e` |

`SHA256SUMS`、公开 `release.json`、GitHub asset digest 和稳定 Homebrew 元数据中的 CLI/DMG 摘要一致。Homebrew job 对两份资产执行 `gh attestation verify`，限定仓库 `YTwsy/Team-Cross`、workflow `.github/workflows/release-unsigned.yml`、source ref `refs/tags/v0.2.2` 和 source digest `1f66f49...`，并拒绝 self-hosted runner。

保留的 workflow artifact 为 `Team-Cross-0.2.2-unsigned-release`，ID `10707051465`，大小 18,687,645 字节，记录的过期时间为 `2026-10-06T16:17:51Z`。

## 稳定 Homebrew 通道

正式流水线先从公开 Release 重新下载资产，复核 checksum、manifest、两份 attestation，并通过发布前公共 Homebrew 安装验证。随后创建 tap [PR #8](https://github.com/YTwsy/homebrew-teamcross/pull/8)；[PR CI run 35753360460](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35753360460) 的 `Verify public Formula and Cask` 通过，受保护自动 squash 合并生成 tap commit `be7c73037dbba9af3f870b7c345e13ff5989ed32`。[公共 main smoke run 35753568377](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35753568377) 随后通过，release workflow 再将 Homebrew 状态、PR 与 commit 写回 Release。

公开 `Versions/stable.json` 记录 `version=0.2.2`、tag `v0.2.2`、来源提交 `1f66f49...`，CLI/DMG URL 和 SHA 与 Release 一致；Formula 为 `Formula/teamcross.rb`，Cask 为 `Casks/team-cross.rb`。Release 正文包含经公共安装 smoke 验证的 `brew install YTwsy/teamcross/teamcross` 与 `brew install --cask YTwsy/teamcross/team-cross`。

## 签名状态与未覆盖范围

本版只提供 arm64、macOS 14.0+ 产物，使用 ad-hoc 签名，未使用 Developer ID 签名或 Apple 公证。Release、provenance 与 Homebrew 安装成功不代表 Gatekeeper 首次批准。

本次发布没有新增真实模型、两台 Mac LAN、跨网络 Tailcat 或持续 DERP 验收。菜单栏热键呼出和固定浮窗的真实桌面操作仍未单独完成电脑控制验收；Claude Code 原生 TUI 与 Tailcat 继续保持实验性边界。
