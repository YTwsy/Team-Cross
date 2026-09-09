# 输入协调与共享

先读 [输入与共享决策](../../sources/decisions/input-and-sharing.md)，再按 [协议](../../sources/protocol.md) 核对字段和操作。

## 状态判断

- 读取可并行；原生客户端与 MCP 写入都检查当前输入者。
- 交接使用当前 `epoch`，交出或接回时关闭旧直接连接；首轮同一协作只有一个直接客户端。
- 当前轮忙碌时不能重复开始；补充、中断和审批分别使用对应入口。
- `requestId` 用于去重与查询，RPC 返回不是整轮完成。状态不明先查询，不自动重放。
- 主机不可达只能判断断线；已观察到结束或到期时才显示对应结果和新邀请入口。

这些约束只覆盖 Team Cross 管理的入口，不是 Provider 所有外部入口的全局单写入者保证。

## 代码与检查

[rpc.go](../../../../internal/collab/rpc.go) 负责写入、事件和审批；[network.go](../../../../internal/collab/network.go) 负责交接、加入、结束与代理；[sharing.go](../../../../internal/sharing/sharing.go) 负责 LAN 邀请与 TLS。

优先检查 [协作测试](../../../../internal/collab/collab_test.go) 中的输入归属、去重、原生/工具并行、审批及访问范围，并运行相关 race test。修改邀请或重连后，区分同机 TLS 验证与真实两台 Mac 的 LAN；当前不包含 Tailnet/Tailcat。

相关任务：[目录与生命周期](workspace-and-lifecycle.md) · [原生客户端](native-clients-and-models.md) · [验证](validation-gates.md)
