# 只读分享与多人协作空间

本页解释空间、材料、成员和执行之间的关系，帮助跨主题定位。当前规则与设计取舍由 [协作空间契约](../../sources/decisions/collaboration-spaces.md) 维护，对象语义见 [核心词汇](../../sources/product-core-and-glossary.md#协作空间的对象边界)，实际验证范围见 [证据入口](../../sources/validation/evidence-map.md#空间材料与读取)。

空间先提供成员、材料和讨论。成员可以发布自己选定范围的会话材料，各自的个人会话继续保持独立；分享一段历史不会自动公开后续内容。材料的固定版本让不同成员和 Agent 能围绕同一份原文讨论。

加入空间、获得执行访问和取得当前输入权是不同关系。同一链接可以供多人加入，每位成员拥有独立凭据；需要一起操作项目时，才明确启用共同执行并开放新增访问。只读空间本身不需要共享 fork，空间的公开材料与执行目录也有各自的授权范围。

参与会话还可以通过明确关联的目标接收空间请求。空间简报保存成员确认的共同依据，可选专用会话帮助接手这些请求；它们与共同执行独立。因此，“有人阅读材料”“Agent 参与讨论”和“共同操作项目”不应视为同一种接入状态。

## 按问题查来源

| 问题 | 主要来源 | 实现入口 |
| --- | --- | --- |
| 空间成员、邀请与执行授权 | [空间契约](../../sources/decisions/collaboration-spaces.md)、[输入与共享](../../sources/decisions/input-and-sharing.md) | [空间](../../../../internal/collab/spaces.go)、[网络](../../../../internal/collab/network.go) |
| 发布范围、更新版本与撤回 | [分享流程](../../sources/product-flows.md#分享范围与确认)、[材料协议](../../sources/protocol.md#已发布会话材料) | [发布](../../../../internal/collab/publication.go)、[材料](../../../../internal/collab/materials.go) |
| 内容存储、授权和按需读取 | [架构](../../sources/architecture.md#空间材料与执行)、[Agent 读取协议](../../sources/protocol.md#agent-按需读取视图) | [材料存储](../../../../internal/materialstore/)、[材料 MCP](../../../../internal/mcp/materials.go) |
| 空间请求、简报与专用角色 | [空间工作台](../../sources/decisions/space-workbench.md)、[接收配对](../../sources/decisions/agent-pairing.md) | [工作台 Core](../../../../internal/collab/workbench.go)、[独立接收会话](../../../../internal/collab/space_receivers.go) |

## 验证时区分

按 [验证门槛](../../sources/validation/test-gates.md) 选择范围隔离、多成员撤销和输入交接检查。实际结果通过 [证据入口](../../sources/validation/evidence-map.md) 定向读取；同机三个 Core、真实模型参与和三台 Mac 的网络各自需要相应证据，较早的双人或旧材料格式报告只适用于当时范围。

相关导航：[产品模型](product-model-and-glossary.md) · [WebGUI 与 MCP](webgui-and-mcp.md) · [验证层次](validation-gates.md)
