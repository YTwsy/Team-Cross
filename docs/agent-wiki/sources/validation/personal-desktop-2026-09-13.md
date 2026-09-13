# 邀请者在个人 Codex 中打开 fork · 2026-09-13

## 范围与环境

验证在邀请者详情页主动点击个人 Codex 入口，以持久化 `sessionId` 定位已创建的 fork。创建、远端操作、完成轮次和交接均不自动切换 Desktop。共享关闭且运行时释放后，入口显示“在个人 Codex 中继续”。产品语义见 [原生客户端决策](../decisions/native-clients-and-models.md)，请求与返回见 [协议](../protocol.md)。

环境为 Apple Silicon、macOS 14.8.5、Go 1.25.3、Node 24.18.0；测试代码基于 `068b5b7` 的本次改动，Core 报告 `0.1.3-dev`，用于准备本地 `0.1.3` 构建。Desktop 应用包为 `/Applications/ChatGPT.app`，静态核对版本 `26.903.71938`（8576）。这不是正式发布或不同 Desktop 版本的验收。

## 工程检查

通过 `go test ./...`、`go vet ./...`，以及 `internal/collab`、`internal/mcp`、`internal/sharing`、`internal/nativecodex`、`internal/nativeclaude`、`internal/service`、`internal/cliinstall` 的 race test。Web check、48 项交互测试和 production build 通过；已更新 `internal/webassets/dist`，检查文档相对链接和 `git diff --check`。

[personal_desktop_test.go](../../../../internal/collab/personal_desktop_test.go) 覆盖：

- 使用协作 fork ID，而非来源 ID；包含空格与单引号的应用路径按独立参数传递。
- 仅本机拥有的 Codex 协作可打开；拒绝加入记录、Claude、缺失 fork 和缺失 Desktop。
- 预览命令不启动 Desktop；重复主动打开不重复 fork、不调用原生运行时，不改变当前输入者、epoch 或直接连接。
- 运行时释放后仍可打开，且不会恢复运行时；打开失败不返回成功。
- 同源 HTTP 请求使用服务端已保存的会话 ID/URL，跨源写入被拒绝。

[flows.test.tsx](../../../../packages/web/src/test/flows.test.tsx) 新增 10 项用例，覆盖远端忙碌时不自动打开、点击请求、隐藏条件、运行时文案和失败重试。该文件共 34 项测试，另有上下文刷新 5 项、批注 9 项。

## 浏览器与打开请求

使用独立测试 Core、合成协作记录与实际 production Web 资源。浏览器对会话状态和历史使用测试响应，点击后的 `POST /personal-desktop` 由真实 Core 处理；测试 `open` 替身记录参数，不启动用户 Desktop 或模型。

确认点击前没有打开请求，点击后命令为 `open -a <测试应用路径> codex://threads/<协作sessionId>`，页面显示“已请求个人 Codex 打开此协作会话”。检查 1440、1024、768 CSS 像素的深浅主题、长目录和顶部按钮布局，均无横向溢出；已实际查看截图。另外检查 released 文案，以及接收者和 Claude 协作不出现该入口。

截图位于 `output/playwright/personal-desktop-20260913/`，测试数据与打开参数保留在 `/private/tmp/teamcross-personal-desktop-20260913/`。测试浏览器按独立会话关闭，测试 Core 按专用数据目录停止；没有操作用户已有会话或安装中的服务。

## 版本构建与安装

本轮 `0.1.3` 候选产物通过 DMG 校验和、挂载安装、ad-hoc 签名完整性、App/CLI 版本一致性、菜单栏 App 跨副本协调、命令安装/移除与兼容 Core 复用检查；隔离 Homebrew 前缀中的 Formula/Cask 安装、升级、互斥和卸载保留数据检查通过。产物面向 arm64、macOS 14.0 及以上，未使用 Developer ID 签名或公证，未发布到远端或替换已有安装。每份构建的提交、校验和与签名状态以其 `release.json` 为准。

安装验收首次在新 App 的退出检查超时。诊断确认 fixture 在启动 helper 记录 `serve` 后立即发送退出，此时 App 尚未完成启动回调、仍处于 `busy`，退出事件被忽略。[verify-app-instance.py](../../../../scripts/verify-app-instance.py) 现等待启动后的 `status` 回读再发送一次退出事件，保留停止 Core 和数据保留断言；修正后完整安装检查通过。

## Desktop 能力边界

本机应用包静态实现包含 `codex://threads/<id>` 的生成与处理：按 ID 读取会话成功后导航，不以侧边栏已有项目为前置条件。该证据仅支持当前版本存在对应入口。

电脑控制工具明确拒绝操作 `com.openai.codex`，因此本轮未验证真实 Desktop 中“列表不可见 fork → 深链接打开 → 侧边栏出现”的窗口全过程，也未验证运行时占用期间的页面表现、重复打开已打开会话的刷新、持续消息同步、审批或真实模型执行。`launched=true` 只表示系统打开请求成功。此次接口不提供只读隔离，不提前释放共享运行时；受控协作输入继续使用直接客户端或辅助 MCP。
