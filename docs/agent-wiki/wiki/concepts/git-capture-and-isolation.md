# Git capture 与隔离

当任务修改 capture、untracked 文件、worktree、Agent 文件访问或 patch export 时，先读本页。

## 生命周期

1. `capture/preview` 读取 repository root、HEAD、branch、porcelain status、staged 和
   unstaged binary patch，并列出 untracked metadata。
2. 用户确认交接说明与需要携带的 untracked 文件。
3. Core 保存 Git snapshot 与 Round 0。
4. 对有 baseline commit 的 Thread，从该 commit 创建 detached worktree。
5. staged patch 以保留 index 的方式应用，unstaged patch 随后应用，再恢复选中的
   untracked 文件。
6. 所有 Provider 顺序共享这个 worktree。
7. UI 和 download endpoint 随时相对 baseline 生成完整 binary patch。

## 为什么不是复制目录

worktree 保留真实 Git object 与 baseline 身份，能够准确导出 tracked binary diff；同时不
需要复制整个 repository。固定 baseline 也避免 branch 在 capture 后移动导致结果无法定位。

离线 Fork 是例外：它显式打包 baseline 可达 Git 对象，在接收端建立独立对象库和
worktree；不是复制主机配置、凭据或完整工作环境。分享代码只读封存 Round，不能从
后来改变的 worktree 动态扩大。详见 [审阅与接力](session-review-and-continuation.md)。

## 需要保持的边界

- 不对原 checkout 运行 `git apply`、写 Agent 文件或改变 index。
- 不用当前 branch tip 替换 capture 时的 HEAD。
- 不把未选择或超限的 untracked 内容悄悄带入 worktree。
- symlink、file mode、binary 内容和 staged/unstaged 语义不能退化成文本复制。
- export 必须能由 `git apply --binary` 应用到同一 baseline。
- 已捕获路径后来被 `.gitignore` 隐藏仍须导出；累计路径来自同 Thread 不可变快照，
  不扫描全部 ignored，不复活删除文件。代码审阅使用 sealed Round，不用 live diff 归锚。
- capture/export 不能执行不可信 Git hooks、textconv 或内容 filter；只读读取也不能
  继承 host Git 路由环境而误指向原 checkout。
- Team Cross 当前只显示 worktree 路径；用户依据该路径自行进入目录是显式操作，
  不等于 Team Cross 已采纳改动。

## unborn repository

没有首个 commit 时不存在稳定 baseline。可以保留只读 Thread 与交接说明，但不能创建
managed Agent 或承诺可导出的 baseline patch。出现首个 commit 后应新建适用的 Thread，
而不是悄悄改变旧 Thread 的 baseline。

## 改动时检查

- `internal/gitstate/capture_test.go` 的 clean/dirty/binary/untracked/unborn 覆盖；
- staged 与 unstaged 同一文件时的应用顺序；
- 单文件 5 MiB、单轮 20 MiB 上限；
- 新鲜 checkout 上 `git apply --binary --check`；
- 应用后的文件与 Thread worktree 一致；
- 原 repository `git status` 没有 Agent 新文件。

## 相关来源

- `../../sources/decisions/git-isolation-and-export.md`
- `internal/gitstate/`
- `internal/server/threads.go`
