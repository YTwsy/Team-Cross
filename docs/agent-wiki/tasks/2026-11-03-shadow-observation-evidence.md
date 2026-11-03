# 普通关注来源校验修复

日期：2026-11-03。分支：`codex/shadow-activity`，基线 `77f7ddc`。本记录接续[普通关注实际失败](2026-11-02-shadow-activity-live-demo.md)，修复用户指定的来源校验问题；不新增会话、任务或界面模型。

## 原因与修改

原生结构化输出要求模型返回完整 SourceRef，包括未使用的字段。讨论和材料正文也包含 requestId、turnId、itemIds，模型将这些业务元数据补入引用后，与 Core 提供的准确来源不一致。因此消息已经送达，认识却没有保存。

改为 Core 提供本轮 `E1` 等编号及准确来源，模型只选择 `evidenceIds`。原目录随 Run 持久保存，提交时还原来源并继续原有校验；不扩大来源权限、不容忍猜测字段。已有原生轮仍按原协议核对，避免重启时把旧输出解释成新协议。

本机现有“核对原生状态”入口增加针对性恢复：只接续旧协议下失败的普通观察，旧批次标为 superseded 并保留错误与原输出。待处理事件重新读取，已经完成的直接输入、公开动作和目标版本保持。新协议错误、其他失败、暂停及正在处理的同对象均不进入这条恢复；失败保存保留原状态，重复核对不重复重试。

实现：[编号目录与恢复](../../../internal/shadow/observation_evidence.go)、[原生观察](../../../internal/shadow/attention_engine.go)、[批次提交](../../../internal/shadow/attention_batches.go)。

## 验证记录

本机 arm64，Go 1.27.1，Node 24.18.0。工程与原生检查时基线包含本次未提交修改；随后修复以 `e9a2cdf5d7d73b4c04feae39e654b7e2bb96c060` 提交，安装产物从该干净提交构建。

- 全部产品包 `go test ./cmd/... ./internal/...` 和 `go vet ./cmd/... ./internal/...` 通过。使用明确的产品目录，避开已有本地发布工具链缓存 `bin/go-mod`。
- Web check、262 项测试和两套嵌入资源构建通过，构建没有产生资源差异。
- [回归测试](../../../internal/shadow/observation_evidence_test.go)覆盖准确引用、撤回、越界来源、目录持久化、旧失败后的直接回复不重放、显式恢复幂等及保存失败。`go test -race ./internal/shadow ./internal/collab ./internal/mcp` 全部通过；shadow 113.500s、collab 109.347s、mcp 4.316s。
- [原生夹具](../../../internal/shadow/observation_evidence_native_test.go)使用全新 home/仓库与 `gpt-5.6-luna` / `openai` / `low`。入口为 `scripts/verify-shadow-native.py --observation-evidence`，保留脚本必需的 fixture-dir、codex-bin、codex-auth 参数。

过程说明：首次指定了不存在的旧 Codex 路径，未调用模型；第二次 Go 自动工具链校验遭遇网络 EOF，改用本机已安装的同版本工具链。第三次真实模型已通过讨论及材料两版本，撤回夹具缺少必需的 JSON tombstone，被准备阶段拒绝；补齐夹具后重新验证。未通过更改产品校验来绕过该错误。

最终 `run4` 原生验收 106.40s 通过：

| 来源 | 保存结果 | 来源校验 |
| --- | --- | --- |
| 执行会话讨论 | 成员乙确认“已批准”，没有未决事项 | 保留 executionId、annotationId、replyId；没有正文中的业务编号 |
| 材料 v1 | 待审核，名额 20 | 准确 version=1 |
| 材料 v2 | 已批准，名额 35，替换 v1 | 准确 version=2；没有补入 requestId、turnId、itemIds |
| 材料撤回 | 旧事实失效，无可保留证据 | evidence=[]，空目录的真实结构化输出通过 |

四轮均为私有认识；没有创建共享消息、续办或 Work。测试结束关闭本次原生运行时。

详细日志和原生状态仅本地保存在 `.local-backups/shadow-observation-evidence-native-20261103*`；这些目录不提交。实际安装空间仍使用此前的专用 Luna 分身及测试仓库，不触发新关联执行会话中的模型输入。该记录不证明两台 Mac 或其他模型已验收。

## 本机安装及原失败记录恢复

- 版本：`0.2.6-dev.shadow-observation-evidence.20261103`，build 221，arm64，来源 `e9a2cdf5d7d73b4c04feae39e654b7e2bb96c060`，dirty=false。
- `make release` 与 `make verify-release` 通过，覆盖 DMG/校验和、图标、App 签名完整性、CLI/App 版本一致和隔离生命周期。ad-hoc 签名，未使用 Developer ID、未公证、未公开发布。
- DMG SHA-256：`20c0b44cb7d574b491b3b3f22b8a67c5b1212bd055166cf57eed981f141aa299`。
- 新版已安装至 `/Applications/Team Cross.app`，App 内 CLI 与运行 Core 的版本和来源提交一致。旧 App 保存在本工作树 `.local-backups/install/2026-11-03-shadow-observation-evidence/original-installed.app`。
- 安装先等待其他分身空闲。Core 升级使两条先前在线的执行会话离线，并关闭一个共享监听；用原会话恢复入口及共享入口还原后，所有原生会话 ID、online 与 sharing 状态均与安装前一致。没有创建新会话或新邀请；此检查不证明旧邀请地址在监听重建后仍可用。

已安装新版在“分身消息与关注状态测试 · 11月2日”中执行一次本机“核对原生状态”，专用分身运行时确认仍为 `gpt-5.6-luna` / `openai` / `low`。当时实际有四条旧协议来源失败，而不只是最初报告的两条：

| 旧失败对象 | 恢复结果 |
| --- | --- |
| 普通关注讨论 B | 保存评审通过、名额 20 → 35 的事实，证据准确指向原讨论回复 |
| 材料 D | 保存 v2 当前状态和与 v1 的变化，保留准确版本引用 |
| 新增空间根讨论 | 保存该测试讨论的事实与准确 annotationId，无待执行事项 |
| 新增执行会话讨论 | 保存该讨论事实，证据包含准确 executionId 与 annotationId |

四条旧失败批次均为 superseded，旧错误和原生输出保留；四轮新观察 completed 且 AttentionCommitted=true。当前所有 focus 的错误数为 0；静音讨论仍按原规则保持未消费，不宣称所有排队项都已处理。

与安装前快照比较：四条公开行动、十条共享聊天消息、Work 集合及原 S1 ID 全部相同，先前已处理的直接输入没有重新排队。没有重发回复、创建 Work 或新增共享输出；材料和讨论的事实确实已持久保存，不仅是清空错误标志。

仅本地证据：`.local-backups/shadow-observation-evidence-live-20261103/{before,after,reconcile}.json`、`before-summary.json`、`after-summary.json` 与 `logs/`；安装与会话恢复回执在 `.local-backups/install/2026-11-03-shadow-observation-evidence/`。专用原生验收运行时已关闭，正常安装的 App、Core 与用户原有协作会话保留运行。此次以后端协议和恢复为主，没有新增界面或截图验收。
