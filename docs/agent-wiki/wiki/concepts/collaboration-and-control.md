# 协作与控制

当任务涉及 participant、role、annotation、control lease、remote command、revision 或
Share API 权限时，先读本页。

## 角色

- **Owner**：主机本地管理员。可以切换 Provider、改变工具网络、导入 Session、创建或
  撤销 Share，并随时抢占远端控制权。
- **Observer**：远端默认角色。可以查看绑定 Thread、事件、patch、evidence，并创建
  annotation。
- **Controller**：持有当前远端控制租约的 participant。在 Observer 能力之外，可以
  send、steer、interrupt 或回答 Agent input request。

Share 可以有多个 Observer，但同一时刻只有一个远端 Controller。

## 租约

控制租约持续 60 秒，WebGUI 通常每 20 秒 renew。没有 Controller 时第一位请求者获得；
其余请求返回占用状态。Owner preempt、participant release 或到期都会让下一位请求者可以
获得新 epoch。

到期租约不应继续显示为 active。每次新的控制周期必须使用新的 epoch，防止旧浏览器标签
或延迟请求恢复控制。

## 两种并发版本

- event `seq`：SSE 的单调 cursor，只用于重放顺序。
- Thread `revision`：写操作的乐观并发版本。

远端写命令携带 `commandId`、`expectedRevision` 和 `leaseEpoch`。Observer annotation
使用 epoch 0；Agent 控制还要验证 participant 与当前未过期 lease。

## 命令幂等

Core 在执行前持久化 `(share_id, command_id)`、participant identity 与请求对象 hash：

- 同 participant 的完全相同请求重放返回已保存结论和当前 Thread snapshot，不再次执行；
- 相同 ID 被不同 participant 或不同请求内容使用时返回 conflict；
- 处理中重复请求返回 in-progress；
- 执行失败也保存 error result，避免不确定重试重复产生副作用。

已完成重放不重新消费当前 revision 或 lease；新 `commandId` 仍必须通过最新 fencing。

增加新的远端写接口时，不能绕过这条路径。
首次 admission 必须把 Share 有效性、capability、participant、revision 和按操作需要的
lease 检查与 command 插入放在同一原子写入。撤销先提交会拒绝迟到 body；已 admission
的操作可能完成，不把撤销描述成回滚正在执行的工作。

## 远端 API 边界

远端只能访问绑定 Thread。即使 participant 知道另一个 Thread ID，也必须被拒绝。响应中
不返回 repo root、worktree path、active invite、host diagnostics 或凭据。

远端不能切换 Agent、导入 stored Session、改变工具网络、附加主机 evidence 或管理 Share。
这些限制是 Core route guard，不应只依靠按钮隐藏。

## 相关来源

- `../../sources/decisions/collaboration-and-control.md`
- `docs/protocol.md`
- `internal/server/collaboration.go`
- `internal/server/share.go`
- `internal/server/agent.go`
- `internal/storage/collaboration.go`
- `internal/server/integration_test.go`
