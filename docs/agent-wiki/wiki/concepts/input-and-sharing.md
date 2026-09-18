# 输入协调与共享

只读空间与原生执行空间共用多份单人邀请和独立成员身份；`selfId` 表示本机身份，有执行时 `writer` 绑定具体成员。材料发布、阅读与讨论无需输入权。启用执行关闭旧只读邀请和成员资格，再按新能力邀请，见 [协作空间任务页](collaboration-spaces.md)；同机三 Core 与真实多台 Mac 分别验收。

个人 MCP 与 CLI 已提供申请、取消、交接、接回与交还入口；参数与命令见 [协议](../../sources/protocol.md)。Core 统一检查角色、成员加入、忙碌状态和 epoch。CLI 协作详情与服务 `status` 分开，输入管理不自动发送或中断模型。

先读 [输入与共享决策](../../sources/decisions/input-and-sharing.md)，再按 [协议](../../sources/protocol.md) 核对字段和操作。

## 状态判断

- 读取可并行；原生客户端与 MCP 写入都检查当前输入者。
- 交接使用当前 `epoch`，交出或接回时关闭旧直接连接；首轮同一协作只有一个直接客户端。
- 当前轮忙碌时不能重复开始；补充、中断和审批分别使用对应入口。
- `requestId` 用于去重与查询，RPC 返回不是整轮完成。状态不明先查询，不自动重放。
- 邀请期限只限制首次加入，一份邀请限一人；加入后的独立访问凭据不随邀请码到期。断线、睡眠、关闭客户端或 B 重启不撤销资格。
- 邀请前显式选择 `lan` 或实验性 `tailcat`，不自动回退或在断线后切换；两种传输复用相同 TLS pin、成员资格和输入协调。
- B 主动离开、A 结束共享或退出/重启才终止加入资格。主机不可达只能判断断线；观察到结束才显示新邀请入口。
- 结束共享后，等执行、审批、已接收请求和直接连接清空，再关闭该协作的后台进程释放原生锁；读取上下文不自动恢复。

这些约束只覆盖 Team Cross 管理的入口，不是 Provider 所有外部入口的全局单写入者保证。

## 代码与检查

[rpc.go](../../../../internal/collab/rpc.go) 负责写入、事件和审批；[network.go](../../../../internal/collab/network.go) 负责交接、加入、结束与代理；[sharing.go](../../../../internal/sharing/sharing.go) 负责邀请与 TLS，[connection.go](../../../../internal/sharing/connection.go) 和 [tailcat.go](../../../../internal/sharing/tailcat.go) 负责显式传输适配。

优先检查 [协作测试](../../../../internal/collab/collab_test.go) 中的输入归属、去重、原生/工具并行、审批及访问范围，并运行相关 race test。修改邀请或重连后，分别报告同机 TLS、同机 Tailcat、两台 Mac LAN、两台 Mac 不同网络以及强制 DERP；前两项不能替代后三项。

相关任务：[目录与生命周期](workspace-and-lifecycle.md) · [原生客户端](native-clients-and-models.md) · [验证](validation-gates.md)

参与方在线由 Core 心跳维持；B 可申请或取消输入，仍需 A 明确交接。App 退出停止本机服务，B 退出不终止 A。见 [分发与首次体验](../../sources/distribution-and-onboarding.md)。

Claude 终端输入同样受当前输入者与 epoch 约束；审批留在同一个原生 worker，接回输入后由新 TUI 处理。MCP 当前只支持空闲文本发送，审批/补充/中断返回 `native_client_required`。见 [Claude 接入契约](../../sources/decisions/claude-native-tui.md)。
