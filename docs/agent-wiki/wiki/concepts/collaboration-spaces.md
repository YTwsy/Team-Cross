# 只读分享与多人协作空间

状态：下一阶段已确认范围，尚未实现。完整设计和实施门槛见 [协作空间契约](../../sources/decisions/collaboration-spaces.md)，对象语义见 [核心词汇](../../sources/product-core-and-glossary.md#下一阶段的对象边界)。

## 已确认的首版目标

- 独立只读分享不要求创建原生 fork，可从一个 Session 直接发起阅读和讨论。
- 首版必须支持三人及以上，至少覆盖发起者 A 与受邀者 B、C 同时参与。不能只实现 A/B 后宣称完成。
- 任一参与者可发布多份选定范围的会话材料；公开范围、建议先看位置和 Agent 实际读取范围分别处理。

## 实现时的对象边界

空间负责成员、材料和讨论；原生个人会话保持独立。已发布材料是固定范围与版本的只读内容，可操作的协作仍是新的原生 fork。设计基线为单主机托管、每空间最多一个可操作协作，后续对话不自动公开。

成员身份必须分别绑定作者、凭据、在线状态、输入申请与请求去重。每份邀请一人、同一空间多份邀请；撤销 B 不影响 C。讨论无需共享输入权，共享执行仍只有一个当前输入者，所有原生和工具写入共用协调入口。

只读空间启用共同执行是明确的新访问范围；发布一段历史不能隐式开放完整原生会话或执行目录。WebGUI、MCP/CLI 与运行时工具必须读取同一份受范围限制的材料。

## 代码与检查

现有入口：[types.go](../../../../internal/collab/types.go)、[sharing.go](../../../../internal/sharing/sharing.go)、[network.go](../../../../internal/collab/network.go)、[MCP](../../../../internal/mcp/server.go)、[WebGUI](../../../../packages/web/src/components/)。当前仍是 `Record` 与单 fork 绑定、单个远端成员；不要把这里的目标当作已经可用的接口。

按来源契约中的顺序实现空间/成员、材料、讨论引用、多人接力和完整入口；这些是同一首版的内部批次。按 [验证门槛](validation-gates.md) 补范围隔离、多成员撤销和交接竞态；同机三 Core 与三台 Mac 分别报告，旧双人结果不能替代。

相关任务：[产品模型](product-model-and-glossary.md) · [输入与共享](input-and-sharing.md) · [WebGUI 与 MCP](webgui-and-mcp.md)
