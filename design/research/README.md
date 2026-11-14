# 主流 agent 桌面应用：界面与交互调研

2026-11-14 整理。给 Talos 做界面和交互时查的参考，每个产品一份笔记，这一页是索引和横向对比。

**来源的局限**：全部来自各家官方文档和更新日志的文字，没有一张真实截图。文档讲功能和快捷键讲得细，讲「长什么样」几乎不讲——间距、字号、颜色、动效、对话流里每种条目的画法，这里都没有。这部分要靠看真实应用补（见最后「还缺的」）。

## 各产品笔记

| 文件 | 产品 | 读到的程度 |
|---|---|---|
| [codex.md](codex.md) | Codex 桌面应用 | 较全：布局、侧栏、Review、worktree、设置、全部快捷键 |
| [claude-code-desktop.md](claude-code-desktop.md) | Claude Code 桌面版 | 较全，desktop 页后 1/3 没读到 |
| [cursor.md](cursor.md) | Cursor | 不全：Agents Window 页 404；排队/插话、检查点、计划模式读到了 |
| [zed.md](zed.md) | Zed Agent Panel | 较全 |
| [warp.md](warp.md) | Warp | 较全，排队和审查写得最细 |
| [windsurf-devin.md](windsurf-devin.md) | Windsurf Cascade、Devin | 中等 |
| [opencode.md](opencode.md) | OpenCode 桌面版 | 没有桌面文档，从更新日志拼的 |
| [t3code.md](t3code.md) | T3 Code | 中等，侧栏和权限较细 |
| [kero.md](kero.md) | Kero | 较全（它是终端，没有对话流） |

Conductor 整站拒绝抓取，只有搜索摘要，放在本页末尾，未核实。

## 横向对比

### 1. 运行中发消息：排队还是插话

这是各家分歧最大、也最讲究的一处。

| 产品 | Enter | 另一个键 | 排队项怎么显示和操作 |
|---|---|---|---|
| Cursor | 排队 | ⌘Enter 立即发；连按两次 Enter 或「Send now」= 插话 | 列在当前任务下面，可拖动排序 |
| Zed | 排队 | 排队项上的「Steer」开关；连按两次 Enter =「Send Now」 | 可改、可删（单条或全部） |
| Windsurf | 排队 | 空输入框再按 Enter = 立即发 | 可删 |
| Warp | 默认打断当前回复（可改成排队） | ⌘⇧J 开自动排队 | 「N queued」面板：排序、立即发、改、删；空输入框 Enter 发队首 |
| OpenCode | — | ⌥Enter 排到最前；⌘Enter 对下一条插话 | 同步中也能编辑 |
| Claude Code | 不停止当前运行，当前动作做完后读到 | 停止按钮 / Esc | CLI：灰色排队项，↑ 取回编辑 |
| Codex | 设置里选「引导当前这一轮」或「等下一轮」 | — | 文档没写 |

共同点：
- **插话都落在「下一次工具调用」或「当前动作做完」，没有一家硬切正在跑的工具。**
- 排队的消息一定看得见，并且至少能删；讲究的能改、能排序、能单条立即发。
- 停止之后，没发出去的消息不自动续跑（Warp：队首回到输入框）。
- 多数把 Enter 默认设为排队，同时让用户在设置里改默认。

Talos 主计划里定的是「Enter 排队、⌘Enter 立即引导、Esc 停止」，和 Cursor / Zed / Windsurf 一致。

### 2. 侧栏与会话状态

- 分组：按项目（Zed、Kero、OpenCode、T3 Code、Codex）。T3 Code 在项目内再分：置顶 → 活跃 → Working → Snoozed → Settled。
- 状态词汇很接近：运行中 / 等你（审批或提问）/ 完成未读 / 出错。Codex 的四种：Running、Needs input、Ready、Blocked。Kero：working、waiting、idle、error、exited。
- 「等你」要能一键到达：Kero ⌘J，Codex ⌘⌥A（下一个需要处理的会话）。
- 未读标记留到用户真的进去为止（OpenCode、Kero）；可手动标未读（Codex ⌘⇧U、OpenCode）。
- 在等后台事情的线程单独一栏（T3 Code 的 Working、OpenCode 收件箱的 Working）。
- ⌘1–9 跳会话几乎是通例（Codex、Kero、OpenCode）；OpenCode 按住修饰键时显示数字。
- 归档而不是删除是默认动作；永久删除藏得深并写明不可撤销（Zed）。T3 Code 对归档、置顶等给 5 秒撤销。
- 重命名：点标题直接改（Claude Code、Zed）；标题自动生成并可重新生成（Zed、T3 Code）。

### 3. 新会话

- 运行位置（本地 / worktree / 云）和基准分支放在输入框下方或旁边，不是单独一步（Codex、Claude Code、T3 Code、Windsurf）。
- worktree 只在会话开始时选（Windsurf 明确说了）。
- 被忽略的文件用 `.worktreeinclude` 拷进 worktree（Codex、Claude Code 同名约定）。
- 归档会话时删 worktree，但先存快照或先问（Codex、Kero、Claude Code）。

### 4. 输入框

- 通例：`@` 文件、`/` 命令、拖入和粘贴附件、模型与推理强度在框内、权限模式在框内或框下。
- `!` 开头直接跑 shell（Warp、OpenCode、Claude Code CLI）。
- 空输入框的 ↑ 取回上一条（Codex）；prompt 历史可搜（OpenCode）。
- 回复后的灰色提示建议，Tab 接受（Claude Code）。
- 模型选择器可搜索，有收藏和最近（Zed、OpenCode、Codex）。
- 打开的菜单里按数字直接选（Claude Code）。

### 5. 对话流

- **工具调用默认折叠成摘要**，详略分档：Claude Code（Normal / Thinking / Verbose）、OpenCode（Verbosity medium / high，一串调用折成一行）。Talos 主计划里的「普通 / 含思考 / 全部」三档和这个一致，但没做。
- 压缩显示成一条可展开的记录（Zed「Context Compacted」）。
- 工具摘要去掉 shell 包装，只留真正的命令（T3 Code）。
- 到步数上限时停下并给「继续」按钮（Windsurf）。
- 已发出的消息可以点开改了重发（Zed）。
- 回复的右键菜单：复制、复制为 Markdown、把线程打开成 Markdown（Zed）。
- 会话内查找 ⌘F（Codex、Cursor）；跳到某条消息（OpenCode）。

### 6. 审批

- 键盘：⏎ 批准，Esc 拒绝（Codex）。按钮上印快捷键（Windsurf）。
- 选项的通例是三档：只这一次 / 本会话 / 永久（存成规则）。Zed：Allow once、Deny once、Always for 工具、Always for 模式。OpenCode：Allow once、Allow always、Reject（并拒掉同会话其他待审批）。
- 运行前可以改命令（Windsurf）。
- 直接打字回复 = 拒绝并附意见（T3 Code）。
- 即使全权模式也拦危险的 `rm`（Claude Code、Zed 的内置规则）。
- 子 agent 的审批出现在父对话里，带名字（T3 Code）；对会话的授权同样适用于子 agent（Windsurf）。

### 7. 改动审查

- 范围的通例：**上一轮 / 未提交 / 对分支**（Kero 三个；Codex 五个：Unstaged、Staged、Commit、Branch、Last turn）。
- 入口就是 `+12 -1` 这个数字（Claude Code）。
- 粒度到 hunk：stage / unstage / revert（Codex）、accept / reject（Zed）、Revert / Keep（Kero，agent 在跑时不能 Revert）。
- **行内评论攒一批再一起发回给 agent**（Codex、Claude Code、Warp）。这是把审查意见交给 agent 的标准做法。
- Warp 的「Refine」：对一处 diff 说一句话让它重做。
- 所有 diff 叠在一个滚动区，滚到哪文件列表高亮哪（Kero）；「全部折叠」（Kero）；点文件名行折叠这个文件（Codex）。
- 点文件名在外部编辑器打开，⌘点某行跳到那一行（Codex）；文件路径到处是同一个右键菜单（Claude Code）。
- 一轮的 diff 不算进 pull / rebase 带来的文件（T3 Code）。
- 提交、推送、开 PR 在应用内完成（Codex、T3 Code）。

### 8. 回退

- 检查点和 Git 分开存（Cursor、Claude Code）。
- 恢复只回退文件，不删消息（Cursor）；或者给选项：代码和对话 / 只对话 / 只代码（Claude Code）。
- shell 命令造成的改动恢复不了，要说明（Claude Code）。

### 9. 面板

- 终端 ⌃`（Codex、Claude Code 相同）。终端属于会话，重启后保留滚动缓冲（OpenCode、Kero）。
- 右侧是带标签的面板区：审查、文件、浏览器、终端、计划（Codex、Claude Code、OpenCode）。没内容的面板不显示入口（Claude Code）。
- 旁支对话：问一句不打断主会话，`/btw` 或 `/side`（Codex、Claude Code、Cursor、Devin、T3 Code 都有）。

### 10. 通知

- 三类事件：做完、等你（审批或提问）、出错，各有开关（Kero、OpenCode、Warp、Codex）。
- **只在用户没在看的时候发**（全部一致）。Codex 给「从不 / 后台时 / 总是」三档。
- Dock 角标显示等你的会话数（Kero）。子 agent 的事件不通知（Warp、OpenCode）。
- 运行时阻止系统休眠（Codex、Zed）。

### 11. 快捷键的共识

| 作用 | 常见键 |
|---|---|
| 新会话 | ⌘N（Codex、Claude Code、Zed、Cursor） |
| 命令面板 | ⌘K（Codex、T3 Code、Cursor）或 ⌘P（Kero） |
| 侧栏 | ⌘B |
| 终端 | ⌃` |
| 上/下一个会话 | ⌘⇧[ / ⌘⇧] |
| 第 N 个会话 | ⌘1–9 |
| 会话内查找 | ⌘F |
| 设置 | ⌘, |
| 快捷键表 | ⌘/ |
| 字号 | ⌘+ / ⌘- / ⌘0 |
| 停止 | Esc |
| 审批 | ⏎ 批准，Esc 拒绝 |

## Conductor（未核实，仅搜索摘要）

- 每个 workspace 有自己的分支和 worktree，放在 `~/conductor/workspaces` 下。
- Diff Viewer：从侧栏打开改动的文件，可切统一视图，可按提交筛选；行内评论可发回给做出改动的 agent。
- 检查点：每一轮自动快照，能看最近一轮改了什么、回到更早的一轮；存在本地，和分支的 Git 历史分开。
- 有 Checks 页看 PR 状态，有「Review」动作让 agent 审查。

## 还缺的

- **真实界面**：本机装了 Claude 桌面应用、Kero、Cursor，可以直接打开逐屏截图，补上文档没有的视觉部分。
- Cursor 的 Agents Window、Conductor：文档抓不到，需要在浏览器里打开看。
- Alma、Magpie、Waku：还没找到资料。
- Codex 文档里没读的几页：Projects、Long-running work、Integrated terminal、Image inputs。
