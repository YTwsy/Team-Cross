# GitHub CI、unsigned 发布与 v0.1.6-rc.1/v0.1.6-rc.2 验证 · 2026-09-15

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

## 受保护 Homebrew 发布链路

[PR #9](https://github.com/YTwsy/Team-Cross/pull/9) 将独立的 `YTwsy/homebrew-teamcross` 纳入 Release 后置流程：稳定通道使用 `teamcross` 与 `team-cross`，RC 独立通道使用 `teamcross-rc` 与 `team-cross@rc`；两条通道的 Formula/Cask 互斥，但卸载切换不删除协作数据。实现提交为 `f6e811b15cc71b28a5258af05cd8f31c14fc43da`，merge commit 为 `3ffb36f5f016521d107e008f547289d1d991aa18`；[PR run `34944769052`](https://github.com/YTwsy/Team-Cross/actions/runs/34944769052) 与 [main run `34945174559`](https://github.com/YTwsy/Team-Cross/actions/runs/34945174559) 均通过。

tap 的初始 [PR #1](https://github.com/YTwsy/homebrew-teamcross/pull/1) 以 merge commit `b6349a00413364a11f4e4e939160e27976779f66` 建立稳定通道、版本元数据、公共安装 verifier 与 CI。[PR run `34942471770`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/34942471770) 和 [main smoke `34942858736`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/34942858736) 均通过。tap `main` 使用 ruleset `23422169`：禁止直接改写、强制受保护 PR、要求精确检查 `Verify public Formula and Cask`、只允许 squash，并要求分支为最新基线。

专用 GitHub App `Team Cross Homebrew Publisher`（App ID `4950863`、Client ID `Iv23liqM2cHpyhlo4jSI`）只安装到 `YTwsy/homebrew-teamcross`，installation ID 为 `161860130`。安装权限限定为 Actions/Checks 读取、Contents/Pull requests 写入和 GitHub 强制的 Metadata 读取；未启用 webhook、用户 OAuth、Device Flow 或组织/账户/企业权限。App 私钥保存在来源仓库 `homebrew-publish` environment 的 `HOMEBREW_PUBLISH_APP_PRIVATE_KEY` secret，Client ID 保存在同名 repository variable；environment 只允许匹配 `v*.*.*` 的 tag。替代私钥成功签发只包含该 tap 的短期 installation token 后，失效 key 已从 App 删除，本地明文替代私钥也在真实发布验证后删除。

[PR #10](https://github.com/YTwsy/Team-Cross/pull/10) 的提交 `1bc067af94ffdab1350065de9e962e054cf2b870` 准备 `v0.1.6-rc.2`，merge commit 为 `8ad5ac41f847b7d62ef9e5174edd0074f7308776`；[PR run `34945530887`](https://github.com/YTwsy/Team-Cross/actions/runs/34945530887) 与 [main run `34945711981`](https://github.com/YTwsy/Team-Cross/actions/runs/34945711981) 均通过。该版本还修复 RC Formula 把 Codex MCP 启动路径绑定到版本化 Cellar 的问题，改用稳定的 `opt/teamcross-rc/bin/teamcross`。

## v0.1.6-rc.2 tag、构建与公开 Release

首次 annotated tag 指向 `8ad5ac41...`，但 [run `34949518481`](https://github.com/YTwsy/Team-Cross/actions/runs/34949518481) 在创建任何 job、artifact 或 Release 前以 `startup_failure` 结束。仓库默认 `GITHUB_TOKEN` 为只读，而调用 Homebrew reusable workflow 的 caller job 未声明被调用方所需的 `contents: write`，GitHub 在调度阶段拒绝整张 job 图；本地 `actionlint` 对语法返回零错误，不能发现这一服务端权限上限。

[PR #11](https://github.com/YTwsy/Team-Cross/pull/11) 的提交 `a3cfde2d28db10990a1cf635db6efeac952385cd` 为 caller job 增加唯一所需的 `contents: write`，merge commit 为 `f76cdb1b491224dc7bebf31088b68dac666d1da2`；[PR run `34949818605`](https://github.com/YTwsy/Team-Cross/actions/runs/34949818605) 与 [main run `34949850291`](https://github.com/YTwsy/Team-Cross/actions/runs/34949850291) 均通过。在确认没有同名 Release 或产物后，经用户明确授权，使用旧 tag object 作为 `--force-with-lease` 条件将唯一远端引用更新为新的 annotated tag object `7836e6481841b1b64ea3d97efeddac8972206d69`，解引用目标为 `f76cdb1...`。

[tag run `34950731821`](https://github.com/YTwsy/Team-Cross/actions/runs/34950731821) 的 `Build and verify arm64 release` job 在 4 分 4 秒内通过全部工程、构建、App/CLI 生命周期、隔离 Homebrew、manifest、SHA-256 与两份 attestation 门槛；随后 `Publish GitHub release` job 在 15 秒内创建 [v0.1.6-rc.2 Pre-release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.1.6-rc.2)。Release 为非 Draft、Pre-release，tag 与 `release.json` 均指向 `f76cdb1...`。workflow artifact `Team-Cross-0.1.6-rc.2-unsigned-release` 的 ID 为 `10389660640`，归档大小为 17,745,491 字节，过期时间为 `2026-09-29T09:11:10Z`。

公开资产及 GitHub 记录的 SHA-256 为：

| 文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| `teamcross-0.1.6-rc.2-darwin-arm64.tar.gz` | 7,808,879 | `99e9307b096dba5e6bd611ef2adfde783160251cd1037a891781a402ef12182b` |
| `Team-Cross-0.1.6-rc.2-arm64.dmg` | 10,052,293 | `313dc9812382c664c208b3faedae9aeda3700a438c7c336446ae99d36164a45f` |
| `SHA256SUMS` | 205 | `d852623e1bc272c40824f60c3160c93486b45382a0e31b3f25f5039ee63c4f90` |
| `release.json` | 476 | `411843fc9284602d095797bfbca8a274d3a33af027c48a397c023c2375b86713` |

重新下载四个公开资产后，`shasum -a 256 -c SHA256SUMS` 对 CLI 与 DMG 均返回 `OK`。`release.json` 记录 `version=0.1.6-rc.2`、`commit=f76cdb1...`、`buildNumber=47`、`dirty=false`、`architecture=arm64`、`minimumMacOS=14.0`、`developerIDSigned=false` 和 `notarized=false`。将 `XDG_CACHE_HOME` 指向可写临时目录后，两份 `gh attestation verify` 均通过，并严格限制 signer workflow、`refs/tags/v0.1.6-rc.2`、source digest `f76cdb1...` 和非 self-hosted runner；默认 cache 下的 verifier 初始化失败仍只是本机 cache 权限，不是 provenance 失败。

## RC tap 发布、恢复与公共安装

同一 tag run 的 Homebrew job 已依次通过公开 Release 状态、四个资产下载、SHA-256、manifest、两份 attestation、渲染验证、公开资产隔离 Homebrew 安装和短期 App token 创建，但第一次在 `git push` tap 分支时以 `could not read Username for 'https://github.com'` 结束：`gh repo clone` 的一次性认证没有成为后续原生 Git push 的 credential helper。失败前远端不存在 RC 分支或 PR，token 在 post action 中正常撤销，Release 资产不受影响。

恢复流程使用同一 GitHub App 再签发一次最小权限 token，复核公开校验和与相同渲染结果，配置临时 Git credential helper 后只写入 RC 三个文件。App bot 创建的提交为 `842d64c9e3edc4ae7c87773e1faec6ec6dce0ac3`，对应 [tap PR #2](https://github.com/YTwsy/homebrew-teamcross/pull/2)；token 随即撤销。[PR check `34951723996`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/34951723996) 通过后，ruleset 以 squash merge commit `a2dd16559b7d0e85a03ab2038b9ab8cf6a800709` 合入。随后 [public main smoke `34951879325`](https://github.com/YTwsy/homebrew-teamcross/actions/runs/34951879325) 在 1 分 35 秒内再次从 Release URL 下载并完成隔离 Formula/Cask 安装。

`Versions/rc.json` 现记录 `0.1.6-rc.2`、来源 `f76cdb1...`、上述 CLI/DMG 摘要以及 `developerIDSigned=false`、`notarized=false`；`Versions/stable.json` 仍保持 `0.1.1`，因此普通稳定通道不会自动切到 RC。Release 正文已按 workflow 的 marker 格式写入 `teamcross-rc`、`team-cross@rc` 两条安装命令、tap PR 和 merge commit。后续 workflow 在创建 tap PR 前运行 `gh auth setup-git`，使原生 Git push 使用当前短期 App token；静态检查、9 项 Homebrew 测试和 `git diff --check` 均通过。

本轮证明 `v0.1.6-rc.2` 在对应 tag 上完成 unsigned arm64 构建、公开 Release、来源证明、受保护 tap PR 和公共 Homebrew 安装闭环。它仍不证明 Developer ID、Apple 公证、下载隔离后的 Gatekeeper 首次批准、Intel、真实模型或两台物理 Mac/Tailcat 公网；这些边界没有因 Homebrew 发布成功而扩大。
