# 本人 @ 与分身委托

日期：2026-11-03。分支 `codex/shadow-activity`，基线 `9f2b69f`。用户要求开启自己 @ 自己，或提供本人把工作交给分身的方式。

## 本次范围

复用已有成员选择、结构化 mention 和默认分身直接输入：本人在根批注或回复中选择 @ 自己，Core 把准确 human 作者和来源交给 S。短问题直接回复原讨论，必要时由模型创建已有 Work；没有新增派活表单、任务状态或第二套等待关系。

私有收件原因使用 `mention`，不同时当作 `own_source_change`；本人没有自己的未读提醒。默认路线、暂停、离线、direct_only、静音及同 ID 去重沿用已有规则。分身自己的公开回复不会循环触发自己。

同时修复旧普通观察失败挡住新直接输入的问题。新输入可独立读取与处理，已失败批次保留错误；不会自动重放旧观察或将旧认识标成成功。关注引用校验失败本身未修复，[11 月 2 日复核](2026-11-02-shadow-activity-live-demo.md)中的普通讨论／材料限制仍成立。

## 工程验证

- `go test ./cmd/... ./internal/...` 与相同产品包范围的 `go vet` 通过。仓库所有受跟踪 Go 源码均在这两个目录；直接 `./...` 被已有忽略目录 `bin/go-mod` 内的依赖模块干扰，本次未删除或移动这些缓存。
- `go test -race ./internal/collab ./internal/shadow ./internal/mcp` 通过。
- Web check 1366 条翻译、29 个测试文件共 257 项、两套嵌入 production build 通过；生成物无差异。
- 新回归覆盖发起者根批注／回复、加入成员准确身份与本地空间映射、首次消费、同请求去重、当前与保存关注模式、25 条普通积压后的新直接输入、失败期间到达、重启、并发直接输入、错误保留及失败输入不自动循环重试。已有分身自发消息排除回归通过。

## 真实模型验证

`scripts/verify-local-shadow.py --self-mentions` 使用专用 Git 仓库、独立配置、新原生会话、同一 Mac 三个 Core 和真实 TLS。仅使用 `gpt-5.6-luna` / openai / low。

临时 CLI 已两次验证加入成员在根批注和具体回复中 @ 自己，暂停期间持久接收、恢复后准确原处回复、无本人未读、无重复委托及无额外 Work；离线手动重试、原 thread 和记忆恢复也通过。最终 stdio 读取因临时裸 CLI 没有可验证的安装构建而返回 `installation_unavailable`，因此未把整条脚本标成通过；成品 App CLI 复核另行记录。

原始日志及证据只在本地 `/private/tmp/teamcross-self-mention-native-20261103{,-b}/` 和 `/private/tmp/teamcross-self-mention-*.log`；脚本保存 `self-mentions.json`、`report.json` 与 stdio 的准确结果。第二次 self-mentions.json 的 SHA-256 为 `e6122f60443d0b112b107613cd7c4eb48f548e71cc40c04e8fd7a6cea18bb516`。两次脚本均通过 finally 关闭自身三个 Core。未做双 Mac 或 TUI／Desktop 交互验收。

## 成品安装与当前测试空间

待成品构建、校验及安装后补充准确版本、源码提交和已有测试空间中的回执。
