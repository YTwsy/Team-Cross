# v0.2.1 正式版发布验证 · 2026-09-21

## 来源与主线

发布准备提交为 `b1b9978a2f0aaa3b8b2ec21791ca921bcec1487e`，包含 README/使用与开发指南调整及 `docs/releases/v0.2.1.md`。PR [#20](https://github.com/YTwsy/Team-Cross/pull/20) 的 CI run [35592101253](https://github.com/YTwsy/Team-Cross/actions/runs/35592101253) 通过 Go test/vet、并发回归、Web check/test/build、嵌入资源一致性、Homebrew 定义测试与 arm64 CLI 交叉编译；随后以 merge commit `6db3f0327a73df51072b0d4e656bef59cdd64c7d` 合入 `main`。

annotated tag `v0.2.1` 的 tag object 为 `561f6ad28c2defaf50992aa24d581e846eb9bcfd`，解引用后精确指向 `b1b9978a2f0aaa3b8b2ec21791ca921bcec1487e`。该 release commit 是上述 `main` merge commit 的第二父提交，因此通过 `origin/main` 可达性检查。没有移动或替换既有 tag。

## tag 构建与 GitHub Release

发布流水线 [run 35592681039](https://github.com/YTwsy/Team-Cross/actions/runs/35592681039) 在 GitHub 托管 `macos-15` arm64 runner 上成功完成。Build job `106310349423` 用时 6 分 58 秒，工程门槛、release 构建、安装与生命周期、隔离 Homebrew、manifest/checksum，以及 CLI 与 DMG 两份 attestation 均通过。Publish job `106312355248` 用时 12 秒，重新下载并验证产物及发行说明后创建 Release。

[GitHub Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.1) 于 `2026-09-21T11:18:07Z` 创建，为非 Draft、非 Pre-release、Latest。公开 `release.json` 记录 `version=0.2.1`、`commit=b1b9978...`、`buildNumber=82`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false`、`notarized=false`。

| 资产 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.2.1-darwin-arm64.tar.gz` | 8,207,562 | `d0cf952bc6a31f1fb502cd9c727467fd607a991b70bd06f276e514b1951fdd54` |
| `Team-Cross-0.2.1-arm64.dmg` | 10,502,200 | `7cb7c4587e154ba890f6a75fc38a4784d7b6ce8db4797b5b5b72844dcc725f94` |
| `SHA256SUMS` | 195 | `b1d2e65bbb88c54b8b8f7edc8111d885992496f383775f61e08ae75a7d5931e9` |
| `release.json` | 461 | `d7ee6899e6b5338c25a20f4cf6b14e166bc33bbe6c81767454842a13be1f4487` |

`SHA256SUMS` 中的 CLI/DMG 摘要与公开 `release.json` 及 GitHub asset digest 一致；Homebrew job 重新下载公开资产并通过 checksum 检查。其 `Verify release manifest and provenance` 步骤也对 CLI 与 DMG 执行 `gh attestation verify`，限定仓库 `YTwsy/Team-Cross`、workflow `.github/workflows/release-unsigned.yml`、source ref `refs/tags/v0.2.1`、source digest `b1b9978...`，并拒绝 self-hosted runner。

保留的 workflow artifact 为 `Team-Cross-0.2.1-unsigned-release`，ID `10635980921`，大小 18,595,412 字节，记录的过期时间为 `2026-10-05T11:17:42Z`。

## 稳定 Homebrew 通道

正式流水线的发布前公开资产安装验证通过后，创建 tap [PR #7](https://github.com/YTwsy/homebrew-teamcross/pull/7)。[PR CI run 35593429333](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35593429333) 的 `Verify public Formula and Cask` 通过；受保护自动 squash 合并生成 tap commit `fe7bd87e4f6336c416936539f87ae0251981a838`。[公共 main smoke run 35593560135](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35593560135) 随后通过，再由 release workflow 将 Homebrew 状态、PR 与 commit 写回 Release。

当前 `Versions/stable.json` 记录 `version=0.2.1`、tag `v0.2.1`、来源提交 `b1b9978...`，CLI/DMG URL 和 SHA 与 Release 一致；Formula 为 `Formula/teamcross.rb`，Cask 为 `Casks/team-cross.rb`。Release 正文已包含经公共安装 smoke 验证的命令：`brew install YTwsy/teamcross/teamcross` 与 `brew install --cask YTwsy/teamcross/team-cross`。

## 签名状态与未覆盖范围

本版提供 arm64、macOS 14.0+ 产物，使用 ad-hoc 签名，未使用 Developer ID 签名或 Apple 公证。Release、provenance 和 Homebrew 安装成功不代表 Gatekeeper 首次批准。此次发布流水线没有运行真实模型，也没有新增两台 Mac LAN、跨网络 Tailcat 或持续 DERP 验收；Claude Code 原生 TUI 与 Tailcat 仍为实验性。
