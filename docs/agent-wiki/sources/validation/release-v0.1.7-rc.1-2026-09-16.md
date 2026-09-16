# v0.1.7-rc.1 发布验证 · 2026-09-16

## 来源与版本

本版发布创建时可选、创建后固定的信任模式，覆盖 Codex 和实验性的 Claude Code。功能提交为 `16d5a1f`，原生工具、权限、恢复与浏览器结果见 [信任模式验收](trusted-runtime-2026-09-16.md)。

[PR #15](https://github.com/YTwsy/Team-Cross/pull/15) 将 `codex/release-v0.1.7` 以 merge commit `face795e41f184665ec19c15ce0a44c99dd3b921` 合入 `main`。PR head 为 `2959f6843cf13b184765a8fa4e8f76d5ed4d264b`，包含版本准备提交 `72e69850e379fc60279e787779a08c91cef8d0f7` 和发行说明链接修正。

annotated tag `v0.1.7-rc.1` 的 tag object 为 `103a51f5b0d2a40f90f6bf66bfce04c0f449f827`，解引用后精确指向 `face795e...`。本文与发布自动化的后续修正不改写已发布的 tag 或资产。

## 发布前检查

本地干净候选 `72e6985` 在 macOS 14.8.5 / arm64 上完成 `0.1.7-rc.1` CLI/App/DMG 构建，`buildNumber=58`。`verify-release.py` 验证 DMG 安装、App 图标与签名完整性、CLI/App 版本一致、真实 App 双副本、命令入口、Core 生命周期及数据保留；`verify-homebrew.py` 验证临时前缀中的 RC Formula/Cask 安装、互斥、升级、稳定 MCP 路径和卸载。两项均通过，SHA-256 与干净状态核对通过；本地预检包不等于最终公开包。

[PR CI `35075704182`](https://github.com/YTwsy/Team-Cross/actions/runs/35075704182) 与 [main CI `35075975621`](https://github.com/YTwsy/Team-Cross/actions/runs/35075975621) 均通过 Go test/vet/race、Web check、53 项 Web 测试、production build、嵌入资源一致性、Homebrew 渲染测试与 Darwin arm64 编译。合并后的 main CI 通过后才推送 tag。

## tag 构建与公开资产

[发布流水线 `35076185174`](https://github.com/YTwsy/Team-Cross/actions/runs/35076185174) 在 GitHub 托管 `macos-15` arm64 runner 上构建。初次运行的 build job `104729040918` 与 publish job `104730584780` 均成功，包含工程门槛、安装与生命周期、隔离 Homebrew、清单、checksum 和两份 artifact attestation。

[GitHub Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.1.7-rc.1) 于 `2026-09-16T08:57:38Z` 创建，为非 Draft、Pre-release。公开清单记录 `version=0.1.7-rc.1`、`commit=face795e...`、`buildNumber=60`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false`、`notarized=false`。

| 资产 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.1.7-rc.1-darwin-arm64.tar.gz` | 7,816,442 | `23cca3c8340eb9779eade86f1f1d15e470fe257ac3b98150fba57dccb78fa100` |
| `Team-Cross-0.1.7-rc.1-arm64.dmg` | 10,041,801 | `92516c1f7b9e270da8381726b91aee952d561ead383ac281d3b5c248a2772a2a` |
| `SHA256SUMS` | 205 | `b5d1aaa79fde0e7bd8b7914713ea448b9da0239229015c6b07d3522b15c5134c` |
| `release.json` | 476 | `9560485c2f3f69b6d4664a7bd2bf6d0d2b198b7e64ac6e660b38007d1315eccd` |

workflow artifact 为 `Team-Cross-0.1.7-rc.1-unsigned-release`，ID `10438740554`，大小 17,735,115 字节，过期时间 `2026-09-30T08:57:23Z`。

四个公开资产已重新下载到 `/private/tmp/teamcross-v017-public-20260916`，逐个核对 GitHub asset digest、大小、SHA256SUMS 与 release.json。CLI tar 和 DMG 的 `gh attestation verify` 均以退出码 0 通过，约束仓库、`.github/workflows/release-unsigned.yml`、source ref `refs/tags/v0.1.7-rc.1`、source digest `face795e...`，并拒绝 self-hosted runner。

## Homebrew RC 与等待检查的竞态

发布流程使用 GitHub App 创建 [tap PR #4](https://github.com/YTwsy/homebrew-teamcross/pull/4)，head 为 `c1b87b7c65236ce637db91c350f7666ba1de1ce2`。[PR 检查 `35076796990`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35076796990) 通过后，受保护自动合并生成 `956f81621e484a2d9c87de717e330edbe86e2f2e`；[公共 main 安装 smoke `35076981593`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/35076981593) 也通过。

首次上游 Homebrew job 在 `08:59:02Z` 因 `gh pr checks --watch` 返回 `no checks reported` 失败；tap 检查直到 `08:59:09Z` 才登记。PR 的自动合并已经启用，tap 验证随后正常执行。仅重跑失败阶段后，发布流水线第二次尝试整体成功，沿用已发布资产，并确认公共 tap 与 Release 回写。没有绕过检查、直接推送 tap main 或替换 Release 资产。

后续 workflow 增加有界等待：最多十分钟确认 `Verify public Formula and Cask` 已登记，再进入原有检查等待、受保护合并和公共 smoke。API 错误、检查失败或超时仍失败。`scripts/test_homebrew_release.py` 增加对实际 shell step 的回归，模拟延迟登记、立即登记、未登记超时、API 错误和检查失败；本地 10 项测试及其中五种等待场景通过。这项发布自动化修正通过后续 PR 进入 main，不改变本版二进制来源。

公共 `Versions/rc.json` 已核对为 `0.1.7-rc.1`，来源提交和 CLI/DMG 摘要与公开 Release 一致；Release 正文已出现 Homebrew 成功状态、tap PR/commit 与 RC 安装命令。

## 收尾与边界

测试仅使用临时安装目录和 Homebrew 前缀，用户已有 Team Cross、个人客户端和模型路由保留。源码与构建缓存未做额外清理，旧版本产物与本地预检材料保留。

本版使用 ad-hoc 签名，未使用 Developer ID 签名或 Apple 公证。构建来源证明和公共安装通过不代表 Gatekeeper 首次批准、Intel、真实 macOS 26 图标显示、两台 Mac LAN、跨网络 Tailcat 或持续 DERP 验收。真实模型和工具范围以本页链接的信任模式记录为准；发布流水线没有重复真实模型调用，也没有新增专用 Desktop 窗口、电脑控制或 Claude OAuth/Keychain 的实测结论。
