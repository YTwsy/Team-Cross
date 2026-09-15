# 分发与首次体验

## 安装产物与边界

首版 Apple Silicon、macOS 14+。Swift/AppKit 菜单栏外壳复用 Go Core 和浏览器 WebGUI。CLI 包含 WebGUI 与 STDIO MCP，安装用户不需要 Go/Node/pnpm。Codex 按参与方式另行使用；B 查看上下文无需 Codex 或本地仓库。

`make release` 调用 [构建脚本](../../../scripts/build-release.py)，在 `dist/release/<版本>/` 生成 CLI tar.gz、App、DMG、SHA256SUMS、release.json 和独立 tap 内容。App 的 helper 位于 `Contents/Resources/teamcross`。Cask 安装 App 并用 `binary` 注册内置 CLI；Formula 提供独立 CLI，两种 Homebrew 安装互斥。Core 仍按数据目录共享，包管理互斥不改变服务发现协议。

首个公开版本使用 `v0.1.1`，发布标题为 `Team Cross v0.1.1`。发布来源为核对过的干净提交；本地构建脚本只生成产物，不发布网络内容。先使 `YTwsy/Team-Cross` Release 产物可下载并核对校验值，再发布 `YTwsy/homebrew-teamcross` 中对应的配方。生成的下载 URL 不代表已经发布，实际状态以 GitHub 为准。本地安装测试使用临时 tap、临时 Homebrew 前缀和应用目录。

GitHub 上的 `CI` workflow 对指向 `main` 的 PR 和 `main` push 运行 Go test/vet、相关 race test、Web check/test/build、嵌入资源一致性和 Darwin arm64 CLI 交叉编译。所有 Tailcat 联网用例默认跳过，不把公共 DERP 可用性变成普通 PR 的外部硬依赖。

`Unsigned macOS release` workflow 接受 `X.Y.Z` 与 `X.Y.Z-rc.N`：手动触发时从可到达 `origin/main` 的所选提交构建、验证、attest 并保存 14 天 workflow artifact，不创建 Release；推送同版本 annotated tag 时使用同一构建链，并在全部检查完成后创建不可覆盖的 GitHub Release。RC 标记为 Pre-release，正式版本标记为 Latest。tag 版本是发布版本的唯一输入，发布构建不使用 Makefile 或脚本的开发默认值。自动门槛依次包括：

1. 完整 Git 历史、tag 格式与 `origin/main` 来源校验。
2. Go/Web 工程门槛，共享一套 Go 缓存并串行运行 test、vet 与 race，避免 Tailcat/Tailscale/gVisor 冷编译重复占用磁盘。
3. arm64 CLI、App、DMG、校验文件、构建清单和 Homebrew 定义生成。
4. `verify-release.py` 的 DMG/App/CLI/生命周期检查，以及 `verify-homebrew.py` 的隔离 Formula/Cask 检查。
5. `release.json` 的版本、来源提交、架构、最低系统、干净状态和 unsigned/unnotarized 边界检查。
6. CLI tar 与 DMG 的 GitHub artifact attestation、下载后 SHA-256 复核和对应渠道的 Release 创建。

tag 发布另要求同一提交包含 `docs/releases/<tag>.md`，并在发行说明中明确 ad-hoc 签名、未使用 Developer ID 和未公证。项目在较长时间内不以 Developer ID 身份作为正式版本发布前置条件；正式 GitHub Release 也可以发布经过同一完整门槛验证的 ad-hoc/unsigned 产物。Latest、稳定版本号和 GitHub provenance 都不代表 Apple 签名、公证或 Gatekeeper 验收；未来取得签名能力后，应显式调整构建清单、验证和发行说明，不能把 unsigned 产物描述为已签名。

Homebrew 定义随候选作为 workflow artifact 保存并已完成隔离安装验证，但 workflow 不写入独立的 `YTwsy/homebrew-teamcross` 仓库。公共 tap 仍在 Release 资产可下载并复核后单独提交；未来自动化应使用仅覆盖 tap 仓库的 GitHub App 或等价最小权限凭据，并以 PR 而非直接改主分支的方式发布。

```sh
make release
make verify-release
make verify-homebrew
# 单独检查真实菜单栏 App 的跨副本启动，需要 macOS 图形登录会话：
make verify-app-instance
# 在干净 checkout 构建候选版本；产物目录已有清单时需另选输出目录：
make release VERSION=0.1.1
make verify-release VERSION=0.1.1
make verify-homebrew VERSION=0.1.1
# 有效身份与已有 notarytool keychain profile 准备好后，在干净 checkout 构建：
python3 scripts/build-release.py --version 0.1.1 --sign-identity 'Developer ID Application: …' --notary-profile teamcross
```

未提供 Developer ID 身份时使用 ad-hoc 签名；签名与公证状态如实保留在构建清单，不作为版本名称。所有非 `-dev` 发布构建要求干净 checkout，重建 Web 后再次核对；已有版本产物不被静默替换。清单记录完整提交、版本、buildNumber、dirty、架构、系统下限与校验值。App 的 buildNumber 来自完整提交历史的提交数量，因此 GitHub release checkout 必须使用完整历史。脚本不保存凭据、不发布网络内容。首版无登录自启和自动更新，升级由原安装渠道处理；卸载不清理协作数据和工作目录。

## 命令入口的归属

DMG 安装者在菜单栏“命令行工具…”安装或移除启动器，默认位置为 `/usr/local/bin/teamcross`。只有安装或移除该入口可能需要系统授权；Core、MCP 和协作执行不提权。启动器用绝对路径调用 App 内的 CLI，保持参数原样传递。相关代码为 [cliinstall](../../../internal/cliinstall/cliinstall.go) 与 [App 菜单](../../../apps/macos/TeamCross.swift)。

`cli-status` 检查命令路径、来源、入口归属和 PATH；`install-cli`、`uninstall-cli` 不启动 Core。高级用户可用 `--cli-dir /absolute/bin` 指定目录，测试必须使用隔离目录。Finder 不继承终端 PATH，因此另外检查常见 Homebrew 命令位置。已有 Cask 指向当前 App 的链接可直接使用，不再注册第二个入口；其他可执行文件、符号链接、被修改的启动器均报告冲突。

安装器仅更新完整匹配自身格式的启动器；通过目录中的生命周期锁串行化安装，新入口使用原子创建，升级使用原子替换。移除操作不触及 App、协作数据或 shell 配置。用户直接把 App 放进废纸篓不会执行清理脚本，卸载 App 前应从菜单移除手动安装的命令。

Formula 与 Cask 均在安装时检查另一渠道的成功安装收据，验证覆盖两种安装顺序及拒绝后的回滚。Cask 只移除 Homebrew 自己管理的 App 与命令链接；Formula 只移除自己的二进制。切换渠道前用户先退出服务，再卸载旧渠道；Formula 升级后可能保留旧版本，切换到 Cask 应使用 `brew uninstall --formula --force teamcross` 移除所有已安装版本。设置页与 `doctor` 分别显示已安装和正在运行的版本；升级不自动中断活动协作。

## 启动、发现与退出

[service](../../../internal/service/service.go) 是 CLI、MCP 和 App 共用的生命周期入口。`teamcross` / `serve` 默认后台启动并打开页面；`serve --foreground` 用于开发。`join` 自动确保服务，`mcp` 仅在实际工具调用时启动服务且不打开浏览器。App 通过同一 helper 的 JSON 命令操作服务，不维护另一套协作实现。

数据目录规范化（含符号链接），`start.lock` 串行化并发启动，`core.lock` 覆盖完整服务生命周期。`connection.json` 以 0600 原子写入实例身份、PID、URL、版本、控制协议、数据目录与本机控制 token。健康检查不启动 Codex，核对身份与协议后才复用；连接失效可启动新实例，不兼容或身份不匹配则报告处理入口。

菜单栏外壳在创建图标之前，由 [AppInstance](../../../apps/macos/AppInstance.swift) 取得同一规范化数据目录的 `app.lock`。它与 `core.lock` 独立；锁文件不删除，文件描述符不传给 helper。不同数据目录可以保留独立 App，路径别名不能绕过去重。

取得锁的 App 建立按本机用户与数据目录区分的 [CFMessagePort](https://developer.apple.com/documentation/CoreFoundation/CFMessagePort) 接收入口。后来的副本将首页或 `teamcross://join` 请求直接交给已有 App，不广播或落盘邀请。接收方对请求 ID 去重并回复入队确认；确认不表示已加入协作，邀请仍进入原有预览流程。客户端忙时排队处理，通信暂时不可用时有界重试；超时提示用户使用已有入口，不创建第二个图标，也不停止 Core。

副本转交完成和启动失败使用仅退出外壳的路径；只有用户对主 App 执行“退出 Team Cross”才进入原有停止服务流程。异常退出后系统释放外壳锁，下次启动可重新取得锁并复用存活的 Core。Finder 再次打开已有 App 时处理 reopen 事件，正常打开协作空间。旧版 App 不具备此协调协议，升级前仍需先退出旧外壳；新版本不按名称强制结束未知旧进程。

默认 43210 占用时回退到动态 loopback 端口；显式端口冲突不回退。控制 API 必须使用本机 token，拒绝浏览器 Origin；不凭陈旧 PID 或程序名终止进程。`status --json` 查询状态；`doctor --json` 检查客户端和 MCP；`stop` 对活动协作要求 `--force`，并等待生命周期锁释放。

浏览器和终端关闭不影响 Core。App“退出 Team Cross”停止本机服务，活动协作先确认；B 退出只断开 B，A 的运行时不随之停止。Core 停止保留 fork、目录、代码和加入记录；恢复与邀请失效继续遵循既有生命周期规则。不自动重放模型写入，也不自动恢复已撤销的共享。

## 首次协作

A 选择来源、目录和连接方式，点击“创建并邀请”。连接方式第一版显式选择局域网或实验性 Tailcat，不做自动探测与降级。创建结果先持久化，分享失败保留该协作并跳转到详情重试分享，不重复 fork。启动不要求 `--repo`；此高级参数只设置本机辅助上下文。

邀请同时给出 `teamcross://join?invite=…`、原始 `tcx3.` 内容及安装指引。App 将邀请经带本机凭据的接口暂存，浏览器只携带随机 pending ID。暂存最多 32 项、10 分钟，仍受原邀请期限约束。邀请声明的名称、主机和 `lan|tailcat` 在预览显示，实际加入使用相同 TLS pin 验证；预览只做本地格式和版本校验，不联系远端、不启动模型。Tailcat 邀请还包含其地址及预共享密钥，安装指引和页面明确提醒用户把整份邀请码视为秘密。

B 加入后直接进入上下文。每 10 秒由 B Core 发送心跳，A 以 30 秒租期显示参与方在线；页面关闭不会停止心跳。主动离开清除在线与输入请求，租期过后不再展示陈旧请求。`request_input` / `cancel_input` 检查 epoch，只变更请求状态；输入仍由 A 明确交接。当前维持 A/B 产品模型，没有新增多参与方权限系统。

直接客户端以连接建立、成功读取或恢复对应 thread 区分 `connected` 和 `session_ready`。Desktop 仍需手动打开会话时如实说明。配置发现支持显式 CLI、安装 App 内的 CLI、PATH 与系统/用户 Applications；实际版本可诊断，实验 Desktop 接口不视为公开稳定合同。

MCP 配置保存稳定 opt/App 绝对路径。通过 Cask 命令链接调用时也解析回 App helper；不保存版本化 Cellar 路径或依赖 shell alias。个人 Codex 或 Claude Code 分别配置；Claude 使用 user 范围原生配置命令，遇到当前项目同名覆盖或禁用时提示处理。配置存在、独立 STDIO 协议探测和各 Provider 实际客户端工具调用分别记录；仅配置成功不代表旧客户端已重载工具。协议探测不启动模型，也不记录为实际客户端调用。

## 检查与相关规范

[安装验证](../../../scripts/verify-release.py) 验证校验文件、DMG 挂载/安装、CLI/App 版本一致与实例复用，并调用 [App 副本验证](../../../scripts/verify-app-instance.py)。后者从两个临时 App 副本通过真实 macOS 启动/URL/退出事件验证外壳去重、请求确认、卡住后的恢复及 Core 保留；邀请 helper 使用测试内容，Core 使用包内真实二进制，不打开浏览器或调用模型。[生命周期验证](../../../scripts/verify-lifecycle.py) 验证 Core 并发启动、符号链接路径、端口冲突、鉴权停止、MCP 延迟启动、崩溃恢复及数据保留。工程、真实客户端、浏览器和清理门槛继续按 [验证契约](validation/test-gates.md)。2026-09-15 的首次 [GitHub CI 与 unsigned RC 验证](validation/github-ci-release-2026-09-15.md) 已确认当次 `macos-15` arm64 托管 runner 的图形登录会话能完成 App 副本检查；该证据只对应记录中的提交与 runner，后续基础设施失败不能静默跳过或沿用旧结论。同机或托管 runner 不能证明两台 Mac LAN、跨网络 Tailcat 或强制 DERP，未使用 Developer ID 的候选产物不能证明公开安装或正式公证通过。

相关来源：[产品流程](product-flows.md) · [架构](architecture.md) · [协议](protocol.md) · [输入协调](decisions/input-and-sharing.md)。

本轮实际证据与未完成项见 [2026-09-11 v0.1.1 验收](validation/distribution-v0.1.1-2026-09-11.md)；[2026-09-10 验收](validation/onboarding-macos-2026-09-10.md) 保留为当时的开发包记录，其共存设计不再是当前分发规则。外部规范：[Apple 自定义 URL Scheme](https://developer.apple.com/documentation/xcode/defining-a-custom-url-scheme-for-your-app) · [Homebrew Cask Cookbook](https://docs.brew.sh/Cask-Cookbook)。
