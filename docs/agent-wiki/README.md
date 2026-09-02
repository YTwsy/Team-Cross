# Team Cross Agent Wiki

这个目录是 Team Cross 面向 coding agent 的稳定知识层。它不是终端用户手册，也不替代
协议规范；目标是让未来 Agent 从经过整理的项目上下文开始工作，不必每次都从全仓库
重新推导同一套边界。

当前采用手工维护，同时让目录形状兼容未来的 wiki compiler、审查和上下文包工作流。

## 目录结构

```text
docs/agent-wiki/
├── README.md
├── sources/
│   ├── project-brief.md
│   ├── decisions/
│   │   ├── collaboration-and-control.md
│   │   ├── git-isolation-and-export.md
│   │   ├── immutable-rounds-and-events.md
│   │   ├── share-and-transport-selection.md
│   │   └── managed-agent-sessions.md
│   └── validation/
│       └── test-gates.md
└── wiki/
    ├── index.md
    └── concepts/
        ├── runtime-architecture.md
        ├── git-capture-and-isolation.md
        ├── share-and-transport.md
        ├── collaboration-and-control.md
        ├── agent-bridge-and-switching.md
        └── validation-gates.md
```

`sources/` 保存稳定来源材料：项目简报、明确的架构决策和验证契约。它们应说明决策、
约束、影响以及何时才值得重新打开。

`wiki/` 保存已经整理的上下文页面：短小、互相链接，并适合 Agent 在相关任务开始前
优先阅读。代码、测试和 `docs/protocol.md` 仍是具体实现与 wire contract 的事实来源。

未来 compiler 的本地状态应放在 `docs/agent-wiki/.llmwiki/`，不要提交到 Git。

## 什么时候更新

当变化会影响未来 Agent 的工程判断时，更新或新增知识：

- Thread、Round、Event 或 evidence 的生命周期语义变化；
- Git capture、worktree 隔离或 patch export 的边界变化；
- 邀请、Share API、连接顺序或失败语义变化；
- Observer/Controller/Owner 权限、租约或命令 fencing 变化；
- Agent Adapter、sandbox、Provider 切换或历史导入语义变化；
- 自动化门槛或两台 Mac 验收信号变化；
- 产品方向变化，并会影响实现取舍。

不要写入一次性日志、临时命令输出、未经验证的猜测、普通 TODO、账户凭据或真实邀请。

## 手工刷新流程

1. 确认变化属于可复用知识，而不是一次性实现细节。
2. 更新对应 `sources/`，记录新的稳定事实或决策。
3. 更新 `wiki/concepts/` 中供未来 Agent 阅读的任务上下文。
4. 检查 `wiki/index.md` 与根 `AGENTS.md` 的导航仍然准确。
5. 在同一提交中运行与该边界对应的验证门槛。

如果未来接入自动 compiler，`sources/` 继续作为来源材料，`wiki/` 继续作为人工审查后的
输出，`.llmwiki/` 保存本地编译状态。
