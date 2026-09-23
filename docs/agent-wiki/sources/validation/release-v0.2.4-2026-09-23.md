# v0.2.4 正式版发布验证 · 2026-09-23

## 来源与主线

本版修复协作详情短页面在成员自动折叠时反复上下跳动的问题，复现、原因、122 项 Web 回归和浏览器检查见[成员折叠稳定性](member-fold-stability-2026-09-23.md)。按用户要求直接提交 `main`，先快进保留远端 README 提交 `b459027`，再提交修复、内嵌 Web 资源、版本入口和发行说明。

发布提交为 `f671cb4a0e30cc79a7a265658209317243af58aa`。Annotated tag `v0.2.4` 的 tag object 为 `cd5a521112ac7358dc90acd388bdccdad3e55be3`，解引用精确指向该提交；发布时远端 `main` 也指向该提交。已有本地 Swift 和宣传素材改动没有进入发布提交。

## 流水线与公开资产

[main CI 35854652993](https://github.com/YTwsy/Team-Cross/actions/runs/35854652993) 通过 Go test/vet、七个包的 race test、Web check/test/build、内嵌资源一致性、Homebrew 定义测试和 Darwin arm64 CLI 交叉编译。

[Tag 发布流水线 35854669001](https://github.com/YTwsy/Team-Cross/actions/runs/35854669001) 全部成功。GitHub 托管 `macos-15` arm64 runner 重新执行工程检查、CLI/App/DMG 构建、安装与生命周期、隔离 Homebrew 安装、manifest/checksum 和两份构建来源证明；发布 job 下载并复核这些资产后创建 Release。

[GitHub Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.4) 于 `2026-09-23T11:34:35Z` 发布，为非 Draft、非 Pre-release，`releases/latest` 返回 `v0.2.4`。公开 `release.json` 记录 `version=0.2.4`、`commit=f671cb4...`、`buildNumber=107`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false`、`notarized=false`。

| 公开资产 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.2.4-darwin-arm64.tar.gz` | 8,249,846 | `71912338436fcbfc9b04b2b8341e24ae3ad14a545b51e16017821a68ac683b68` |
| `Team-Cross-0.2.4-arm64.dmg` | 10,571,802 | `cdb6e10268db12452d15da4bde66fde468c5aab6280e5a13c5ebfe7ca3d05833` |
| `SHA256SUMS` | 195 | `153f7a4289df03dfa916ec4faf4df8bd8a54e795473779522653089df5cee28f` |
| `release.json` | 462 | `d786379d85799b875159d965b8e08cefc1860e0ab76e18c8e80e0617fc56fad3` |

独立下载完整公开资产至本地忽略目录 `dist/release/0.2.4-public-verification/` 后，CLI 与 DMG 的 SHA-256 校验通过，来源提交与 GitHub asset digest、manifest、稳定 Homebrew 元数据一致。在主机环境分别运行 `gh attestation verify`，限定仓库、`.github/workflows/release-unsigned.yml`、`refs/tags/v0.2.4`、完整来源提交并拒绝 self-hosted runner，均成功。受限环境无法初始化 Sigstore verifier，因此来源证明复核在主机环境完成。

Workflow artifact 名为 `Team-Cross-0.2.4-unsigned-release`，ID `10746953764`，记录的过期时间为 `2026-10-07T11:34:16Z`。

## 稳定 Homebrew 通道

后续 Homebrew 阶段从公开 Release 下载并验证资产、来源证明与隔离安装，再自动创建 tap [PR #10](https://github.com/YTwsy/homebrew-teamcross/pull/10)。[PR CI 35855474623](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35855474623) 通过，受保护合并生成 `df06f5227aa9efa083f0ab25bec5b65272cdfbb4`；[公共 main smoke 35855687846](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35855687846) 同样通过。Release 正文已自动写回 Homebrew 发布结果。

公开 `Versions/stable.json` 记录 `version=0.2.4`、tag `v0.2.4`、来源提交 `f671cb4...`，CLI/DMG 的下载地址与摘要均与 Release 一致。稳定 Formula 为 `Formula/teamcross.rb`，Cask 为 `Casks/team-cross.rb`。

## 验证边界

本版仅提供 Apple Silicon、macOS 14+ 产物，使用 ad-hoc 签名，未使用 Developer ID 签名或 Apple 公证。安装与生命周期检查由本次托管流水线执行；未替换用户本机正在运行的 App/Core，升级仍需退出并通过原渠道安装新版本。

本次没有新增真实模型、原生客户端、两台 Mac LAN、跨网络 Tailcat 或持续 DERP 验收。原问题页面仅作只读复现，测试预览、临时视口与媒体模拟已关闭或恢复，原有 Core 和用户浏览器页面保留。
