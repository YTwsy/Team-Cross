# GitHub CI、unsigned 发布与 v0.1.6-rc.1 验证 · 2026-09-15

## 范围与来源

本轮验证新接入的 GitHub `CI` 与 `Unsigned macOS release candidate` workflow。实现经 [PR #5](https://github.com/YTwsy/Team-Cross/pull/5) 以 merge commit `4693bc048273edd097d016c15ae7b60d2f6e7fc9` 合入 `main`；原本只存在于本地 `main` 的 App 图标提交 `56b827e248c554468972c906f9550b5aa70c7466` 保持原提交身份，并成为该远端 `main` 的祖先。

候选构建使用 GitHub 托管的 `macos-15` arm64 runner、完整 Git 历史、Go 与 Node/pnpm 的共享任务缓存。它没有读取开发机工作区，也没有使用真实模型、Tailcat 实网或用户的正式 Homebrew 前缀。

## PR 与 main 工程门槛

首次 [PR workflow run](https://github.com/YTwsy/Team-Cross/actions/runs/34929808947) 的 Go test 与 vet 已通过，但 race test 在 `internal/cliinstall.TestConcurrentInstallAndRemove` 发现真实竞态：安装器在取得目录锁前读取启动器状态，另一个并发操作可能在检查与执行之间创建目标，导致把 Team Cross 自己的启动器误报为外部冲突。提交 `171f9ad1080040c3b1a195fcbdda4038fac6f859` 将影响决策的状态读取移入锁内，同时保留无需写入时的 Cask 快速返回。

修复后的 [PR run `34930278082`](https://github.com/YTwsy/Team-Cross/actions/runs/34930278082) 在 3 分 31 秒内通过；合并后的 [main push run `34930537593`](https://github.com/YTwsy/Team-Cross/actions/runs/34930537593) 在 4 分 55 秒内再次通过。两轮都覆盖：

- patch 格式、`go test ./...`、`go vet ./...`；
- `collab`、`mcp`、`sharing`、`nativecodex`、`nativeclaude`、`service`、`cliinstall` 的 race test；
- Web check、交互测试、production build 与嵌入资源一致性；
- `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` 的 CLI 交叉编译。

## unsigned arm64 候选构建

[workflow dispatch run `34930880865`](https://github.com/YTwsy/Team-Cross/actions/runs/34930880865) 从 `main@4693bc048273edd097d016c15ae7b60d2f6e7fc9` 构建 `0.1.6-rc.1`，`Build and verify arm64 candidate` 在 7 分 49 秒内通过。除完整工程门槛外，实际完成：

- arm64 CLI、App、DMG、`release.json`、`SHA256SUMS` 与 Homebrew Formula/Cask 生成；
- DMG 挂载安装、App/CLI 版本一致、两个真实 App 副本、命令启动器和 Core 生命周期检查；
- 临时 Homebrew 前缀中的 Formula/Cask 安装、升级、互斥、回滚与卸载；
- manifest 边界、下载后 SHA-256 复核，以及 CLI tar/DMG 各一份 GitHub SLSA build provenance attestation。

这次托管 runner 的图形登录会话足以完成 `verify-app-instance.py`，因此该 runner 在此提交和日期上的 App 副本检查已由实际运行确认；这不是对未来 runner 镜像或其他 macOS 环境的永久保证。

workflow artifact `Team-Cross-0.1.6-rc.1-unsigned-candidate` 的 ID 为 `10382006309`，GitHub 记录的归档大小为 17,744,250 字节，过期时间为 `2026-09-29T05:05:54Z`。下载后再次运行 `shasum -a 256 -c SHA256SUMS`，两个文件均通过：

| 文件 | SHA-256 |
| --- | --- |
| `teamcross-0.1.6-rc.1-darwin-arm64.tar.gz` | `01b0a1f5a0bdd130fee9f898224d31b3805f415ab3c7093e60c85c0d8e9b6633` |
| `Team-Cross-0.1.6-rc.1-arm64.dmg` | `df5e4312afafbb02b71f075bfa1a11e881fa274255aefb516e1d407b104048a6` |

`release.json` 记录 `buildNumber=36`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false` 和 `notarized=false`。GitHub attestations API 分别为上述两个摘要返回一份 provenance，解析后的来源为本仓库、`refs/heads/main`、该 workflow 与提交 `4693bc048273edd097d016c15ae7b60d2f6e7fc9`。

## 首次候选的发布边界

这次首次候选由 `workflow_dispatch` 触发，发布 job 按条件跳过；当时核对后不存在 `v0.1.6-rc.1` tag 或同名 GitHub Release，也没有写入独立 Homebrew tap。当时的 workflow 只允许从 `main` 可达提交创建 annotated `vX.Y.Z-rc.N` tag，并进入不可覆盖的 Pre-release 发布路径。

该首次结果证明 GitHub 托管构建、安装验证、隔离 Homebrew 验证、校验和与来源证明在对应提交上闭环，不证明 Developer ID 签名、公证、下载隔离后的 Gatekeeper 首次批准、公开 tap 安装、Intel/其他 macOS、真实模型或两台 Mac 网络。Tailcat 的两台 Mac LAN/跨网/强制 DERP 门槛仍以 [Tailcat 验证记录](tailcat-transport-2026-09-15.md) 为准，未因本次 CI 成功而扩大结论。

## unsigned 正式版策略与 v0.1.6-rc.1 发布

用户确认较长时间内不再以 Apple Developer ID 作为正式版发布前置条件。[PR #7](https://github.com/YTwsy/Team-Cross/pull/7) 的提交 `334324adf271f7339248213521bed4cf97841293` 将 workflow 扩展为同时接受 `X.Y.Z` 与 `X.Y.Z-rc.N`：RC 发布为 Pre-release，正式版本发布为 Latest；两者继续要求干净的 `main` 来源、完整工程/安装/Homebrew 验证、SHA-256、provenance，以及发行说明中的 ad-hoc、未使用 Developer ID、未公证声明。它没有把 Latest、稳定版本号或 provenance 表述成 Apple 信任链。

PR [CI run `34934013885`](https://github.com/YTwsy/Team-Cross/actions/runs/34934013885) 在 59 秒内通过，merge commit `014f8ffbbf81f8e0f97d2af0afc661c8ba584713` 合入 `main` 后，[main run `34934108547`](https://github.com/YTwsy/Team-Cross/actions/runs/34934108547) 在 47 秒内再次通过。随后以 `version=0.1.6` 手动触发 [unsigned release run `34934182476`](https://github.com/YTwsy/Team-Cross/actions/runs/34934182476)：版本和 `main` 来源检查实际接受正式版本号，完整 build/verify job 在 4 分 22 秒内通过，发布 job 按 `workflow_dispatch` 规则跳过。保存的 `Team-Cross-0.1.6-unsigned-release` artifact ID 为 `10382787851`，对应 `main@014f8ff`；这证明正式版本号 gate 已移除，而不是绕过其余发布门槛。

annotated tag `v0.1.6-rc.1` 的 tag object 为 `bb75ecb4c12a2f729fd6425c99cef4b50c79c8cb`，解引用后与 `origin/main` 同为 `014f8ffbbf81f8e0f97d2af0afc661c8ba584713`。[tag run `34934581957`](https://github.com/YTwsy/Team-Cross/actions/runs/34934581957) 的 build/verify job 在 3 分 23 秒内完成，14 秒的 publish job 随后复核下载产物和发行说明并创建 [GitHub Pre-release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.1.6-rc.1)。Release 状态为非 Draft、Pre-release；workflow artifact `Team-Cross-0.1.6-rc.1-unsigned-release` 的 ID 为 `10383416136`。

公开资产及 GitHub 记录的 SHA-256 为：

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.1.6-rc.1-darwin-arm64.tar.gz` | 7,808,334 | `59a3a83374d2b3beb1467ef68426f5c95a8a550fb478431feda8bc0f27809dd4` |
| `Team-Cross-0.1.6-rc.1-arm64.dmg` | 10,052,848 | `3b1fbc62cabbdc1cb68148a1a9ac8b054b46ba12e06766fd6b104a8371d1884a` |
| `SHA256SUMS` | 205 | `9472d00353a9bc51180e26749925bf75d9b4e21b6e69f0c6adb3836d9895e81d` |
| `release.json` | 476 | `103089a93cc508e8c10a383aea4ca683f2020d75b835527a48bb3678f35a60e2` |

重新从公开 Release 下载四个资产后，`shasum -a 256 -c SHA256SUMS` 对 CLI 与 DMG 均返回 `OK`，两个元数据文件的本地摘要也与 GitHub asset digest 一致。`release.json` 记录 `version=0.1.6-rc.1`、`commit=014f8ff...`、`buildNumber=40`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false` 和 `notarized=false`。

在 arm64、macOS `14.8.5 (23J423)` 上使用仓库 `verify-release.py` 对公开下载运行隔离安装检查。首次运行已通过 CLI/Core 生命周期，但 `verify-app-instance.py` 的 Swift 辅助程序在冷 module cache 下超过固定 30 秒编译时限；缓存预热后的同一命令在 13.6 秒内完整通过，最终结果包含 checksum、DMG 安装、App 图标、签名完整性、CLI/App 版本一致、双 App 单实例、命令安装/移除、既有命令保护、兼容 Core 复用和数据保留。失败路径和成功路径都由脚本的 `finally` 卸载本次 DMG，没有留下 `v0.1.6-rc.1` 挂载。

GitHub attestations API 对 CLI 与 DMG 的上述摘要各返回一份记录。使用 `gh 2.92.0` 严格验证时，将 Sigstore TUF cache 指向可写临时目录后，两份 `gh attestation verify` 都通过，并约束 signer workflow 为 `YTwsy/Team-Cross/.github/workflows/release-unsigned.yml`、source ref 为 `refs/tags/v0.1.6-rc.1`、source digest 为 `014f8ff...`，同时拒绝 self-hosted runner。最初未设置临时 cache 时的 `no valid Sigstore verifiers` 来自沙箱不能写 `~/.cache/gh`，不是 attestation 内容失败。

该 Release 仍是 Apple Silicon、ad-hoc 签名、未使用 Developer ID、未公证；未更新公共 Homebrew tap，也没有新增真实模型、两台 Mac LAN、跨网络 Tailcat、强制持续 DERP、Intel 或下载隔离后的 Gatekeeper 验收。正式版本今后可以使用同一 unsigned 流程，但每次仍需保留这些边界并重新运行对应 tag 门槛。
