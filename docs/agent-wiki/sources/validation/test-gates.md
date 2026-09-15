# 验证门槛

本页说明变更后应运行什么，以及每种检查能支持什么结论。指定版本的实际结果见 [2026-09-10 安装验收](onboarding-macos-2026-09-10.md) 与 [2026-09-09 原生协作验收](native-collaboration-2026-09-09.md)，这些记录不是后续提交自动通过的证明。

## 按变更选择检查

| 变更范围 | 应完成的检查 |
| --- | --- |
| 仅 Markdown、导航或文档迁移 | 相对链接、代码路径、旧引用、索引可达性与 `git diff --check`；不需要启动模型或测试客户端 |
| 产品代码 | 下列 Go 与 Web 工程门槛，按修改范围补相关回归 |
| Git 预览、目录创建或恢复 | 原目录分支/HEAD/暂存区/文件不变；worktree 不复制四类未提交内容；子目录映射、新会话 ID 与来源关联 |
| 输入协调、共享或进程生命周期 | 相关 race test，以及直接连接/MCP 共用输入者、重复请求、审批、断线、结束访问和恢复 |
| LAN / Tailcat 传输或邀请格式 | 邀请互斥字段、TLS pin、一次性加入、成员重连、取消与资源关闭；Tailcat 另跑显式联网测试，并把同机、两台 Mac 直连和强制 DERP 分开报告 |
| 原生协议、客户端启动或账户路由 | 除协议测试外，使用专门会话实测对应 TUI/Desktop；分别验证登录、历史、输入、审批和 A 上执行 |
| 模型与推理设置 | 来源继承、客户端选择、失败不更新显示、通知、恢复与写入归属；真实调用仅使用 Luna |
| WebGUI | 类型检查、交互测试、production build，更新嵌入式资源，并进行实际浏览器和截图复核 |
| 菜单栏 App 启动、转交或退出 | Swift 检查和构建；在图形登录会话执行真实双副本并发、数据目录别名、邀请转交与确认重试、无响应恢复、独立目录、异常退出后复用 Core、显式退出停止服务 |

## 工程命令

在仓库根目录运行：

```sh
go test ./...
go vet ./...
go test -race ./internal/collab ./internal/mcp ./internal/sharing ./internal/nativecodex ./internal/nativeclaude ./internal/service ./internal/cliinstall
pnpm --filter @teamcross/web check
pnpm --filter @teamcross/web test
pnpm --filter @teamcross/web build
go build -o bin/teamcross ./cmd/teamcross
```

修改 `packages/web/src/` 后，提交重新生成的 `internal/webassets/dist/`。纯文档修改无需重建这些产物。若沙箱阻止 Go 缓存写入，可将 `GOCACHE`、`GOMODCACHE` 指向 `/private/tmp` 下的任务专用目录。

旧 Node Agent Bridge 已移除，不再运行或恢复旧 Bridge 的 check/test/build 门槛。

## 现有自动化入口

| 文件 | 重点覆盖 |
| --- | --- |
| [CI workflow](../../../../.github/workflows/ci.yml) | PR/main 的 Go test/vet、相关 race test、Web check/test/build、嵌入资源一致性和 Darwin arm64 CLI 交叉编译；不运行真实模型或 Tailcat 联网测试 |
| [Unsigned release workflow](../../../../.github/workflows/release-unsigned.yml) | 手动 arm64 正式版/RC 构建，以及 annotated tag 对应的 Latest/Pre-release；校验 `origin/main` 来源、完整 release/Homebrew 验证、清单、SHA-256 和 artifact provenance；不代表 Developer ID 签名、公证或两台 Mac 网络验收 |
| [Homebrew publish workflow](../../../../.github/workflows/homebrew-publish.yml) | 只处理已存在的公开 Release；重新验证 tag/main 来源、公开 checksum、manifest、attestation 与对应渠道隔离安装，通过最小权限 GitHub App 创建 tap PR，等待受保护合并及公共 tap push smoke 后回写 Release；不直接修改 tap `main` |
| [workspace_test.go](../../../../internal/workspace/workspace_test.go) | 两种目录模式、Git 现场保留、文件路径范围 |
| [collab_test.go](../../../../internal/collab/collab_test.go) | 创建恢复不发送 prompt、输入归属、去重、审批、原生与工具并行、访问范围、本机登录路由 |
| [lifecycle_test.go](../../../../internal/collab/lifecycle_test.go) / [membership_test.go](../../../../internal/sharing/membership_test.go) | 首次加入期限、持久成员、丢失响应、主动离开、空闲释放、并发请求和旧连接隔离 |
| [invitation_test.go](../../../../internal/sharing/invitation_test.go) / [tailcat_test.go](../../../../internal/sharing/tailcat_test.go) | `tcx3` 传输字段互斥、Tailcat 地址与精确版本检查、回调 listener 关闭并发 |
| [model_test.go](../../../../internal/collab/model_test.go) | 模型继承、设置更新、拒绝请求、恢复与通知 |
| [process_test.go](../../../../internal/nativecodex/process_test.go) | 客户端配置与启动不强制模型 |
| [server_test.go](../../../../internal/mcp/server_test.go) | STDIO 读取不发送输入，保留输入文本与请求 ID |
| [service_test.go](../../../../internal/service/service_test.go) / [onboarding_test.go](../../../../internal/collab/onboarding_test.go) | 实例身份、控制协议、稳定 opt 路径、邀请预览、Core 心跳与输入申请 |
| [cliinstall_test.go](../../../../internal/cliinstall/cliinstall_test.go) | 参数与带引号路径、未知命令保护、并发安装、重复移除和 App 更新后的命令行为 |
| [verify-release.py](../../../../scripts/verify-release.py) / [verify-homebrew.py](../../../../scripts/verify-homebrew.py) | CLI/App/DMG、App 命令安装、稳定/RC 独立 Homebrew 定义、临时前缀安装、同渠道双向互斥、升级与卸载；`--public` 另要求通过公开 Release URL 安装 |
| [verify-app-instance.py](../../../../scripts/verify-app-instance.py) | 两个真实 App 副本与 macOS URL/退出事件；隔离 Core、请求去重、路径别名、无响应与异常退出恢复；测试邀请不联系远端或调用模型 |
| [flows.test.tsx](../../../../packages/web/src/test/flows.test.tsx) | 首页、创建、邀请失败、输入交接状态与模型显示 |
| [annotation_reply_test.go](../../../../internal/collab/annotation_reply_test.go) / [runtime_test.go](../../../../internal/mcp/runtime_test.go) / [annotations.test.tsx](../../../../packages/web/src/test/annotations.test.tsx) | 单层回复、去重、并发快照、访问撤销、受限运行时工具、内嵌草稿与键盘保存 |

GitHub 托管 `macos-15` arm64 完整候选、正式版本号路径与 `v0.1.6-rc.1` 发布的实际结果、来源提交、校验值、attestation 与未覆盖边界见 [2026-09-15 GitHub CI、unsigned 发布与 v0.1.6-rc.1 验证](github-ci-release-2026-09-15.md)。

## 真实模型和客户端

[TestLiveCodex](../../../../internal/collab/live_test.go) 默认跳过。显式选择一个新的专用测试目录后运行：

```sh
TEAMCROSS_LIVE_DIR=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveCodex$' -v -count=1 -timeout=7m
```

该用例建立测试仓库、来源和两个 fork，执行证明文件操作，并验证结束共享后独立原生进程可取得写入锁、只读不会重新占用、恢复保留同一 ID。全部真实模型调用限定 `gpt-5.6-luna`；不要把测试配置写入产品默认值。模拟模型名称只能证明转发、状态和拒绝语义，不能证明其他模型实际可用。

完成上述 fixture 后，可不发送 prompt 地检查真实 A/B 原生网关交接：

```sh
TEAMCROSS_LIVE_EXISTING_FIXTURE=/private/tmp/teamcross-fresh-fixture \
  go test ./internal/collab -run '^TestLiveNativeHandoff$' -v -count=1 -timeout=1m
```

该检查只接受 `TestLiveCodex` 生成的专用 manifest，不能指向普通用户会话。当前邀请与释放结果见 [2026-09-10 验收](membership-and-release-2026-09-10.md)。

原目录与新 worktree 分别覆盖直接 TUI、直接 Desktop、辅助 TUI、辅助 Desktop，共八种组合。实测核对执行主机、目录、fork、读写结果、审批、输入交接、并行辅助、客户端切换、重连、邀请失效和结束访问。

## LAN 与 Tailcat 网络门槛

[共享层 Tailcat 联网测试](../../../../internal/sharing/tailcat_live_test.go) 和 [协作层 Tailcat 联网测试](../../../../internal/collab/tailcat_live_test.go) 默认跳过。它们会访问 Tailcat DERP；只能在明确允许联网且能初始化系统网络监视的测试主机运行：

```sh
TEAMCROSS_TEST_TAILCAT=1 \
  go test ./internal/sharing ./internal/collab \
  -run '^TestLiveTailcat(Membership|Collaboration)$' \
  -v -count=1 -timeout=100s
```

共享层覆盖 Tailcat Server/Client、TLS 1.3 SPKI pin、一次性加入和成员凭据重连；协作层再覆盖显式选择、状态、输入交接与直接客户端 WebSocket 桥接。两项在同一台 Mac 上运行，日志中出现 DERP 引导或随后出现 `via=direct` 只说明该次本机路径，不能证明两台物理 Mac、不同 NAT、受限 UDP 或持续 DERP 中继。

跨设备必须分别完成并记录：

1. 两台 Mac 同一 LAN，显式选择 `lan`，不使用 `--test-loopback`。
2. 两台 Mac 处于不同网络，显式选择 `tailcat`，覆盖加入、状态、直接客户端、输入交接、断线重连、离开和结束共享。
3. 阻断点对点 UDP 或使用受控 DERP，证明业务流量保持经 DERP；必须依据明确路径诊断，不能仅凭连接成功或启动时连接过 DERP 推断。

每项记录 Team Cross 提交、Go/Tailcat 版本、macOS 版本、网络条件、DERP 来源、可观察路径及清理结果。公共 DERP 可用性与性能不属于一次测试可长期保证的产品承诺。

## 界面与测试收尾

页面检查包括首页、创建、详情、加入、设置和关键失败状态；在 1440、1024、768 CSS 像素及深浅主题下检查布局。核对实际视口、长路径、水平溢出、滚动、焦点、键盘和主要动作可达性，并实际查看截图。

完成后关闭本次启动的专用 Desktop/TUI、Core、app-server、浏览器页面及其测试辅助进程。先确认 PID、父子关系或测试数据目录，再退出对应实例；保留用户正在使用的 Codex 和其他浏览器内容，不按程序名批量终止。测试会话、仓库和验收材料按本次约定保留。

新增实际验收记录应注明日期、版本、环境、执行的检查及未覆盖范围；同步 [验收导航](../../wiki/concepts/validation-gates.md)。

## Claude 原生 TUI

Claude 的 Go 回归位于 `internal/nativeclaude` 与 `internal/collab/claude_test.go`；覆盖历史链、过期起点、原生 fork 参数、只复制所选来源、只发布新 fork 的单文件个人 history 入口、个人 settings/全局状态/认证/插件/来源及其他历史不被 Team Cross 改写、协作运行配置隔离、精确 job 停止、终端白名单、取消、断线不重放、TLS 成员访问、输入收回、就绪判断和不支持的 API。

真实验收使用 [verify-claude-native.py](../../../../scripts/verify-claude-native.py)，显式传入一个新的空测试目录、当前构建 CLI 和不低于 `2.1.268` 的 Claude 正式版（实际运行版本写入验收记录）：

```sh
python3 scripts/verify-claude-native.py \
  --fixture-dir /private/tmp/teamcross-claude-fresh-fixture \
  --teamcross-bin /absolute/path/to/teamcross \
  --claude-bin /absolute/path/to/claude
```

脚本只使用本机 CliProxyAPI 中的 `gpt-5.6-luna`。凭据仅在 loopback guard 内存中使用，测试配置写入占位 token；所有实际模型请求逐个检查模型名。脚本建立专用个人 Claude home、额外个人历史哨兵、来源、原目录/worktree 原生 fork、两个真实 Core 与原生 TUI；校验 runtime 只复制所选来源、fork transcript 以单文件进入 A 的个人 history 并出现在原生 `/resume` picker、零输入不以 JSONL 落盘为创建门槛、个人 settings/插件标记/来源/其他历史不变、结束时只停止本次 job，以及执行端、审批交接、发送、输入后恢复与清理。`--keep-preview-seconds` 可暂留测试 Core 供页面复核，创建测试目录下的 `finish-preview` 可提前结束。此脚本属于同机验证，不能代替真实双 Mac LAN 验收。

## 个人 Claude 辅助 MCP

使用 [verify-claude-assist.py](../../../../scripts/verify-claude-assist.py)；参数形式与原生脚本相同，使用新的空测试目录。脚本运行真实 `claude mcp add --scope user`，验证重复配置与其他条目保留，然后通过普通个人 TUI 加载用户配置。禁止用显式 `--mcp-config` 注入替代个人配置验收。

门槛包括：工具列举、A 上文件/历史/批注读取、非输入者发送拒绝、交接后发送、相同请求 ID 去重、个人与共享会话分离、同一 worker 的直接 TUI 接入、结束共享后工具访问撤销。测试可为明确列出的 Team Cross 工具设置允许清单，不使用全局跳过权限；个人 TUI 不授予本地 shell/file 工具。所有模型请求继续只允许 CliProxyAPI 的 `gpt-5.6-luna`。

UI 按钮打开另需可访问 macOS 桌面自动化的本机 Core；沙箱中的 `osascript` 失败不能当成产品按钮成功。协议检查、实际客户端工具调用、启动请求与进程/原生 TUI 就绪分别记录。关闭 PTY 后再等待客户端退出，并独立清理每个测试资源，某个 TUI 退出异常不能跳过其他 Core 和验收材料保存。

配置与数值版本回归见 [mcp_test.go](../../../../internal/nativeclaude/mcp_test.go)，跨 Provider 辅助与 Codex 控制能力回归见 [assist_test.go](../../../../internal/collab/assist_test.go)。

## 共享原生批注工具

使用 [verify-annotations.py](../../../../scripts/verify-annotations.py)，分别为 Codex 和 Claude 选择新的空测试目录：

```sh
python3 scripts/verify-annotations.py \
  --provider codex \
  --fixture-dir /private/tmp/teamcross-annotations-fresh-codex \
  --teamcross-bin /absolute/path/to/teamcross \
  --codex-bin /absolute/path/to/codex

python3 scripts/verify-annotations.py \
  --provider claude \
  --fixture-dir /private/tmp/teamcross-annotations-fresh-claude \
  --teamcross-bin /absolute/path/to/teamcross \
  --claude-bin /absolute/path/to/claude
```

脚本通过直接 TUI 读取只有批注与人工回复中才有的标记，再回复原批注，并验证结束共享后恢复同一会话仍能读取。真实模型只使用 Luna，Claude 沿用本页的 loopback guard；Codex 使用专用配置和已有登录凭据，不修改个人配置。检查继承的个人 MCP 被禁用；只接受本次批注工具对应的确认，不跳过所有审批。

`--keep-preview-seconds` 可暂留 fixture 供 WebGUI / 专用 Desktop 检查；写入 fixture 的 `finish-preview` 可提前结束。Desktop 须另外确认窗口内的实际读取与回复，进程启动或网关连接不等价于完成验收。当前结果见 [2026-09-13 批注验证](annotations-2026-09-13.md)。
