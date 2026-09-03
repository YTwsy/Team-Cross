---
title: 多人协作、控制租约与命令幂等
kind: decision
status: accepted
---

# 多人协作、控制租约与命令幂等

## 决策

一个 Share 可以同时有多个 Observer，但同时最多只有一个远端 Controller。Controller
通过 60 秒租约获得 `send`、`steer`、`interrupt` 和 input response 能力；WebGUI 通常
每 20 秒续约。主机 Owner 不受远端租约限制，并始终可以抢占或撤销它。

所有远端写入都使用 `commandId`、`expectedRevision` 和 `leaseEpoch`。服务端先按 Share
范围 admission 命令身份：完全相同且已经完成的请求直接重放其执行结论，不再次产生副作用。
新命令插入与 Share 有效性、capability、participant、revision 及按操作需要的 lease 检查
必须是同一 SQLite 原子写入，不能先鉴权、再无条件 claim。

## 原因

多人可以并行阅读和批注，但两个远端参与者同时驱动同一个 Agent 会造成不可解释的 Turn
顺序。短租约提供明确的单写者，同时允许浏览器断线后自动释放控制。

网络断开可能发生在服务端已经执行命令、响应尚未到达接收者之后。持久化命令身份让接收
者能够安全重试；revision 防止基于旧界面创建新写入，lease epoch 防止旧 Controller 在
被抢占后恢复权限。

## 不变量

- Observer 可以查看和批注，但不能驱动 Agent。
- 同一 Share 同时最多一个远端 Controller；Owner 可以随时抢占。
- event `seq` 是 SSE cursor，Thread `revision` 是乐观写入版本，两者不能互换。
- 命令身份由 Share、`commandId`、参与者和规范化请求语义共同约束。
- 同 ID 同语义的已完成请求不重新校验当前 revision/lease，也不重新执行；它只是重放此前
  已获授权的结果。
- 同 ID 不同参与者或不同语义必须返回 command conflict。
- 首次 Agent 命令必须在 dispatch 前通过当前 participant、revision、epoch 和 expiry
  校验，并以 expected revision 原子追加 command event。
- Share 撤销与新命令 admission 以数据库提交顺序线性化。撤销先提交时，即使旧请求已
  进入 HTTP handler、后来才送完 body，也不能新获执行资格。仅通过初始 HTTP 鉴权不算
  admission；此前已正式接受的操作可能完成，撤销不承诺回滚已接受的 Agent 工作。
- Observer annotate 和 control.request 不要求事先拥有租约；renew/release/Agent 命令
  必须匹配当前 holder、epoch 与未过期 lease，所有新命令都受有效 Share/capability 约束。
- 失败与完成结论都持久化；状态为 running 的重复请求返回 in-progress。
- 有效 Share/capability/member 下，因 revision/lease 失效而拒绝的新命令直接持久化为
  error，不获得执行资格。首次保留原 fencing 错误，精确重放返回已保存失败；撤销、
  过期或越权请求不新建命令记录。
- 远端权限由 Core route guard 强制执行，不能只依靠 WebGUI 隐藏按钮。

## 影响

重试调用可以得到当前 Thread snapshot，但不能假定它与首次 HTTP body 按字节相同；幂等
保证的是同一命令只执行一次，并持久化其完成或失败结论。新的用户意图必须生成新的
`commandId`，并使用 UI 最新的 revision 与 lease epoch。

## 重新打开条件

如果未来允许多个并行 Agent Run、细粒度文件所有权或离线命令队列，需要重新设计租约
粒度与 command key。任何变化都必须继续提供单调 fencing 和响应丢失后的安全重试。

## 事实来源

- `internal/storage/collaboration.go`
- `internal/server/collaboration.go`
- `internal/server/share.go`
- `internal/server/agent.go`
- `internal/server/integration_test.go`
- `docs/protocol.md`
