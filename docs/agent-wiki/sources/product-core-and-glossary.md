# 产品核心与统一词汇

产品围绕一个动作组织：从来源会话分出协作，在明确的执行目录中继续，并把访问和输入交给同事。完整用户流程见 [产品流程](product-flows.md)。

## 词汇映射

| 产品用词 | 含义与代码字段 |
| --- | --- |
| 来源会话 | A 已有的所选 Provider 原生会话，保存为 `sourceId`；不是共享后继续写入的会话 |
| 来源起点 | 预览确认的已完成轮 `sourceTurnId`；Codex 传 `lastTurnId`，Claude 绑定已完成的来源快照 |
| 协作 | Team Cross 管理的记录 `Record`，有自己的 `id`，关联来源、fork、执行目录、命令状态与批注 |
| 协作会话 / fork | 创建出的新原生会话，保存为 `sessionId`；客户端切换和恢复继续该 ID |
| 原生 Thread | Codex 协议中的会话对象，RPC 使用 `threadId`；不是旧版 Team Cross 的 Thread 聚合模型 |
| 原生 Turn / 轮次 | Codex 一次输入后的运行过程；不是必须由用户创建或填写的 Round |
| 执行主机 | 持有共享运行时并执行代码的 A；B 的本机客户端与辅助会话不改变共享执行位置 |
| 执行目录 | `executionCwd`，包含正确的仓库子目录映射；区别于仓库根和 worktree 根 |
| 目录模式 | `workspaceMode: existing | worktree`，分别使用原目录或新建干净 worktree |
| 目录归属 | `workspaceOwned` 表示目录是否由 Team Cross 创建，不表示结束共享时可以删除它 |
| 邀请 | 绑定单一协作、仅用于首次加入的临时凭据；一小时内限一人使用 |
| 加入资格 | 主机接受加入后确认的独立访问凭据；持续到主动离开或本次共享结束，和在线状态、输入归属独立 |
| 输入者 | `writer: owner | remote`，决定当前谁能通过协作入口写入；不自动写入 Provider prompt |
| 输入交接版本 | `epoch`，用于拒绝已经过时的输入交接操作 |
| 请求 ID | `requestId`，用于区分并查询写入；RPC 响应与整轮执行完成不同 |
| 批注 | `Annotation`，可带引用的人类反馈，保存后不自动触发模型调用 |
| 模型设置 | 原生运行时确认的 `model`、`modelProvider`、`reasoningEffort`；表示会话配置，不是逐轮执行遥测 |

## 状态应分别理解

`state` 描述协作准备、就绪或失败，加入记录还可表示等待确认加入、已离开和结束；`online` 描述运行时连接；`busy` 描述轮次运行；`sharing` 描述远端访问是否开放；`connected` 描述直接客户端是否连接。`invitationState` 只描述首次加入；`runtimeState` 区分运行、释放和待恢复。不能用其中一个字段代替其他状态。

“创建协作”“恢复运行时”“开始一轮”“交出输入”“结束共享”是不同动作。读取和预览不发送业务指令；恢复不再 fork；结束共享不要求提交代码，也不删除目录。

## 产品边界

Codex 的直接操作与本地辅助可使用 TUI 或 Desktop；Claude 实验性直接入口与个人辅助入口均为原生 TUI，能力边界见 [Claude 接入契约](decisions/claude-native-tui.md)。“独立会话”指辅助者有自己的会话 ID 与本地上下文，不限定为某一种界面。辅助者通过工具访问共享协作，而不是先合并双方完整历史。

来源使用原目录时，创建操作保持 Git 现场；用户后续让协作 Agent 修改文件，则会直接修改原目录中的文件。新 worktree 只从确认的 HEAD 检出，不复制 staged、unstaged、untracked 或 ignored 内容。

不恢复旧版 Evidence、不可变 Round、managed Run、Observer/Controller 租约模型或专用成果出口。需要的新能力应在当前模型下明确设计，不因历史文档中存在就默认启用。

## 维护入口

字段以 [协作类型](../../../internal/collab/types.go) 和 [接口说明](protocol.md) 为准。修改用词时同步 [用户流程](product-flows.md)、[产品模型任务页](../wiki/concepts/product-model-and-glossary.md) 与面向用户的文案。
