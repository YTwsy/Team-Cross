# Composer mentions 空结果修复

日期：2026-10-03。分支：`codex/meta-plugin-webgui`。修复提交：`53d34720ae10cccf29fcfd3a1f71b5ec22c4eb8a`，基于 `794ba0a`。本记录补充先前的 [mentions 接入验收](2026-10-03-composer-mentions.md)，不将先前的协议验证当作桌面界面验收。

## 原因与复现

用户在重启桌面后搜索“连接池 v1”仍无结果。现场插件和演示 Core 均已是 `9c1c378`，桌面插件进程在升级后重启，因此不能归因于上轮安装缓存。

只读核对已安装 ChatGPT `26.930.21537`（build `12776`）的打包代码：元数据发现的 mention hook 使用兼容调用格式 `{query,path:[]}`。该桌面请求失败时，搜索层返回空 items。Team Cross 原实现只接受一个 query 字段，拒绝 path。

对实际安装包进行 stdio 复现：`{query:"连接池 v1"}` 返回两个结果，包含“连接池调查 · v1”；同一请求加入 `path:[]` 后返回 `isError:true`，错误为参数校验拒绝其他字段。先前验收只构造 query-only 请求，未覆盖桌面的实际请求形状。

## 修复与验证

搜索工具的 inputSchema 与运行时校验共同接受可选的空 path 数组。非空数组、null、字符串、对象或其他额外字段仍拒绝。path 不参与检索，也不表示文件系统路径。Core 搜索、材料版本和权限机制没有改动，协议见 [个人资源库](../../sources/protocol.md#个人资源库)。

- `go test ./...`、`go vet ./...`、MCP 与 pluginpack 的 race 检查通过。
- Web check 784 条翻译、12 文件/139 项测试、两种 production build 通过；前端源码和嵌入产物未变。
- 修订验收脚本逐项比较标准请求与桌面请求。真实 stdio 和原生 app-server 均返回 `desktopEmptyPathMatchesQueryOnly=true`；覆盖空搜索、v1、v2、原批注和未发布内容查询。
- 固定材料版本、批注资源读取、回复分页、不同 Core 拒绝、撤回后的查询排除与旧 URI 拒绝继续通过。
- 临时原始报告为 `/private/tmp/teamcross-mentions-path-20261003-1Lz78Y/acceptance/acceptance.json`；以上关键结果已保存在本记录中。fixture 写入 finish 后通过最终断言并退出，原生测试客户端退出码 0，临时目录下无剩余测试进程。没有真实模型输入或主窗口自动化。

## 安装与边界

从修复提交重建，升级原有 `teamcross@teamcross-local`。安装源、演示数据目录和正在运行的演示 Core 保留，`~/.codex/config.toml` 升级前后字节相同。再次通过真实安装源加载，桌面请求格式与资源读取通过，原生测试客户端退出码 0。

安装运行文件 SHA-256：`b117d8414be2f932b4280d9a400359679b871772e2e3c5b0106121403c8cf434`。

确认主桌面的旧插件进程仍映射旧文件 inode，而安装源已经换成新 inode；没有强制结束用户进程。用户在当前工作结束后完全退出并重新打开 ChatGPT，才能让原有输入框使用此次修复。原生客户端按桌面格式验证成功，仍不等于实际输入框已显示结果；最终显示以用户重载后的观察为准。
