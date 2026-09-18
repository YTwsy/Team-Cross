# 验证与验收导航

当前应执行的命令、测试文件及环境要求，以 [验证门槛](../../sources/validation/test-gates.md) 为准。这页用于快速选择证据，避免把某一层的成功扩大为完整产品验收。

| 证据 | 能支持的判断 |
| --- | --- |
| 构建、类型检查与静态检查 | 当前代码与资源可编译，未代替运行时验证 |
| 单元、集成与 race test | 覆盖对应自动化用例中的逻辑和并发路径，模拟运行时不代表真实模型 |
| 真实浏览器与截图 | 页面流程、状态和视觉表现，未代替原生客户端执行 |
| 专用 TUI/Desktop 与原生运行时 | 对应版本、入口和测试目录中的真实行为 |
| 同机两个 Core 或两个客户端 | 本机 A/B 流程，未证明跨设备网络 |
| 两台 Mac 的 LAN | 实际网络条件下执行过的协作路径 |

GitHub `CI` workflow 自动执行 Go/Web 与 Homebrew 定义工程门槛；unsigned release workflow 可以手动生成经完整安装与 Homebrew 检查的 arm64 正式版或 RC artifact，也可以从 `origin/main` 上的同版本 annotated tag 生成 provenance，并把 RC 发布为 Pre-release、正式版本发布为 Latest。Release 成功后，Homebrew workflow 从公开资产复核 checksum、manifest、attestation 与隔离安装，再以最小权限 GitHub App 创建独立 tap PR；tap 的受保护检查、自动合并和公共 `main` smoke 全部成功后才回写 Homebrew 已发布。稳定版使用无后缀定义，RC 使用显式独立定义。上述流程都不自动运行真实模型、Tailcat 公网 smoke 或两台 Mac 网络验收，也不把稳定版本、Latest、Homebrew 可安装或 artifact provenance 当作 Developer ID 签名或 Apple 公证。

## 已记录的验收

[2026-09-18 只读空间与材料](../../sources/validation/materials-2026-09-18.md) 记录范围冻结、固定版本、三 Core 发布与撤销隔离、MCP/CLI、WebGUI 浏览器流程及可选执行。来源为合成历史，运行时为测试替身；未扩大为真实模型、原生客户端或跨设备网络结论。

[2026-09-18 多成员基础](../../sources/validation/multi-member-2026-09-18.md) 记录独立邀请、具体成员输入归属、逐人撤销、跨成员去重及同机三个 Core 的浏览器检查。

[2026-09-16 个人 Agent 与 CLI](../../sources/validation/agent-cli-collaboration-2026-09-16.md) 记录输入管理入口、Core 统一检查、模拟 Provider 的双 Core/TLS 接力与工程回归；真实客户端与网络范围以记录内的实际覆盖为准。

[2026-09-16 v0.1.7-rc.1 发布](../../sources/validation/release-v0.1.7-rc.1-2026-09-16.md) 记录信任模式的 PR/main CI、干净候选安装、公开 arm64 CLI/DMG、tag provenance、受保护 Homebrew RC，以及 tap 检查登记竞态的恢复与回归。二进制来源固定为 `face795e`，不因后续自动化修正改变。

[2026-09-16 信任模式](../../sources/validation/trusted-runtime-2026-09-16.md) 记录 Codex / Claude 两种模式的真实 Luna TUI、个人 MCP 与 hook、权限与原生确认、同 session 恢复、53 项 Web 回归和浏览器尺寸/主题矩阵。属于同机双 Core；未覆盖两台 Mac、专用 Desktop 窗口或实际浏览器/电脑控制工具。

[2026-09-16 App 图标与 v0.1.6-rc.3 发布](../../sources/validation/app-icon-release-v0.1.6-rc.3-2026-09-16.md) 记录非透明主体范围、sRGB/alpha 规范化、传统 ICNS 打包、tag 与公开资产、严格 provenance、受保护 Homebrew RC 发布及 SHA-256。它没有完成真实 macOS 26 的 Finder/Dock/Launchpad 目视验收，也不证明 Developer ID 签名或公证。

[2026-09-15 GitHub CI、unsigned 发布与 v0.1.6-rc.1/v0.1.6-rc.2](../../sources/validation/github-ci-release-2026-09-15.md) 记录 PR/main 工程门槛、CI 发现并修复的 CLI 启动器并发竞态、正式版本号 gate 调整、两个 RC 的 tag 与公开 Pre-release，以及 `v0.1.6-rc.2` 的 GitHub App、受保护 tap PR、公共 Formula/Cask 安装、SHA-256 和 provenance 复核。它不证明 Developer ID 签名、公证、真实模型或两台 Mac 网络。

[2026-09-15 Tailcat 显式连接方式](../../sources/validation/tailcat-transport-2026-09-15.md) 记录 `lan|tailcat` 二选一、`tcx3`、Go 1.27.1、工程/race/Web 门槛、同机 Tailcat 数据面和 arm64 发布构建链。两台物理 Mac、不同网络、强制持续 DERP、正式签名公证与发布仍未覆盖。

[2026-09-14 Claude 个人 CLI/TUI 历史](../../sources/validation/claude-personal-history-2026-09-14.md) 记录原生 fork 的单文件个人 history 发布、`/resume` picker 实际可见、个人配置与其他历史隔离、原目录/worktree、审批交接、MCP 去重、中断及恢复；完整轮次 10 次 Luna 请求，属于同机两个 Core。未覆盖旧数据迁移、跨磁盘 fallback、OAuth/Keychain、Desktop/Web/Cloud 或两台 Mac LAN。

[2026-09-14 Codex 动态 MCP 与运行时恢复](../../sources/validation/codex-runtime-mcp-2026-09-14.md) 记录 CLI `0.154.0-alpha.6.2` 的插件动态 MCP 配置回归、隔离枚举修复、完整工程门槛和真实 Luna 创建/释放/同 session 恢复。属于同机两个 Core；未恢复用户原有协作或验证专用 Desktop 窗口。

[2026-09-13 个人 Desktop 定位 fork](../../sources/validation/personal-desktop-2026-09-13.md) 记录邀请者主动打开、同源与本机归属检查、不恢复运行时、48 项 Web 回归以及 6 种尺寸/主题组合。浏览器点击由真实 Core 处理，系统打开由测试替身记录；Desktop 实际窗口显示受电脑控制工具限制，未覆盖。

[2026-09-13 批注闭环](../../sources/validation/annotations-2026-09-13.md) 记录两个直接 TUI 的实际读取/回复/恢复、Codex MCP 隔离、WebGUI 内嵌编辑与单层回复、38 项 Web 回归和 6 种尺寸/主题组合。专用 Desktop 只确认进程和连接，窗口内工具调用仍未覆盖；属于同机两个 Core。

[2026-09-12 个人 Claude 辅助 MCP](../../sources/validation/claude-assist-2026-09-12.md) 记录个人 MCP 原生安装、实际 TUI 工具调用、A 上读写、交接、去重、访问撤销与浏览器按钮打开。属于同机两套隔离 Core；较新 CLI 仅验证最低版本逻辑。

[2026-09-12 Claude 原生 TUI](../../sources/validation/claude-native-tui-2026-09-12.md) 记录实验性单 worker 接入、原目录/worktree、原生审批交接、MCP 去重、中断、首次输入前恢复与 Core 异常恢复；完整轮次 10 次 Luna 请求，属于同机两个 Core 验证。该记录对应旧的独立 `claude-home` / 离线 JSONL 实现，不证明当前个人 CLI/TUI history 路径。

[2026-09-11 原处批注](../../sources/validation/webgui-annotations-2026-09-11.md) 记录对话选区、代码行、原文快照、MCP 往返和变化后的定位边界，Web 28 项回归与 6 种尺寸/主题组合；使用模拟协作数据，不包含真实模型理解评测或两台 Mac LAN。

[2026-09-11 菜单栏 App 跨副本去重](../../sources/validation/app-instance-2026-09-11.md) 记录两个真实 App 副本的并发、邀请转交、请求确认去重、无响应恢复、独立目录与退出边界，以及 DMG/CLI/Core 回归和本机修复；测试邀请未连接远端，未调用模型。

[2026-09-11 v0.1.1 分发](../../sources/validation/distribution-v0.1.1-2026-09-11.md) 记录 App 命令入口、Formula/Cask 双向互斥与失败回滚、升级保留数据、CLI 0.153.4 的真实 Luna 运行时回归及浏览器检查；原生菜单/系统批准、完整窗口矩阵和两台 Mac 仍未覆盖。

[2026-09-10 邀请与原生会话释放](../../sources/validation/membership-and-release-2026-09-10.md) 记录持续加入资格、失联恢复、空闲释放、真实原生写入锁与 A/B 网关交接；区分专用 Desktop 进程验证和仍未完成的窗口交互。

[2026-09-10 WebGUI 上下文刷新](../../sources/validation/webgui-context-refresh-2026-09-10.md) 记录刷新期间保留内容与滚动位置的回归、17 项前端测试、工程门槛和 12 种浏览器组合；使用模拟协作数据，不覆盖原生客户端或跨设备网络。

[2026-09-10 首次体验与 macOS 安装](../../sources/validation/onboarding-macos-2026-09-10.md) 记录 CLI/App/DMG、隔离 Homebrew 安装升级、服务生命周期、App 邀请链接、浏览器与两种 TUI 入口的当前结果，并明确保留原生菜单/Desktop、完整 GUI 矩阵和两台 Mac LAN 等未完成项。

[2026-09-09 原生协作验收](../../sources/validation/native-collaboration-2026-09-09.md) 记录初次重构和 main 迁移结果：同机独立 A/B、两种目录模式与四种客户端入口、审批、并行辅助、模型设置和恢复。真实调用均使用 Luna。

该记录明确保留两台 Mac 的 LAN、不同真实模型和其他 Desktop 全局功能的未覆盖边界。后续变更应按范围重新验证，不能仅引用历史“通过”结论。

## 交付时记录

分别报告工程检查、浏览器检查和真实客户端检查；标明实际版本、环境及未覆盖范围。真实模型只使用专用 Luna 会话和仓库；验证后精确关闭本次测试客户端、Core、app-server、浏览器和残留辅助进程。

仅整理文档时检查链接、旧引用、代码路径和索引可达性即可，不重复启动这些运行时。新增验收记录后同步本页与 [任务索引](../index.md)。

相关任务：[目录](workspace-and-lifecycle.md) · [原生客户端](native-clients-and-models.md) · [输入与共享](input-and-sharing.md) · [WebGUI/MCP](webgui-and-mcp.md)
