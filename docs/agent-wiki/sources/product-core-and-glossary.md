---
title: Team Cross 产品核心与统一词汇
kind: source
status: accepted
---

# Team Cross 产品核心与统一词汇

## 产品定义

> Team Cross 让团队在不改变既有工作区和工作流的前提下，安全地分享、审阅、临时移交或继续一个 coding-agent Session。

Team Cross 不替团队选择或重新设计工作区。团队可以使用 Lody、multica、其他协作系统或
自建仓库，也可以继续使用已有的任务、文档和沟通方式。Team Cross 只在不同工作流最终
都会触达的 coding-agent Session 层建立协作能力。

它从一个具体 Session 出发，为这次工作补充可共享、可批注、可控制和可接力的协作外壳。
原生 Agent UI 仍然可以是个人最熟悉的执行界面；Team Cross 负责跨参与者的共同视图、
来源记录、权限边界与交接历史，而不是重新实现一个完整 Agent IDE。

## 产品判断规则

> 一个很实用的产品判断规则：它是否从一个具体 Session 出发，并帮助另一个人理解、审阅、控制或继续这次工作？

如果一个功能不能通过这条规则，通常不应进入 Team Cross 的核心产品面。团队账户、任务
规划、知识库、仓库托管和通用远程桌面即使有价值，也不因此自动属于 Team Cross。

## 交互原则：双界面，单 Writer

Provider 原生 UI 是个人执行界面。用户可以继续使用自己的终端、快捷键、配置和 Codex
Desktop 或 Claude Code 操作习惯。Team Cross 是共享协作界面，负责呈现经过归一化的
Session 事件、代码状态、Evidence、Annotation、参与者与控制关系。

两种界面可以同时展示同一项工作，但不能同时向同一个 Run 写入。任意可写 Run 同一时刻
只能有一个 Writer；从原生 UI 切换到 Team Cross 控制，或再切回原生 UI，都必须经过明确
的 Handoff、fencing 和可审计状态转换。Team Cross 不通过复制完整终端或重做 Provider
全部 UI 来获得控制能力。

## 能力承诺

Team Cross 应按 Adapter 实际证明的 capability 渐进提供体验，而不是把所有 Session 都
描述成可以热接管：

1. 对可读取的 Session，至少可以捕获、分享、审阅和批注其有来源的上下文。
2. Provider 与本机 Adapter 支持实时事件时，可以 `Follow Session`。
3. 只有执行位置、权限、进程状态和 Writer 边界可验证时，才可以 `Take Control`。
4. 只有 Provider 支持且 Team Cross 已验证相同 Session 身份与 sandbox 时，才可以
   `Resume Session`。
5. 不能 Resume 时，仍可从不可变 Round 创建新的 managed Session，即
   `Continue from Round`，但 UI 必须明确这是新的 Session。

当前 v0 完整支持的是 Team Cross 创建的 managed Run；外部 Native Session 仍是带来源、
不可信的 Evidence，尚不支持 Attach、实时 Follow、Resume 或热接管。

## 要解决的问题

### 1. Session 难以被另一个人完整理解

Agent 对话、工具调用、代码变化、工作目录、Git 基线、外部日志和未决问题通常散落在
不同位置。单独转发 transcript 或 patch 会丢失它们之间的因果关系，接收者难以判断
“为什么这样改”“结论对应哪版代码”以及“下一步还缺什么”。

### 2. Session 被困在发起者的机器和界面里

接收者通常只能观看截屏、复制聊天记录，或者让发起者代为输入。团队缺少一个受限而明确
的途径来观察当前工作、提出定位准确的意见，并在必要时临时驱动同一个 Agent 工作过程。

### 3. 审阅意见与 Agent 工作历史脱节

普通聊天中的反馈很难绑定到具体 Round、事件、文件或代码行，也无法可靠表达反馈所依据
的上下文版本。Team Cross 需要让 Annotation 成为 Thread 历史的一部分，而不是另一条
难以追踪的旁路消息。

### 4. 控制与接力缺少安全边界

“让别人继续”不能等同于开放远程 Shell、共享整台机器，或允许多个人同时向 Agent 写入。
系统需要明确当前 Writer、Controller 租约、Owner 抢占、命令幂等、工作目录和 Provider
capability，并让权限变化可追溯。

### 5. “继续”包含几种完全不同的语义

继续同一个 Provider Session、从交接快照启动一个新 Session、切换 Provider，以及从某个
节点独立探索，具有不同的身份、上下文和安全条件。产品必须明确区分 `Resume Session`、
`Continue from Round`、Provider switch 与 `Fork Thread`，不能用一个含糊的“继续”按钮
掩盖差异。

### 6. 团队工作流天然异构

如果产品要求团队先迁移任务系统、仓库组织或协作习惯，接入成本就会抵消 Session 交接的
价值。Team Cross 应以 Adapter 和 capability discovery 接入不同 Agent 与机器，在已有
工作流旁边工作，而不是要求所有团队采用同一种工作区。

## 统一词汇

下列词汇同时约束产品文案、文档、API 和数据模型。Provider 自己使用 `thread`、`task`、
`conversation` 或 `session` 时，Adapter 应保留原始标识，并映射到这里的中立语义。

### Workspace

团队已经选择的代码仓库、任务系统、文档、沟通方式和机器环境。Workspace 属于外部世界，
Team Cross 与它正交；Team Cross 不把自己定义成新的团队 Workspace。

### Agent Session

由 Codex、Claude Code 等 Provider 创建并拥有的连续对话身份。它保存什么上下文、能否跨
进程或跨目录恢复，以及原生 ID 的生命周期，都由 Provider 决定。

- **Native Session**：用户从 Provider 原生 UI 或 CLI 创建和使用的 Agent Session。
- **Managed Session**：由 Team Cross 通过 Adapter 创建、且执行根目录和权限边界已知的
  Agent Session。

`native` 与 `managed` 描述 Session 的来源和控制关系，不改变 Session 仍由 Provider
实现这一事实。文档中存在歧义时不得裸用 `Session`。

### Thread

Team Cross 拥有的、长期存在且只能追加的协作单元。它围绕一个工作目标组织 Session、
Run、Round、Event、Evidence、Annotation、Share 和控制历史，是实际被分享、审阅与接力
的对象。

一个具体 Session 可以是 Thread 的起点，但 Thread 不等于 Session。一个 Thread 可以按
顺序连接多个 Session、Provider 或执行位置，而不丢失共同的交接历史。当前 v0 中，有
baseline commit 的 Thread 固定拥有一个隔离 worktree，所有 managed Session 顺序共享它。

### Run

一个 Agent Session 在某台机器、某个执行根目录和一组权限边界中的一次实际运行绑定。
Run 由 Team Cross 记录，至少需要知道 Provider、Session 引用、host、execution root、
运行模式、Writer、capability snapshot 和生命周期状态。

Session 表示 Provider 对话身份；Run 表示这段身份在 Team Cross 中何时、何地、以什么
能力运行。两者不得作为同义词使用。当前 v0 只允许 Team Cross 创建和控制 managed Run；
外部 Native Session 仍只可作为 evidence 导入。

### Turn

Run 接受一次用户或 Controller 输入后产生的一次 Agent 工作过程，从输入被接受开始，到
完成、中断或失败结束。一个 Turn 可以包含多条 Message、Tool call、文件变化和 input
request。

### Event

Thread 中追加写入的细粒度事实，例如 `turn.started`、`message.completed`、
`tool.completed`、`file.changed` 或控制权变化。Event 用于实时呈现、恢复和审计，不代表
一个完整交接快照。

### Round

Thread 在一个有意义的边界上封存的不可变交接快照。Round 可以绑定 Event 范围、Git
snapshot、patch、文件清单、Evidence 引用、summary、来源 Run 和未决问题。

Round 0 表示 Thread 创建时的初始状态。v0 可以在 Turn 完成或中断后自动封存 Round，但
`Turn` 与 `Round` 不是同一概念：Turn 是 Agent 执行过程，Round 是可供理解、审阅和接力
的稳定检查点。UI 应把 Round 表述为“交接快照”或 “checkpoint”，而不是“对话轮次”。

### Evidence

Git 无法表达或位于仓库之外的参考材料，例如日志、文本、文件、截图元数据和 imported
transcript。Evidence 必须保存来源，并默认视为不可信输入；导入 Session transcript 不等于
Attach、Resume 或控制该 Session。

### Annotation

参与者对 Thread、Round、Event、Evidence、文件或代码行留下的可定位意见。Annotation
属于协作历史，不修改已经封存的 Round，也不会自动变成 Agent 指令。

### Share

绑定单一 Thread 的临时访问能力。Share 定义参与者能观察、批注或请求控制的范围，但不是
Thread 副本、长期成员关系、机器登录凭据或远程 Shell 权限。Share 失效不删除 Thread。

### Handoff

以某个 Round 为明确起点，把后续工作交给另一位参与者、另一个执行位置或新的 Agent
Session 的过程。Handoff 是可追溯的状态转换，不是一份孤立的聊天摘要。

### Participant、Observer、Owner、Controller 与 Writer

- **Participant**：通过某个 Share 进入 Thread 协作面的临时参与者身份，不等于 Team
  Cross 账户或团队成员目录。
- **Observer**：可以查看和批注、但不能向 Agent Run 发送写命令的 Participant。
- **Owner**：主机侧最终授权者，可以撤销 Share 或抢占远端控制。
- **Controller**：在有效 Lease 内获准向当前可控 Run 发送命令的远端参与者。
- **Writer**：当前唯一可以实际向 Run 写入输入的界面或控制路径，可以是 Provider 原生
  UI，也可以是 Team Cross。

Controller 不等于主机管理员，取得 Control 也不代表获得仓库、Shell 或其他 Thread 的
访问权。任何可写 Run 同一时刻都只能有一个有效 Writer；Writer 切换必须显式发生并能被
审计。

### Control Lease 与 Command

- **Control Lease**：授予某个 Participant 的短期、独占远端控制资格。Lease 到期、被抢占
  或 epoch 变化后，旧命令不得继续生效。
- **Command**：通过 Team Cross 请求改变协作状态或驱动 Agent 的可审计操作。远端写命令
  必须携带 `commandId`、`expectedRevision` 和 `leaseEpoch`，并遵守幂等与 fencing 语义。

Command 是受限的 Agent 操作，不是任意 Shell 输入。

## 标准动作语义

- **Attach Session**：把已有 Native Session 与 Thread 建立可验证关联；不自动取得写权。
- **Follow Session**：只读取和呈现 Session 后续事件。
- **Take Control**：取得当前 Run 的 Agent 输入权，不是控制整台主机。
- **Resume Session**：在 Provider 明确支持且 Team Cross 已验证边界时，继续相同 Session
  ID。
- **Continue from Round**：以不可变 Round 为上下文创建新的 Session 和 Run。
- **Import Session**：把历史 transcript 作为 Evidence 保存，不建立活跃 Session 关联。
- **Switch Provider**：在同一 Thread 内封存 outgoing Round，并创建目标 Provider 的新
  Session 和 Run。
- **Fork Thread**：从某个 Round 创建独立的后续协作历史。
- **Open in Provider**：在 Codex、Claude Code 等原生 UI 中打开对应 Session；Thread 身份
  不因此改变。

实现或 UI 尚不具备某项动作所需 capability 时，应明确显示不可用或采用语义不同的
fallback，不得把 `Continue from Round` 描述成 `Resume Session`。

## 关系与不变量

```text
Thread
├── Agent Sessions
│   └── Runs
│       ├── Turns
│       └── Events
├── Round 0 ... Round N
├── Evidence / Annotations
└── Shares
```

1. 一个 Thread 可以跨越多个 Session，一个 Session 不等于一个 Thread。
2. 一个 Session 可以有顺序发生的多个 Run，但同一时刻最多只有一个有效 Writer。
3. 一个 Round 一旦封存便不可改变；后续工作只能追加新的 Event 与 Round。
4. Share 是 Thread 的临时访问能力，不是持久状态本身。
5. imported transcript 只是 Evidence，不证明原 Session 可 Attach、Resume 或接管。
6. 相同工作目标的接力留在同一 Thread；需要独立后续历史时，从 Round Fork 新 Thread。

## 非目标

Team Cross 不负责规定团队如何组织人员、任务、目标和仓库，也不试图替代：

- 团队 Workspace 或项目管理系统；
- Codex、Claude Code 等原生 Agent UI；
- Git 托管、PR、Issue 或知识库；
- 通用聊天工具；
- 远程桌面、远程 Shell 或整机管理；
- 默认常驻的云端 Agent fleet 与全局任务调度器。

新的产品能力只有在强化 Session 的理解、审阅、受限控制或继续时，才应进入核心产品面。
