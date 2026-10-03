# 构建与发布

本页维护安装产物的构建、版本与来源校验、GitHub / Homebrew 发布顺序及签名边界。安装后的命令归属、服务生命周期和首次使用由 [分发与首次体验](distribution-and-onboarding.md) 维护；检查要求见 [验证门槛](validation/test-gates.md)，具体版本的已知结果见 [证据入口](validation/evidence-map.md#网络与分发)。流程存在或 workflow 配置完成不表示某个版本已发布。

## 构建产物

`make release` 调用 [构建脚本](../../../scripts/build-release.py)，在 `dist/release/<版本>/` 生成 CLI tar.gz、App、DMG、SHA256SUMS、release.json 和对应渠道的独立 tap 内容。开发默认版本以 [Makefile](../../../Makefile) 的 `VERSION` 为准；安装渠道的名称、互斥和 App helper 归属见 [安装产物与边界](distribution-and-onboarding.md#安装产物与边界)。

未提供 Developer ID 身份时使用 ad-hoc 签名；签名与公证状态如实保留在构建清单，不作为版本名称。所有非 `-dev` 发布构建要求干净 checkout，重建 Web 后再次核对；已有版本产物不被静默替换。清单记录完整提交、版本、buildNumber、dirty、架构、系统下限与校验值。App 的 buildNumber 来自完整提交历史的提交数量，因此 GitHub release checkout 必须使用完整历史。脚本不保存凭据、不发布网络内容。

## 发布顺序与流水线

发布来源为核对过的干净提交。先使 `YTwsy/Team-Cross` Release 产物可下载并核对校验值，再发布 `YTwsy/homebrew-teamcross` 中对应的配方。生成的下载 URL 不代表已经发布，实际状态以 GitHub 为准。本地安装测试使用临时 tap、临时 Homebrew 前缀和应用目录。

GitHub 上的 `CI` workflow 对指向 `main` 的 PR 和 `main` push 运行 Go test/vet、相关 race test、Web check/test/build、嵌入资源一致性和 Darwin arm64 CLI 交叉编译。所有 Tailcat 联网用例默认跳过，不把公共 DERP 可用性变成普通 PR 的外部硬依赖。

`Unsigned macOS release` workflow 接受 `X.Y.Z` 与 `X.Y.Z-rc.N`：手动触发时从可到达 `origin/main` 的所选提交构建、验证、attest 并保存 14 天 workflow artifact，不创建 Release；推送同版本 annotated tag 时使用同一构建链，并在全部检查完成后创建不可覆盖的 GitHub Release。RC 标记为 Pre-release，正式版本标记为 Latest。tag 版本是发布版本的唯一输入，发布构建不使用 Makefile 或脚本的开发默认值。自动门槛依次包括：

1. 完整 Git 历史、tag 格式与 `origin/main` 来源校验。
2. Go/Web 工程门槛，共享一套 Go 缓存并串行运行 test、vet 与 race，避免 Tailcat/Tailscale/gVisor 冷编译重复占用磁盘。
3. arm64 CLI、App、DMG、校验文件、构建清单、渠道元数据和 Homebrew 定义生成。
4. `verify-release.py` 的 DMG/App/CLI/生命周期检查，以及 `verify-homebrew.py` 的隔离 Formula/Cask 检查。
5. `release.json` 的版本、来源提交、架构、最低系统、干净状态和 unsigned/unnotarized 边界检查。
6. CLI tar 与 DMG 的 GitHub artifact attestation、下载后 SHA-256 复核和对应渠道的 Release 创建。
7. [Homebrew 发布 workflow](../../../.github/workflows/homebrew-publish.yml) 从公开 Release 重新下载资产，严格复核 annotated tag、`origin/main` 来源、checksum、manifest 与两份 attestation，在临时 Homebrew 前缀安装对应渠道后，使用短期 GitHub App token 向 `YTwsy/homebrew-teamcross` 创建 PR。tap 的只读 CI 通过并自动合并后，再等待公共 `main` 安装 smoke，并把 tap PR、commit 与安装命令写回 GitHub Release。

tag 发布另要求同一提交包含 `docs/releases/<tag>.md`，并在发行说明中明确 ad-hoc 签名、未使用 Developer ID 和未公证。项目在较长时间内不以 Developer ID 身份作为正式版本发布前置条件；正式 GitHub Release 也可以发布经过同一完整门槛验证的 ad-hoc/unsigned 产物。Latest、稳定版本号和 GitHub provenance 都不代表 Apple 签名、公证或 Gatekeeper 验收；未来取得签名能力后，应显式调整构建清单、验证和发行说明，不能把 unsigned 产物描述为已签名。

Homebrew 发布是同一 tag 流程中位于 GitHub Release 之后的受保护阶段，不与二进制构建并行：配方 URL 必须先成为可公开下载的不可覆盖 Release 资产。`Team-Cross` 仓库的默认 `GITHUB_TOKEN` 不跨仓库写入；`homebrew-publish` environment 只允许 `v*.*.*` tag，并只提供安装到 `YTwsy/homebrew-teamcross` 的 GitHub App 身份，运行时进一步收窄为 tap 的 Actions/Checks 只读与 Contents/Pull requests 读写。自动化还要求 workflow ref 与输入 tag 完全相同；手动补发必须从同一 release tag 运行。它只维护版本专属 `codex/homebrew-v...` 分支和 PR；遇到无 PR 的同名远端分支、同渠道版本倒退、同版本不同元数据或无法识别的既有发布时停止，不强推或直接改 `main`。

公共 tap 的 `main` 只接受 PR，并要求 `Verify public Formula and Cask` 检查。新 PR 创建后先有界等待该检查登记，再等待检查完成，避免 `gh pr checks --watch` 在没有检查时立即退出；十分钟内未登记、API 错误或检查失败均停止发布。稳定 tag 更新无后缀 Formula/Cask；RC tag 只更新显式 RC 定义，不会把稳定安装者自动带到候选版。tap PR 与合并后的 push 都从公开 URL 下载 `release.json`、`SHA256SUMS`、CLI 和 DMG，在临时 Homebrew 前缀验证 Formula/Cask 安装、命令字节、同渠道及跨渠道互斥。GitHub Release 先成功而 tap 后续失败时，二进制 Release 保持有效，公共 tap 继续停留在上一个已验证版本，整条 workflow 以失败状态提示修复或安全重跑；不能把这种部分状态写成 Homebrew 已发布。

## 本地构建与检查

以下命令在仓库根目录执行。`X.Y.Z` 是待发布版本的占位符，执行前替换成同一个实际版本；候选版本使用 `X.Y.Z-rc.N`。已有产物目录不能被静默覆盖。

```sh
# 本地开发产物；不上传网络内容。
make release
make verify-release
make verify-homebrew
# 单独检查真实菜单栏 App 的跨副本启动，需要 macOS 图形登录会话：
make verify-app-instance
# 在干净 checkout 构建版本产物；已有清单时需另选输出目录：
make release VERSION=X.Y.Z
make verify-release VERSION=X.Y.Z
make verify-homebrew VERSION=X.Y.Z
# 有效身份与已有 notarytool keychain profile 准备好后才使用：
python3 scripts/build-release.py --version X.Y.Z --sign-identity 'Developer ID Application: …' --notary-profile teamcross
```

构建和安装检查入口：[构建脚本](../../../scripts/build-release.py)、[产物验证](../../../scripts/verify-release.py)、[Homebrew 验证](../../../scripts/verify-homebrew.py)。自动化入口：[CI](../../../.github/workflows/ci.yml)、[Release](../../../.github/workflows/release-unsigned.yml)、[Homebrew 发布](../../../.github/workflows/homebrew-publish.yml)。

## 结果与证据

发布结果须区分本地构建、workflow artifact、公开 GitHub Release、tap PR 与公开安装检查。具体版本的提交、runner、资产校验和未覆盖项按 [任务记录规则](../README.md#任务记录) 保存；改变已知范围时同步 [证据入口](validation/evidence-map.md#网络与分发)。托管 runner 或本机安装不自动证明真实模型、两台 Mac 网络、Apple 公证或 Gatekeeper 首次批准通过。
