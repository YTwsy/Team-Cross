# Team Cross：**你的工具，就是协作的入口。**

Team Cross 是 macOS 上围绕 Coding Agent 会话展开的本地协作工具。带上各自的 Codex、Claude Code 会话材料，邀请同事一起阅读、引用和讨论；需要一起动手时，再创建新的原生协作会话，明确开放执行访问并交接输入。

继续使用熟悉的终端和原生客户端，也可以打开 WebGUI 查看共同上下文。协作空间与共享执行托管在发起者的 Mac，个人 Agent 通过 MCP 按需参与，各自保留自己的会话、模型与本地上下文。

[交互演示](https://teamcross.pages.dev/) · [下载 v0.2.4 正式版](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.4) · [使用指南](docs/user-guide.md) · [文档导航](docs/README.md)
<img width="2400" height="1500" alt="Codex 图像 2026年9月23日 23_34_56" src="https://github.com/user-attachments/assets/ca8316f5-f7a3-441f-a2a9-317822355187" />

## 从一次具体工作开始

“能帮我看一下这个 Session 吗？”可以先从一段调查开始，也可以请同事接着处理手头的工作。Team Cross 支持三人及以上参与同一个空间，每个人有独立身份，可以贡献多份材料。

| 你想做什么 | 如何参与 |
| --- | --- |
| 看一眼，提意见 | 阅读同事选择公开的内容，在原文旁批注与回复，无需取得输入权 |
| 带来我的调查 | 从自己的会话中选一段，预览后发布到同一空间，讨论引用具体版本 |
| 带上我的 Agent | 让个人 Codex 或 Claude Code 通过 MCP 按需读取材料、核对依据并回复讨论 |
| 接过输入继续 | 获得执行访问并明确交接输入后，在对应原生客户端操作共享会话，执行留在发起者主机 |

例如，你分享一次登录问题的定位过程，同事补充复现记录，另一位同事带来项目约定。大家引用这些材料讨论，让各自的 Agent 帮忙核对；确定修改方案后，再由一位同事接过输入完成修改和验证。

## 安装

当前正式版本为 **`v0.2.4`**。以下方式选择一种：

| 方式 | 安装与打开 |
| --- | --- |
| 下载 App | 下载 [Apple Silicon DMG](https://github.com/YTwsy/Team-Cross/releases/download/v0.2.4/Team-Cross-0.2.4-arm64.dmg)，将 `Team Cross.app` 拖入“应用程序”并打开 |
| Homebrew App | `brew install --cask YTwsy/teamcross/team-cross`，打开 App 或运行 `teamcross` |
| Homebrew CLI | `brew install YTwsy/teamcross/teamcross`，然后运行 `teamcross` |

安装后运行不需要 Go、Node 或 pnpm。App 自带 CLI 和本地服务；DMG 用户可从菜单栏“命令行工具…”安装命令入口。阅读已发布材料无需先安装 Codex 或准备本地仓库。

当前安装包使用 ad-hoc 签名，未经 Developer ID 签名或 Apple 公证。首次打开若受到系统提示，先尝试打开 App，再按 [Apple 的说明](https://support.apple.com/zh-cn/102445) 在“系统设置 → 隐私与安全性”中允许打开。

升级前退出正在运行的 Team Cross，通过原渠道更新。安装程序保留协作数据与工作目录，但 **`v0.2.4` 不加载或迁移旧 `schema:2` 材料，旧数据仍保留在磁盘**。稳定版与 RC Homebrew 渠道分别维护，普通升级不会自动切换渠道；更换安装方式前应先卸载原渠道。

完整的切换步骤见 [安装与升级](docs/user-guide.md#安装与升级)，版本变化、CLI 下载、校验值和来源信息见 [Release](https://github.com/YTwsy/Team-Cross/releases/tag/v0.2.4)。

## 开始使用

打开菜单栏 App，或在终端运行：

```sh
teamcross
```

`teamcross` 与 `teamcross serve` 都会启动或复用本地服务，打开浏览器后返回终端。发起者需保持 Team Cross 运行，供同事连接；关闭浏览器不会停止服务。

### 先分享讨论

1. 点击“发起协作 → 先分享讨论”，搜索并选中自己的来源会话。
2. 在会话正文旁选择“从这轮开始”和“到这轮结束”，点击“预览分享内容”，核对选中范围的正文、工具过程和导出说明后创建只读空间。
3. 选择局域网或 Tailcat，生成邀请链接并发给同事。同一链接可供多位同事加入，大家都可以发布自己的材料、批注和回复。

只读分享不创建原生 fork，也不开放执行目录。后续对话不会自动公开，更新材料需要作者主动预览并发布新版本；已有批注和引用仍指向当时的版本。撤回可以停止后续读取，已经被读到的内容无法收回。

材料支持 Markdown、代码高亮、原文旁批注与固定版本引用。长工具输出按需展开，对话目录可以读取并定位尚未加载的轮次，阅读时可查看材料来源与公开范围。详见 [分享与审阅](docs/user-guide.md#分享与审阅) 和 [阅读与批注](docs/user-guide.md#阅读与批注)。

“资源库”可找回参与过的 Session 材料、批注和上下文，跨类型加入选择，生成一个本机读取编号后交给个人 Agent；同一空间的选择也能预览后发给共享会话。读取入口在底部内联显示，原有“设置与连接”入口保留。菜单栏左键或 `Control + Option + T` 打开速览，右键保留服务菜单。详见 [资源库与速览](docs/user-guide.md#资源库与速览)。

### 共同继续执行

1. 在已有空间选择“启用共同执行”，或从首页选择“发起协作 → 直接一起执行”。
2. 确认来源会话、执行目录和权限模式，创建新的原生协作 fork。可以沿用原目录，也可以从选定 `HEAD` 创建新 worktree。
3. 向指定成员开放执行访问，明确交接输入后，对方使用对应的原生 TUI 或 Codex 专用 Desktop 接着做，完成后交还输入。

启用执行保留原空间的链接、成员、材料和讨论。**开放执行访问会共享完整原生历史与工作目录，输入权另行交接**；原只读链接继续只授予材料和讨论访问。多人可以同时阅读讨论，共享执行同一时刻只有一位输入者。

原目录模式保留 Git 分支、暂存区和已有文件；新 worktree 从确认的 `HEAD` 检出，不复制未提交内容。结束共享保留会话、代码与工作目录，之后可以恢复同一个 fork。详见 [执行与目录选择](docs/user-guide.md#共同继续执行) 和 [结束与恢复](docs/user-guide.md#结束与恢复)。

### 收到邀请

先安装 Team Cross，再打开 `teamcross://` 邀请链接，或在“加入协作”中粘贴邀请码。核对协作名、主机和访问范围后加入。终端也可以运行：

```sh
teamcross join '收到的邀请码或完整邀请链接'
```

关闭或重置邀请链接不会移除已有成员；发起者可以单独撤销某位成员的访问。临时断线不等于离开，重连与退出规则见 [邀请与加入](docs/user-guide.md#邀请与加入)。

## 让自己的 Agent 参与

WebGUI 是可选入口。个人 Codex TUI/Desktop 或 Claude Code TUI 可以通过 Team Cross MCP，按你的指令发布材料、读取上下文并参与讨论；终端也提供对应 CLI 入口。

在“设置与连接”中选择 Codex 或 Claude Code，点击“接入本机”，已有客户端重新连接 MCP 或重新打开。也可以使用页面给出的完整路径执行配置命令，详见 [个人 Agent 与 MCP](docs/user-guide.md#个人-agent-与-mcp)。

接入后，可以这样提出请求：

> “列出这个空间的材料，阅读这条批注引用的版本，结合我们的项目约定核对一下，再回复到原批注。”

个人辅助 Agent 的会话与模型调用留在各自本机；发往共享会话的任务在发起者主机执行。保存批注不会自动向共享 Agent 发送任务，需要你明确让它读取和处理。

直接操作共享会话时，原生运行时已经提供当前空间的材料与批注工具，无需另外安装个人辅助 MCP。具体入口和操作差异见 [使用指南](docs/user-guide.md)。

## 当前支持范围

| 项目 | 范围 |
| --- | --- |
| 系统与分发 | Apple Silicon、macOS 14+；App / CLI；暂无 Intel 产物、自动更新或登录自启 |
| Codex 直接操作 | 原生 TUI、专用于协作的 Codex Desktop；个人 Desktop 与专用窗口用途不同 |
| Claude Code 直接操作 | 原生 TUI，实验性；补充、中断、审批与模型选择在原生 TUI 中处理，未接入 Claude Desktop |
| 个人 Agent 辅助 | Codex TUI/Desktop、Claude Code TUI 通过 MCP 参与，可辅助不同 Provider 的协作 |
| 局域网 | 参与者处于可互访的同一局域网，发起者保持服务运行 |
| Tailcat | 实验性；无需 Tailscale 账户或系统 TUN，可直连或经 DERP 中继，质量取决于网络与所用 DERP |
| 执行目录 | 普通 Git 仓库；沿用原目录或创建新 worktree，暂不支持含 submodule 的仓库 |
| 模型与推理强度 | 创建时继承原生来源，后续遵循当前输入者在原生客户端中的选择；产品不固定模型 |

共同执行默认采用**受限模式**。显式选择**信任模式**会沿用发起者的原生配置、工具和权限，工具可能访问工作目录之外的数据；模式创建后固定，恢复时沿用。具体差异见 [协作模式](docs/agent-wiki/sources/decisions/runtime-modes.md)。

局域网与 Tailcat 由用户明确选择，不自动切换。邀请包含访问凭据，应按秘密保管。原生客户端接口仍可能随版本变化，Desktop 的其他全局功能不在兼容承诺中。

已记录的同机验证不代表两台 Mac、跨网络 Tailcat 或持续 DERP 中继已完成验收；实际版本与覆盖范围见 [验收导航](docs/agent-wiki/wiki/concepts/validation-gates.md)。

## 产品方向

从一个具体 Session 出发，帮助另一个人理解、审阅或继续这次工作，同时保留各自熟悉的工作区和工具。

我们也在探索让托管机器上的 Agent 在需要人判断时自主发出邀请。这仍是产品方向；当前由用户明确发起分享、开放执行访问与交接输入。

## 开发与文档

从源码开发需要 Go 1.27.1+、Node 24+ 和 pnpm；构建 App 还需要 macOS Command Line Tools / Swift。

```sh
pnpm install
make build
./bin/teamcross serve
```

- [使用指南](docs/user-guide.md)：安装升级、CLI / MCP、阅读批注、输入交接与恢复。
- [开发与验证](docs/development.md)：本地开发、构建安装包、工程检查与真实模型测试入口。
- [Agent Wiki 索引](docs/agent-wiki/wiki/index.md)：产品决策、架构、协议与分版本验收记录；参与开发先读 [AGENTS.md](AGENTS.md)。
- [文档导航](docs/README.md)：按使用场景查找资料。
