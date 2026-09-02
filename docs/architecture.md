# Team Cross 产品与架构图解

本文保存当前 v0 的三张核心图：已实现闭环、技术分层和 `collaborate` 实时协作时序。
它们用于快速建立共同心智模型；协议字段和失败语义以 `docs/protocol.md` 为准，具体实现
仍以代码与测试为最终事实来源。

Team Cross 的长期产品入口是一个具体 coding-agent Session；当前 v0 则先从 Git capture
创建 Thread，再在隔离 worktree 中创建 managed Session。两者不能被写成同一项已实现
能力。Session、Thread、Run、Turn 与 Round 的规范语义和目标能力阶梯见
[产品模型与统一词汇](agent-wiki/wiki/concepts/product-model-and-glossary.md)。

## 当前 v0 闭环

这张图只描述当前已经实现的路径：一次交接从本地 dirty state 进入隔离 Thread，再由
另一位协作者批注或驱动主机 Agent，最后形成下一轮不可变快照。整个过程中，原始
checkout 不被自动写回。

```mermaid
flowchart LR
    A[开发者 A 的原始 checkout] -->|capture| B[Round 0<br/>基线 + dirty diff + 交接说明]
    B --> C[隔离 Thread worktree]
    C --> D[临时 Share 邀请]
    D --> E[开发者 B 的 join proxy + WebGUI]
    E -->|查看与批注| F[Thread 时间线]
    E -->|取得 Controller 租约| G[主机托管 Codex / Claude]
    G -->|消息、工具事件、文件变化| F
    G -->|只写隔离目录| C
    F -->|Turn 完成或 Provider 切换| H[Round N<br/>summary + patch + evidence 引用]
    H -->|继续协作| E
    H -->|查看或下载 patch| A
    C -. 不自动 apply、commit 或 cherry-pick .-> A
```

关键边界：

- Share 传递的是受限 Thread 能力，不是 Shell 或整台主机权限。
- 接收者默认是 Observer；只有当前 Controller 能发送 Agent 控制命令。
- 新 Round 追加到历史，不覆盖旧 Round。
- 用户采纳结果必须是后续显式操作。

## 技术分层

这张图展示不同运行时为何同时存在，以及远端访问如何与本地管理员能力隔离。

```mermaid
flowchart TB
    subgraph UI[控制面]
        HO[主机浏览器]
        RO[接收者浏览器]
    end

    subgraph ENTRY[本地与远端入口]
        ADMIN[Loopback 管理员 API]
        PROXY[接收者 loopback join proxy]
        ROUTE[接收端拨号器<br/>LAN → Tailnet → Tailcat]
        SHARE[单 Thread Share API<br/>临时 TLS listener]
    end

    subgraph CORE[Go Core]
        HTTP[REST + SSE]
        MODEL[Thread / Round / Event<br/>Lease / Command]
        GIT[Git capture / worktree / patch]
    end

    subgraph STATE[持久状态]
        DB[(SQLite)]
        CAS[(Content-addressed objects)]
        WT[(隔离 worktree)]
    end

    subgraph AGENT[Node Agent Bridge]
        RPC[JSONL-RPC stdio]
        CODEX[Codex app-server]
        CLAUDE[Claude Agent SDK]
        MOCK[Mock Adapter]
    end

    HO --> ADMIN
    RO --> PROXY
    PROXY --> ROUTE
    ROUTE --> SHARE
    ADMIN --> HTTP
    SHARE --> HTTP
    HTTP --> MODEL
    MODEL --> DB
    MODEL --> CAS
    MODEL --> GIT
    GIT --> WT
    MODEL <--> RPC
    RPC --> CODEX
    RPC --> CLAUDE
    RPC --> MOCK
    CODEX --> WT
    CLAUDE --> WT
    MOCK --> WT
```

分层原则：

- WebGUI 负责呈现和发起动作，不保存第二套 durable state。
- Go Core 是 Thread、权限、幂等、连接选择和恢复语义的唯一协调层。
- Agent Bridge 只做 Provider 协议适配，不监听协作网络。
- Share API 只暴露绑定 Thread 的能力，管理员 API 始终只在 loopback。

## `collaborate` 实时协作时序

这张图聚焦接收者从加入、观察到取得控制，再发送一条可安全重放的 Agent 命令。相同
`commandId` 的完全相同请求会返回持久化结果；新的或语义不同的请求仍要通过 revision
与 lease epoch 检查。

```mermaid
sequenceDiagram
    autonumber
    actor Owner as 主机 Owner
    actor Receiver as 接收者
    participant UI as 接收者 WebGUI
    participant Proxy as join proxy
    participant Core as Share API / Go Core
    participant Store as SQLite
    participant Bridge as Agent Bridge
    participant Agent as Codex / Claude

    Receiver->>UI: 打开本机 join 页面
    UI->>Proxy: 获取 Thread snapshot 与 SSE
    Proxy->>Core: pinned TLS + secret + participant identity
    Note over Proxy,Core: Proxy 校验 SPKI；Share gate 校验版本、到期、撤销与 secret
    Core->>Store: upsert participant 并读取 Thread
    Store-->>Core: Thread revision + event cursor
    Core-->>Proxy: snapshot + SSE stream
    Proxy-->>UI: Observer 视图

    Receiver->>UI: 请求控制
    UI->>Proxy: control request(commandId, revision)
    Proxy->>Core: 转发受认证请求
    Core->>Store: claim command + 获取 60 秒 lease
    Store-->>Core: leaseEpoch
    Core-->>Proxy: Controller 状态
    Proxy-->>UI: Controller 状态

    Receiver->>UI: Send / Steer / Interrupt
    UI->>Proxy: commandId + expectedRevision + leaseEpoch
    Proxy->>Core: 转发命令
    Core->>Store: lookup commandId + participant + 完整请求
    alt 完全相同的已完成重放
        Store-->>Core: 持久化结果
        Core-->>Proxy: 返回当前 Thread 状态
        Proxy-->>UI: 返回当前 Thread 状态
    else 首次执行
        Core->>Store: 校验 revision 与 lease fencing
        Core->>Store: claim commandId
        Core->>Store: 追加 command event 并推进 revision
        Core->>Bridge: JSONL-RPC runs.*
        Bridge->>Agent: 驱动主机 managed Session
        Agent-->>Bridge: message / tool / file / status event
        Bridge-->>Core: 统一事件
        Core->>Store: 持久化事件与命令结果
        Core-->>Proxy: SSE 增量 + 更新后的 Thread
        Proxy-->>UI: SSE 增量 + 更新后的 Thread
    end

    loop 每 20 秒
        UI->>Proxy: renew 当前 lease
        Proxy->>Core: 转发 renew
        Core->>Store: 校验 participant + epoch + expiry
    end

    Owner->>Core: reclaim control
    Core->>Store: 提升 epoch 并撤销远端租约
    Core-->>Proxy: control revoked event
    Proxy-->>UI: control revoked event
    UI->>Proxy: 延迟到达的旧 epoch 命令
    Proxy->>Core: 转发旧命令
    Core-->>Proxy: 拒绝 stale lease fence
    Proxy-->>UI: 显示控制权已失效
```

这个时序只表示 Team Cross 自己托管的 managed Session。已有 Codex 或 Claude 历史只能
作为带来源、不可信的 evidence 导入，不能被远端热接管。
