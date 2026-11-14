# T3 Code

来源：github.com/pingdotgg/t3code 的 README 和 docs/user（thread-sidebar、permission-modes、source-control、faq）、t3.codes、Better Stack 的介绍文（2026 年 3 月，标签可能过时）。它是多个 agent（Claude Code、Codex、Cursor、OpenCode 等）的统一控制台，自己不带模型。composer 和 keybindings 两页没读。

## 布局

- 三栏：左项目，中间是所选项目的线程，右边是对话（prompt、回复、agent 的计划、工具调用）。
- 右侧还有 Diff 面板和「线程详情卡」。详情卡列出这个线程的终端或 agent 启动的本地服务，可以在内置浏览器面板里打开，也可以 Stop。

## 侧栏（文档写得很细）

- 项目内的线程顺序：**置顶 → 活跃 → Working（折叠；在等子 agent、等 PR 检查、或有后台命令的线程）→ Snoozed → Settled**。
- 线程完成、失败、或被审批卡住时，回到活跃列表。
- 线程图标反映 PR 状态：打开、已合并、已关闭。
- 菜单：Pin、Settle thread、Snooze › Custom…、Auto-settle behavior、Regenerate title。
- 用量受限的线程显示「Limited」和重置时间，可「Snooze until reset」「Wake now」。

## 新线程

- 开了多个项目时，「New thread」先出项目选择；⇧点击直接在当前项目里开。
- 可以不选项目开线程，放在一个临时目录里。
- 输入框旁选工作方式（New worktree）和基准分支。
- **⇧点击多个模型：同一条 prompt 同时发给几个模型，各自一个线程、一个 worktree。**

## 输入与运行中

- 控件：模型、推理强度、Chat / Plan 模式、权限。权限不是默认值时，显示在模型旁边。
- ⌘Enter：开始这个线程，同时立刻打开下一份草稿。
- 被用量上限卡住时，排队的消息保留。空输入框里的「Resume」继续被限流或被打断的一轮。
- 停止会连带停掉这个线程派出去的子 agent。
- 把文件拖到侧栏的某个线程行上，就附加给那个线程。

## 权限

- 四档：Supervised、Auto-accept edits、Auto（交给提供方的自动审查）、Full access。新线程的默认值在设置里，出厂是 Full access。
- 审批出现在对话里。子 agent 的审批请求显示在父对话里，带子 agent 的名字。
- Claude 的审批待处理时，直接打字回复 = 拒绝并附上意见。

## Diff 与 Git

- **一轮的 diff 不包含这一轮只是拉取（pull、rebase）带进来的文件。**
- Diff 面板可选来源：提交（最近 100 个）。统一/分栏两种视图。
- 提交弹窗列出改动文件和行数；「Commit & push」是 ⌘⏎。
- PR 页：审查、请求评审、合并。「Mark files as viewed」折叠文件并更新计数。
- 工具调用可展开看完整命令和输出；摘要里会把 shell 包装去掉，只留真正的命令。

## 命令面板

- ⌘K。搜线程可按标题、分支、PR、项目；消息里的匹配单独列在「In messages」下。

## 快速动作

- 线程工具栏的播放图标后面是快速动作；「Add Action」新建，每个动作有名字、可选快捷键、命令，可设为建 worktree 时自动跑。

## 值得抄的细节

- **置顶、取消置顶、settle、snooze、归档、丢弃草稿，都给 5 秒的撤销 toast。**
- 侧栏把「还在等后台事情的线程」单独收成 Working 一栏，不和需要你处理的混在一起。
- 一条 prompt 多模型对比。
- 工具摘要去掉 shell 包装。
