# GitHub CI 与 unsigned RC 验证 · 2026-09-15

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

## 发布与验收边界

本次由 `workflow_dispatch` 触发，发布 job 按条件跳过；核对后不存在 `v0.1.6-rc.1` tag 或同名 GitHub Release，也没有写入独立 Homebrew tap。只有从 `main` 可达提交创建的 annotated `vX.Y.Z-rc.N` tag 才会进入不可覆盖的 Pre-release 发布路径。

该结果证明 GitHub 托管构建、安装验证、隔离 Homebrew 验证、校验和与来源证明在本次提交上闭环，不证明 Developer ID 签名、公证、下载隔离后的 Gatekeeper 首次批准、公开 tap 安装、Intel/其他 macOS、真实模型或两台 Mac 网络。Tailcat 的两台 Mac LAN/跨网/强制 DERP 门槛仍以 [Tailcat 验证记录](tailcat-transport-2026-09-15.md) 为准，未因本次 CI 成功而扩大结论。
