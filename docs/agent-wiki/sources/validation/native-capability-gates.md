---
title: 原生 Session 能力验收契约
kind: validation
status: accepted
---

# 原生 Session 能力验收契约

Team Cross 的能力标记表示已集成并验证的产品能力，而不是 Provider 协议中出现了同名
方法。只读历史、实时 Follow、准确打开原生 UI、同会话 Resume 和独占控制必须分开判断。

## 已实现的只读边界

`sessions.snapshot` 只调用历史读取方法，生成结构化文本候选，由 Go Core 不可变保存。
Codex 使用 `thread/read`，遇到 `paginated` history 时使用 `thread/turns/list` 的完整项
分页；Claude 使用 `getSessionMessages` 和可用时的 `getSessionInfo`。这些路径不得创建
Run、Resume、Fork 或发送 Turn。未知、非文本、缺失及超限材料必须有可见标记。

Codex 原生对话身份以 `thread.id` 为准；`thread.sessionId` 是 Session tree root，Fork
可能与来源共享它，因此不能以相同 `thread.sessionId` 证明“同一会话”。原始来源也必须
保留；`appServer` 可由不同客户端创建，不能自动标成 Desktop。

## 分能力验收

| 能力 | 必须取得的证据 | 不足以证明的信号 |
| --- | --- | --- |
| Read | 指定身份的消息/工具映射、缺失标记、零执行调用链 | initialize 成功 |
| Follow | 原生端继续工作时被动收到或只读增量读取、去重、断线恢复和缺口标记 | `thread/read` 成功或刷新页面 |
| Open | 原生 UI 打开准确的原生对话 ID，且没有增加 Writer | 仅打开应用或项目目录 |
| Resume | 同一原生对话 ID 在隔离 worktree 恢复，并实际验证 cwd/sandbox | API 存在 `cwd` 参数或 Fork 成功 |
| Take Control | 旧原生输入失效、单 Writer fencing 持久化、崩溃恢复和交还 | 只有 Team Cross Controller 租约或原生状态 idle |

CLI 和 Desktop 单独执行上述验收；不能用 CLI 结果代替 Desktop。原生返回的
`canAcceptDirectInput` 或 `thread/unsubscribe` 不等同于对其他客户端的独占输入授权。

## 当前原生控制状态

2026-09-03 对本机 Codex CLI 0.152.1 进行了版本与生成协议定义的静态检查。可见
`thread/read`、历史分页和 `thread/resume`；Resume 有 cwd/runtime roots 覆盖项，且运行中
会话的 Resume 是重新加入该会话。协议形状本身没有证明 Team Cross 可以封锁原生 UI
的输入来源或完成 Desktop 交还，因此 `follow`、`open`、`resume`、`takeControl` 保持关闭。

这不是实际 Session/Turn 测试，也没有连接用户的活跃 daemon 或枚举个人会话。不得据此
声称 M3、M4 原生接管或任何真实 sandbox 行为已经验收。

后续真实测试必须显式 opt-in，并使用专用 repository、隔离 worktree 和专用原生 Session。
验收材料记录 CLI/Desktop 版本、实际原生 `thread.id`、每次 Run 的目录、输入来源与拒绝
旧 Writer 的证据，以及原 checkout 前后状态。若不能可靠关闭旧输入，停止启用该能力；
不得通过 UI 自动化、公开原始 app-server、复制凭据或创建新 Session 来替代安全门槛。

## 已实现但未启用的 Follow reader

`sessions.poll` 已提供限定到指定 Codex Session 的只读增量合同，Core 已实现持久游标、
不可变检查点、重试/重启重验能力、停止 fencing，WebGUI 可呈现并显式管理 reader。
测试覆盖初次尾部、边界更新、超过一页的补读、暂时断线、历史缺口、重置、重复结果与
长工具输出；它们使用合成 app-server/Provider 数据，不是 CLI/Desktop 原生验收。
Snapshot 与 Follow 的序列化结果另按实际 JSON 字节收口，测试覆盖大量需转义控制字符，
避免字符计数合格但单行超过 Bridge 通道限制。截断材料必须标记为不完整。

`bridge.info.methods` 出现 `sessions.poll` 只表示 RPC 合同可调用，不等于某来源的
`capabilities.follow=true`。本机初始检查也不再使用 `thread/list` 枚举一个无关 Session。
`Open in Provider` 仍禁用：仅证明 `codex app PATH`、应用注册 URL scheme 或静态路由
可解析，不能证明实际定位准确原生对话或不会产生第二个 Writer。

原生实时 Share 已有独立授权与不可变公开窗口合同，不改变上述能力门槛。它只允许已经
成功读取的活动 Follow，并限制到明确类别和当前预览；使用合成 reader 测试公开窗口、
批注和 SSE，不能据此把 CLI/Desktop 的 `follow` 标记打开。

Core 的 `sessions/open` HTTP 入口已实现但受 fresh `read/open` 能力限制。它只接受
Owner 明确确认，使用固定 bundle 和 UUID 链接，并拒绝历史 managed 同 ID 及未确认
Writer 状态。202 仅表示系统接受请求。合成 opener 测试不实际启动应用；此实现不能
替代真实 Open 验收，更不构成同一 Session 的安全交还。

## Desktop 深链的静态证据

2026-09-03 对本机 Codex Desktop 26.831.21537 / build 7579 的安装 bundle 做只读检查；
实际安装路径为 `/Applications/ChatGPT.app`，bundle ID 为 `com.openai.codex`。其
`Contents/Resources/app.asar` 内原生复制链接实现生成 `codex://threads/<thread.id>`，
主进程要求 `codex:` protocol、`threads` host 与 UUID 格式 conversationId；找到对应
Thread 后导航 `/local/<conversationId>`。这些证据比单纯注册 scheme 更具体，但不是
一次真实 Open 验收。

精确代码位置：`webview/assets/app-initial-b09f80199db1.js` 的 `cBi`（line 2845）；
`.vite/build/src-B8dS-jjl.js` 的 `GE/tD`（line 122）；
`.vite/build/main-7MZ5kTIG.js` 的 `localConversation` 分支（line 1485）。

外部 URL 解析不读取 `hostId` query，不能以追加该参数声称能准确定位远端机器。将来
生成链接只能带经验证的最小 UUID，不携带可触发额外行为的 prompt/browser/review 参数。
Renderer 同时含 `thread/resume` hydration 路径；尚未证明导航时是否触发，所以 Open
不能被当作已证明只读，managed Writer 存在时尤其不得直接启用。

本次没有启动应用、连接会话、读取个人 profile/历史或调用模型。能力仍为 `open:false`；
后续必须独立验证真实目标身份、cwd 和 Writer 边界。

## 可复现的静态检查

`packages/agent-bridge/scripts/probe-native-capabilities.mjs` 需要
`TEAMCROSS_NATIVE_CAPABILITY_PROBE=1`，只读取 CLI 版本并生成临时协议定义。输出明确
分开 `observed` 与 `acceptance`，实际 CLI/Desktop 验收始终标记为 `not-run`。

## 官方来源

- [Codex App Server](https://learn.chatgpt.com/docs/app-server)：只读历史、Resume/Fork
  身份和协议生成接口。
- [Remote connections](https://learn.chatgpt.com/docs/remote-connections)：原生 Desktop
  的跨主机工作流，不代表可供第三方复用的受限接管 API。
