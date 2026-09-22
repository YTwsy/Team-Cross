# 2026-09-22 个人资源库与资源速览验证

## 环境与范围

- 源码分支：`codex/feature/resource-library`，以 `f88cb79`（分享范围阅读与确认）为基线；本记录对应其上的资源库工作区改动，不代表已发布版本。
- macOS 14.8.5，Apple Silicon / arm64，Go 1.27.1，Node 24.18.0，pnpm 11.19.0。
- Go 构建缓存、Core 数据、测试 App 与材料均独立于用户正在使用的数据目录。浏览器历史和材料为合成内容；HTTP、材料存储、持久化及 MCP STDIO 为实际实现。

产品范围见 [资源库决策](../decisions/resource-library.md)，接口见 [个人资源库](../protocol.md#个人资源库)。

## 工程检查

以下检查通过：

- `go test ./...`、`go vet ./...`；`go test -race` 覆盖 `internal/collab`、`internal/mcp`、`internal/materialstore`、`internal/sharing`、`internal/nativecodex`、`internal/nativeclaude`、`internal/service`、`internal/cliinstall`。最后的 Core 调整后再次完成全量 test/vet 与 collab/mcp race。
- Web `pnpm check`、`pnpm test`（8 个文件、106 项）与 `pnpm build`，重新生成 `internal/webassets/dist`，并构建当前 CLI。Vite 仍提示主 bundle 超过 500 kB，不影响构建结果，本次没有做额外拆包。
- macOS Swift 类型检查与 arm64 macOS 14 目标优化编译。
- `scripts/verify-app-instance.py` 使用本次测试 App 通过真实跨副本启动、规范数据目录、首页/邀请转交、确认去重、错误请求拒绝、无响应持有者恢复、重开、独立目录、次实例退出保留 Core、外壳异常退出恢复、显式退出与数据保留检查。

[Core 用例](../../../../internal/collab/library_test.go) 覆盖持久化、不可变编号、请求去重、固定版本、并发收藏/选择、成员身份与本人回复、分页/到期、越界定位、撤回与访问撤销、共享 listener 和运行时工具隔离、跨空间个人读取与同空间共享发送预检查。[MCP 用例](../../../../internal/mcp/library_test.go) 验证明确编号、路由及分页参数。前端覆盖保留设置页、阅读与选择分离、筛选保留选择、原文及回复、内联入口无对话框、选择改变不自动更新旧编号、重新生成新请求、输入权和跨空间限制、访问失效、原生桥调用，以及 Core 省略零值/重排定位字段后仍可取消原选区。

## 实际浏览器与 MCP

使用内嵌生产资源而非仅用组件 mock，完成以下流程：

1. 从两个测试空间浏览按 Session 分组的材料、原文批注和回复，选择材料及批注，检查设置导航与原有命令行、外观、原生客户端、个人辅助接入区。
2. 点击生成入口，底部显示真实编号与复制按钮，没有模态对话框。保持入口展开后仍可搜索并加入另一空间的批注；旧编号不变，显示选择变化提示，共享发送禁用。重新生成后编号更新，复制按钮显示“已复制”。切到设置页后选择与入口继续保留。
3. 在独立 fixture 的模拟运行时确认共享发送预览，实际经过 Core RPC 接收，显示“已接收，尚未确认执行完成”；没有调用模型，也不把接收回执视为模型完成。
4. 当前构建的 `teamcross mcp --data-dir <测试目录>` 经真实 STDIO 初始化、工具枚举和 `read_selection`，读回 2 项内容：材料版本 1，以及包含 1 条回复的原批注。结果来自该测试 Core 的真实材料存储与讨论记录。
5. 在实际 CSS 视口 `1440×900`、`1024×900`、`768×900` 的浅色与深色各检查一次，保存并查看六组截图：无横向溢出、无读取入口对话框、底部栏保持在侧栏右侧。窄屏展开阅读时收起列表，关闭预览后可以继续搜索。
6. 紧凑速览在 `471×651` CSS 视口检查当前选择与内联入口，共用 Core 中的 3 项选择，无横向溢出或模态对话框。

截图与精简证据保留在本地忽略目录 `bin/resource-library-evidence/`，包含 `library-inline-{light,dark}-{1440,1024,768}.png`、`resource-quick-inline.png`、`viewport-checks.json` 和 `mcp-read-selection.json`。尺寸来自 DOM 实测；浏览器截图有缩放，不用 PNG 像素数推断 CSS 视口。

## 覆盖边界与收尾

原生 App 已完成编译和生命周期脚本，速览页面、共享选择与桥调用完成浏览器/组件验证；电脑控制未能绑定这次独立测试 App，因此**菜单栏左/右键、全局热键和固定浮窗的真实桌面交互尚未验收**，不能用生命周期检查替代这些交互。

本次未使用真实模型、未接入用户实际 Session，未进行两台 Mac、LAN 或 Tailcat 网络验收。没有构建正式 DMG、发布 Release 或更新用户安装。早先一轮临时浏览器 fixture 达到存活上限后结束，随后重建 fixture 并完成检查；正常验证门槛均通过。

测试结束后精确关闭本次受控浏览器页、临时 fixture、测试 App 与独立 Core，恢复临时视口设置，受控浏览器标签列表已确认为空。外部 Edge 扩展枚举超时，未能确认测试 App 自动打开的外部标签是否仍在；对应测试服务已停止。保留本地证据与测试数据，不停止用户其他客户端。
