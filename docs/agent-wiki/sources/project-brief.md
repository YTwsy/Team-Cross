---
title: Team Cross 项目简报
kind: source
status: current
---

# 项目简报

Team Cross 是 macOS 上的本地协作工具：从已有 Session 发布选定的历史，邀请同事阅读、讨论并带回自己的调查；需要共同执行时创建新的原生 fork。空间、可选的共享会话、模型调用与代码执行留在发起者 A；每位参与者通过自己的 Team Cross Core 加入。

main 已采用原生协作架构，当前仍处于原型阶段，可以按确认的产品方向重构，不维护旧数据或旧 API 兼容性。

## 产品基准

- 创建可操作会话始终产生新的原生会话 ID，保存来源及确认过的完成轮。
- 用户选择使用原目录，或从确认的 Git HEAD 创建干净 worktree。创建原目录协作不改变现有分支、暂存区和文件；后续显式 Agent 操作在该目录执行。
- 直接入口支持 TUI 与专用 Desktop；辅助入口支持用户自己的 TUI/Desktop，通过本地 MCP 参与。
- 两种入口可以同时存在，读取可并行，写入遵循统一输入归属。
- WebGUI 负责协作管理、轻量上下文和批注；完整 Agent 对话与执行交互交给 Codex 客户端。
- 结束共享关闭远端访问，保留会话和代码。提出问题、整理摘要、验收成果或提交代码都不是协作开始和结束的必填条件。
- 产品不固定模型或推理强度；真实模型测试只使用 `gpt-5.6-luna`。

## 当前范围

当前面向普通 Git 仓库、macOS 与 A/B 两位参与者；Codex 为主线，Claude Code 原生 TUI 为实验性 Provider。连接方式由用户显式选择局域网或实验性 Tailcat，不自动降级或切换。不包含 submodule 支持、强制 Round、独立 Evidence 体系、离线交接包、专用 patch/PR 流程、Tailnet 控制平面集成或 Tailcat 服务可用性承诺。

同机 A/B、同机 Tailcat、两台 Mac 的 LAN、两台 Mac 不同网络和强制 DERP 中继分别报告。Desktop 指定运行时入口依赖已测试的客户端版本，不能仅凭共用 app-server 协议就宣称所有 Desktop 功能兼容。

## 只读分享与多人空间

独立只读分享可以不创建可执行 fork；首版同时支持三人及以上，不能以双人实现代替。任一参与者可以发布多份选定范围的会话材料，供人和 Agent 按需读取、批注和回应。对象语义见 [核心词汇](product-core-and-glossary.md#协作空间的对象边界)，设计基线、实施顺序与首版完成标准见 [协作空间契约](decisions/collaboration-spaces.md)。这些能力已进入本地原型；材料范围、导出上限和验证边界以契约及协议为准。

## 主要来源

[用户说明](../../../README.md) · [产品流程](product-flows.md) · [核心词汇](product-core-and-glossary.md) · [架构](architecture.md) · [协议](protocol.md) · [验收导航](../wiki/concepts/validation-gates.md)

产品方向、主要入口或支持范围变化时，先同步本页与核心词汇，再更新相关决策和任务页面。
