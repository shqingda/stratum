# 主流 agent 桌面应用：界面与交互调研

2026-11-14 整理。给 Talos 做界面和交互时查的参考，先是横向对比，各产品的笔记附在本页末尾。

**来源的局限**：全部来自各家官方文档和更新日志的文字，没有一张真实截图。文档讲功能和快捷键讲得细，讲「长什么样」几乎不讲——间距、字号、颜色、动效、对话流里每种条目的画法，这里都没有。这部分要靠看真实应用补（见最后「还缺的」）。

## 各产品笔记（全文见本页末尾）

| 文件 | 产品 | 读到的程度 |
|---|---|---|
| 见下文「codex」 | Codex 桌面应用 | 较全：布局、侧栏、Review、worktree、设置、全部快捷键 |
| 见下文「claude-code-desktop」 | Claude Code 桌面版 | 较全，desktop 页后 1/3 没读到 |
| 见下文「cursor」 | Cursor | 不全：Agents Window 页 404；排队/插话、检查点、计划模式读到了 |
| 见下文「zed」 | Zed Agent Panel | 较全 |
| 见下文「warp」 | Warp | 较全，排队和审查写得最细 |
| 见下文「windsurf-devin」 | Windsurf Cascade、Devin | 中等 |
| 见下文「opencode」 | OpenCode 桌面版 | 没有桌面文档，从更新日志拼的 |
| 见下文「t3code」 | T3 Code | 中等，侧栏和权限较细 |
| 见下文「kero」 | Kero | 较全（它是终端，没有对话流） |
| 见下文「settings」 | 设置页（Cursor、Antigravity、Kero 实机截图） | 截图 7 张，在 shots/ |

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

## 附：各产品笔记

### Codex 桌面应用（OpenAI）（codex）

来源：官方文档（developers.openai.com/codex/app/* 现在跳到 learn.chatgpt.com/docs/*）。只有文字，没看到截图；视觉样式、对话流里每种条目怎么画，文档没写。

#### 窗口

- 同一个 ChatGPT 桌面应用里有 Chat、Work、Codex 三种模式，⌃1/2/3 切换。
- 左：侧栏（⌘B 开合），放会话、项目、Scheduled（定时任务）、Activity（铃铛）。
- 中：会话标题栏 + 对话流 + 底部输入框。worktree 会话的标题栏有「Hand off」「Create branch here」「Open」。
- 下：集成终端（⌃` 开合；⌘J 开合整个底部面板）。
- 右：标签式侧面板——Review（⌃⇧G）、文件树（⌘⇧E）、内置浏览器（⌘T）。⌘P 搜文件。
- 布局三档：全屏对话 / 分栏 / 隐藏标签，⌘⇧B 轮换；⌘⌥B 在对话与标签之间切。
- 会话可以弹成独立小窗，可「Always on top」。

#### 侧栏

- 「Chats」旁有筛选图标（含按时间排序）；「Add new project」加项目；悬停项目名出三点菜单可移除。
- 状态：Activity 里列出未读、运行中、等你回复的会话。浮窗宠物用四种状态：Running、Needs input、Ready、Blocked。
- 未读：⌘⇧U 标未读，⇧Esc 清掉全部未读标记，⌘⌥A 跳到下一个需要处理的会话。
- 置顶 ⌘⌥P，归档 ⌘⇧A（归档后在设置里可恢复，回到原来的位置），重命名 ⌘⌥R。
- ⌘1–9 跳到第 N 个会话；⌘⌥1–6 打开最近的会话。
- ⌘F 会话内查找，⌘G / ⌘⇧G 上下一个。搜索全部会话默认没快捷键，能匹配内容和分支名。

#### 新会话

- ⌘N 新会话；⌘⌥O 新建不属于任何项目的会话。
- 输入框下方选运行位置：Local / Worktree / Cloud；选 Worktree 时再选基准分支和本地环境（环境带 setup 脚本）。
- 也能用斜杠命令：`/local` `/worktree` `/cloud` `/project` `/model` `/reasoning`。
- 选错了位置：取消，输入框为空时按 ↑ 取回刚才的 prompt。
- 设置里有「Suggested prompts」：打开应用时给出结合上下文的建议。

#### 输入框

- 里面的控件：运行位置、分支、本地环境、模型与推理强度、权限、语音、发送/停止。
- 权限控件在输入框下方，有「Approve for me」（自动审查）。
- `/` 出命令列表，边打边筛：`/review` `/plan` `/goal` `/side` `/fork` `/compact` `/status` `/mcp` `/rename` `/usage` `/pin` `/archive`。启用的技能也在列表里。`$` 显式调用技能。
- 空输入框按 ↑ 取回上一条 prompt。
- 模型选择 ⌃⇧M，最近用过的模型 ⌃⌘M。

#### 运行中

- 设置 › General › 「Follow-up behavior」：运行中发的消息是「引导当前这一轮」还是「等下一轮」，用户自己选默认。
- 同页还能选 Enter 是直接发送，还是多行时/总是要 ⌘Enter。
- Goal 模式：输入框上方有一条进度行，带暂停/继续、改目标、清除。运行中仍可继续发消息。
- 「Prevent sleep while running」防止运行时休眠。
- 文档没写排队消息怎么显示、怎么改删，也没给停止的快捷键。

#### 审批

- 审批请求打开时：⏎ 批准，Esc 拒绝。
- `/approve`：对最近一次被自动审查拒掉的操作放行一次重试。
- 浏览器权限按站点分「Browse / Download / Upload」，各有「Requires approval / Always allow / Block」。
- 权限请求和提问的通知可以分别开关。

#### Review 面板

- 需要项目在 Git 仓库里；不是仓库就提示创建。
- 范围：Unstaged（默认）、Staged、Commit（选某个提交）、Branch（对基准分支）、Last turn（上一轮的改动）。
- 顶部「Stage all」「Revert all」；每个文件、每个 hunk 都能 stage / unstage / revert。
- 点文件名在外部编辑器打开；点文件名那一行的空白处折叠/展开这个文件的 diff；⌘点某一行跳到编辑器的那一行。
- 行内评论：悬停某行点「+」，写意见，提交；然后在对话里发一句，Codex 把评论当修改意见处理。
- `/review` 的结果以行内评论出现；可设成在单独的会话里跑（Detached）。
- 提交、推分支、开 PR 都在应用内完成。

#### 其他面板

- 终端：聚焦时 ⌃L 或 ⌘K 清屏。项目动作（本地环境里定义的脚本）在这里跑，顶栏有快捷按钮，第一个动作是 ⌘⇧D。
- `/plan` 进计划模式；没有单独的计划面板。
- 浏览器：⌘T 开标签，⌘⇧. 在浏览与评论模式间切。
- 旁支对话：`/side` 或 ⌘⌥S，问一句不打断主会话。

#### Worktree 与定时任务

- 托管的 worktree 放在 `$CODEX_HOME/worktrees`，默认 detached HEAD。「Hand off」把会话连同代码在 Local 和 Worktree 之间搬。
- 仓库根的 `.worktreeinclude` 列出要拷进 worktree 的被忽略文件（如 `.env`）。
- 默认只保留最近 15 个托管 worktree；置顶或在跑的不删。归档会话会删它的 worktree，删前存快照，重新打开时可恢复。
- Scheduled：侧栏的一个收件箱。独立任务每次开新会话；会话内的任务回到那个会话。应用要一直开着。

#### 通知

- 一轮完成的提醒：从不 / 只在应用在后台时 / 总是。权限请求和提问各有开关。

#### 设置分页

General、Profile、Keyboard Shortcuts（可按名字搜，也可直接按键搜）、Notifications、Appearance（主题、强调色、界面字体和代码字体）、Pets、Browser、Computer Use、Personalization（AGENTS.md、记忆）、Archived chats。

#### 快捷键（macOS）

| 作用 | 键 |
|---|---|
| 命令菜单 | ⌘K 或 ⌘⇧P |
| 设置 / 快捷键表 | ⌘, / ⌘/ |
| 打开文件夹 | ⌘O |
| 后退 / 前进 | ⌘[ / ⌘] |
| 字号 | ⌘+ / ⌘- / ⌘0 |
| 侧栏 | ⌘B |
| 底部面板 / 终端 | ⌘J / ⌃` |
| 新会话 | ⌘N |
| 归档 / 标未读 / 置顶 / 重命名 | ⌘⇧A / ⌘⇧U / ⌘⌥P / ⌘⌥R |
| 会话内查找 | ⌘F |
| 上/下一个会话 | ⌘⇧[ / ⌘⇧] |
| 下一个需要处理的会话 | ⌘⌥A |
| 第 N 个会话 | ⌘1–9 |
| 模型选择 | ⌃⇧M |
| 批准 / 拒绝审批 | ⏎ / Esc |
| Review / 文件树 / 搜文件 | ⌃⇧G / ⌘⇧E / ⌘P |
| 布局轮换 / 全屏对话 | ⌘⇧B / ⌘⇧F |
| 旁支对话 | ⌘⌥S |
| 复制工作目录 / 会话链接 | ⌘⇧C / ⌘⌥L |

#### 值得抄的细节

- 「取回上一条 prompt」用空输入框的 ↑。
- 会话卡住时的排查提示：先看是不是有审批在等。
- 快捷键设置可以直接按组合键来搜。
- 归档会话删 worktree 前存快照，重开可恢复。
- 一轮完成的通知分「从不 / 后台时 / 总是」三档。

### Claude Code 桌面版（Claude 桌面应用的 Code 页）（claude-code-desktop）

来源：code.claude.com/docs/en/desktop、desktop-quickstart、interactive-mode、permission-modes、checkpointing。desktop 页很长，只读到前 2/3。本机装了这个应用，可以直接看真实界面补全。

#### 窗口

- 每个会话有标题栏：标题（点击改名）、标题旁的小箭头（会话菜单）、图标按钮 Terminal / Changes / Browser / Files（悬停显示名字）、⋮ 溢出菜单。
- 中间是对话流；旁边是「会话详情栏」（session rail），几张卡片：在哪运行、Changes、后台任务、Context。窗口够宽才出现，可隐藏。
- 底部输入框；输入框下面一行：环境、项目文件夹、权限模式、模型、用量环。
- 面板种类：对话、diff、浏览器、终端、文件、计划、任务、子 agent。拖面板标题可移动，拖边缘改大小，⌘\ 关掉聚焦的面板。面板可以最小化到角落，也可以弹成独立窗口再停靠回来。
- 窗口窄时放不下的按钮收进 ⋮。没有专门按钮的面板（后台任务、计划）有内容时才出现在 ⋮ 里。

#### 侧栏

- 「+ New」新会话（⌘N）。悬停分组标题出「Group and sort」。
- 归档：悬停会话 › 菜单 › Archive。worktree 里有未提交改动时会提示，「Archive anyway」丢弃。Archived 页里可 Unarchive。可设置 PR 合并或关闭后自动归档。
- 并排看：⌥点击会话，或右键 › Open in › Split view；⌘点击在新窗口打开。

#### 新会话

- 输入框下面一行从左到右：环境（Local / Cloud / SSH）、项目文件夹（菜单里有 Open folder…）、权限模式、模型。
- ⌘⌥1–9 选环境；⌘⌥0 打开「New environment」对话框。
- Worktree 开关：这个会话在自己的 worktree 里跑，默认在 `<项目>/.claude/worktrees/`；`.worktreeinclude` 拷贝被忽略的文件。
- 建议任务卡片：Start / Do it here / Options / Start all。

#### 输入框

- Enter 发送。可拖入图片、PDF 和其他文件。
- 左边「+」：Add files or photos、Add folder、Slash commands、Add connectors、Add plugins。
- `@文件名` 加文件；`/` 出命令和技能（`/compact` `/context` `/code-review` `/rewind` `/btw`）。
- **提示建议**：回复之后，空输入框里出现灰色建议文字；Tab 或 → 接受；没接受就按 Enter 不会把它发出去；一打字或加附件它就消失。
- 模型 ⌘⇧I，权限模式 ⌘⇧M，推理强度 ⌘⇧E；点用量环看套餐用量。打开的菜单里按 1–9 直接选第 N 项。

#### 运行中

- Enter 发一条纠正，不停止当前运行；Claude 在当前动作做完后读到它，下一步之前调整。
- 停止按钮立即打断；Esc 停止回复，已做的工作保留。
- CLI 的说法（桌面版文档没确认）：运行中发的消息显示为灰色排队项；在第一行按 ↑ 可以把它取回来编辑；Ctrl+Enter 立即发送排队消息。

#### 对话流

- 三档显示（会话菜单 › Transcript view，或 ⌃O 轮换）：
  - Normal：工具调用折叠成摘要，回复全文显示。
  - Thinking：再加上思考。出现过思考之后菜单里才有这一档。
  - Verbose：每次工具调用、读文件、中间步骤全显示。
- 对话流里会出现的卡片：代码审查结果（按文件分组）、预览服务器卡片（带 Open）、建议任务、来自其他会话的消息。

#### 权限模式与审批

- 模式：Manual（改文件、跑命令前都问，能看到 diff 再接受或拒绝）、Accept edits（自动接受编辑和 mkdir/touch/mv）、Plan（只探索不改）、Auto（后台分类器把关）、Bypass permissions。
- 浏览器站点：Allow once / Always allow / Deny。电脑操作按应用：Allow for this session / Deny，并显示权限档（只看 / 只点 / 完全控制）。
- 计划审批的选项（CLI 文案）：批准并切到自动模式 / 批准但编辑逐个确认 / 继续规划。
- 即使在 Auto 和 Bypass 下，危险路径的 `rm` 也会被拒。

#### Diff 与审查

- 入口：Changes 卡片上的 `+12 -1`，或标题栏的 Changes（⌘⇧D）。按文件逐个看。
- 评论：悬停某行点行号旁的「+」，输入，Enter 加上；⌘Enter 把所有评论一起提交。Claude 改完后给出新的 diff。
- 右键任意文件路径（对话里、diff 里、文件面板里都行）：Attach as context、Open in（VS Code / Cursor / Zed）、Show in Finder、Copy › 绝对路径 / 相对路径 / 文件名。
- `/code-review` 出一张审查卡片：Walk through in diff、Fix this one、Apply fixes。
- PR：出现在 Changes 卡片里，带 CI 状态；⋮ 里有 Auto-fix、Merge when ready。
- 检查点（CLI）：`/rewind` 或空输入框连按两次 Esc 打开菜单：恢复代码和对话 / 只恢复对话 / 只恢复代码 / 从这里起做摘要。shell 命令造成的改动恢复不了。

#### 终端、浏览器、文件

- 终端：⌃`，在会话文件夹里打开，面板标题栏的「+」加标签。右键对话里的文件夹 › Open in terminal。
- 浏览器：⌘⇧B，带标签；有 Dev servers 菜单；⌘⇧S 选页面元素发给 Claude。dev server 配在 `.claude/launch.json`。
- 文件面板：可直接编辑，自动保存；和磁盘冲突时给 Override / Discard。

#### 并行与其他

- 旁支对话：⌘; 或 `/btw`，不存盘。
- 会话结束时如果你没在看它，发系统通知。
- Remote Control：每个会话一个开关，可在别的设备上接着操作。

#### 快捷键（macOS）

| 作用 | 键 |
|---|---|
| 快捷键表 | ⌘/ |
| 新会话 / 关闭会话 | ⌘N / ⌘W |
| 上/下一个会话 | ⌘⇧[ / ⌘⇧]，⌃Tab |
| 停止 | Esc |
| 接受提示建议 | Tab 或 → |
| Diff / 浏览器 / 终端 | ⌘⇧D / ⌘⇧B / ⌃` |
| 关掉聚焦的面板 | ⌘\ |
| 旁支对话 | ⌘; |
| 轮换显示档 | ⌃O |
| 权限模式 / 模型 / 强度 | ⌘⇧M / ⌘⇧I / ⌘⇧E |
| 菜单里选第 N 项 | 1–9 |
| 加评论 / 提交全部评论 | Enter / ⌘Enter |

#### 值得抄的细节

- `+12 -1` 这个数字本身就是进入审查的入口。
- 提示建议只在空输入框里出现，灰色，不会被误发。
- 思考没出现过时，「Thinking」这一档就不出现在菜单里。
- 文件路径在任何地方右键都是同一个菜单。
- 没内容的面板不占按钮位置，有内容了才出现。
- Context 卡片标题上直接显示上下文占用百分比。

### Cursor（Agent 与 Agents Window）（cursor）

来源：cursor.com/docs 的 agent/review、terminal、modes、configuration/kbd，以及 changelog。**Agents Window 的文档页是 404，没读到**；它的窗口布局、侧栏、新建流程都缺。下面只有读到的部分。

#### 运行中：排队与插话（读得最清楚的一块）

- 运行中按 Enter：消息进队列，**显示在当前任务下面，按顺序排**，可以拖动调整顺序，当前任务做完后依次执行。
- ⌘Enter：立即发送。消息接在最近一条用户消息后面，随工具结果一起送给模型。
- 插话（steer）：打一条跟进，点「Send now」，或者连按两次 Enter。它在 agent **下一次工具调用时**送达，不打断正在进行的动作。
- 取消生成：⌘⇧⌫，或点 Stop。
- 快捷键表里 Return 标着「Queue message (default)」，说明默认行为可以在设置里改。

#### 输入框

- `@` 提及，`/` 命令。`/side` 或 `/btw` 开旁支对话，`/goal` 设长期目标。
- ⇧Tab 在 agent 模式之间轮换（Agent / Ask / Plan）；⌘. 打开模式菜单；⌘/ 在模型间轮换。
- ⌘⇧L 把选中的代码加进对话；⌘V 粘贴代码或日志时作为上下文加入。
- 语音：输入框为空且 agent 空闲时，发送按钮的位置换成「开始语音」的波形按钮。

#### 计划模式

- ⇧Tab 轮到 Plan；也会根据关键词自动建议。
- agent 先问澄清问题、调研，写出一份你可以编辑的计划，然后「Click to build」。
- 计划默认存在用户目录，「Save to workspace」移进项目。
- 官方建议：跑偏了就回退改动、改计划重来，不要靠一条条跟进去修。

#### 检查点

- 做较大改动前自动建检查点，记录所有被改文件的状态。
- 点对话时间线上的任一检查点可预览当时的文件，再恢复。也可以用之前请求上的「Restore Checkpoint」，或悬停消息时出现的「+」。
- 恢复**只回退文件，不删对话里的消息**。检查点存在本地，和 Git 分开。

#### 审查

- diff 视图随改动实时更新。
- 「接受全部」⌘Enter，「拒绝全部」⌘⇧⌫。
- 「Review › Find Issues」让 agent 专门审一遍自己的改动。
- 论坛（非官方文档）：Agents Window 里是按文件的 stage / unstage / revert；逐处接受或撤销要到编辑器窗口。

#### 终端与审批

- 「Run Mode」决定命令什么时候直接跑、什么时候问、什么时候进沙箱。沙箱拦未授权的文件访问和网络。细节页 404，没读到。

#### 快捷键

| 作用 | 键 |
|---|---|
| 排队（默认） | Return |
| 立即发送 | ⌘Return |
| 取消生成 | ⌘⇧⌫ |
| 接受 / 拒绝全部改动 | ⌘Return / ⌘⇧⌫ |
| 轮换模式 | ⇧Tab |
| 模式菜单 | ⌘. |
| 轮换模型 | ⌘/ |
| 新对话 / 新标签 | ⌘N / ⌘T |
| 上/下一个对话 | ⌘[ / ⌘] |
| 搜对话 / 对话内查找 | ⌘K / ⌘F |

#### 值得抄的细节

- 排队和插话是两个不同的手势：排队等这一轮结束，插话落在下一次工具调用，都不会硬切正在做的事。
- 排队的消息就列在当前任务下面，能拖动排序。
- 恢复检查点不动对话。

### Zed Agent Panel（zed）

来源：zed.dev/docs/ai 的 agent-panel、zed-agent、tools、tool-permissions、parallel-agents、agent-settings、external-agents。思考和工具卡片具体怎么画，文档没写。

#### 布局

- Agent Panel 顶栏右上「+」开「New Thread…」菜单；底部是消息编辑器；编辑器上方有一条可折叠的条，列出被改的文件。
- 面板底部有滚动箭头，跳到最新一条或回到开头。左下的准星图标让编辑器跟随 agent 正在看的位置。
- Threads Sidebar 是单独一栏（⌘⌥J）。「Panel Layout › Agentic」把 agent 和线程栏放左边、项目和 Git 放右边；「Classic」是传统编辑器布局。

#### 线程栏

- 按项目分组，每组有标题；每行是标题 + 状态标记 + 是哪个 agent。worktree 的线程挂在主项目下面。
- 项目标题上的「+」直接在这个项目里开线程。
- ⌃Tab 线程切换器。
- 归档：悬停点归档图标，或选中按 ⇧⌫。**在跑的线程不能归档**。
- Thread History（⌘G）含已归档的，模糊匹配标题；垃圾桶图标是永久删除，文档明说不可撤销。
- 归档一个线程会连它的 worktree 一起从磁盘删掉；恢复线程时 worktree 也恢复。

#### 输入

- Enter 发送。⇧⌥Esc 展开编辑器。
- `@` 可加：文件、目录、符号、以前的线程、技能、诊断信息、分支 diff、URL。
- 粘贴多行代码会自动变成 @ 引用；⌘⇧V 粘贴原文。图片可拖入、粘贴或 @。
- 模型选择 ⌘⌥/；悬停模型点星标收藏，⌥Tab 在收藏间轮换。
- 模型不支持工具调用时显示「No tools」。

#### 运行中

- 运行中发的消息**默认排队**，agent 做完再发。
- 排队的消息上有「Steer」开关：打开后在下一步（通常是工具调用和回复之间）送达。
- 排队消息可以改、可以删，单条或全部。
- 打断：点停止，或对排队消息「Send Now」（连按两次 Enter）。
- **任何已发出的消息都能改**：点它的卡片，改 prompt 或上下文，重新提交。

#### 对话流

- 流式输出，带「正在用哪个工具」的指示。
- 右键回复：Copy、Copy as Markdown、Copy This Agent Response、Scroll to Top / Bottom、Open Thread as Markdown。
- 压缩显示为一条「Context Compacted」，可展开看摘要。
- 每条回复下有 👍👎；token 用量显示在 profile 选择器旁边。
- 标题自动生成，点标题可改；菜单里有「Regenerate Thread Title」。

#### 工具权限

- 全局默认 confirm，可设 allow / deny；每个工具可单独设默认，并有 `always_allow` / `always_deny` / `always_confirm` 三组正则。
- 优先级：内置安全规则 › always_deny › always_confirm › always_allow › 工具默认 › 全局默认。
- 审批卡片的菜单：**Allow once / Deny once**、**Always for 〈工具〉**、能抽出安全模式时还有 **Always for 〈模式〉**。
- 内置规则（只针对终端，不可覆盖）：对 `/`、`~`、`.`、`..` 的 `rm -rf`。
- 串联的命令会拆开逐条检查。设置页带一个「测试你的规则」的小工具。

#### 审查改动

- 面板里报出改了哪些文件、多少行。「Review Changes」（⌃⇧R）打开一个多文件合并的 diff 页。
- 每个 hunk 接受或拒绝，或整体接受/拒绝。
- `single_file_review` 打开后，文件自己的标签页里也直接显示 agent 的 diff 和同样的按钮。

#### 通知

- 只在发出 prompt 后把 Zed 切到后台时才提醒：桌面通知、声音，或两者。
- 生成期间阻止系统休眠；等确认时不阻止。

#### 自动压缩

- 到 90% 自动压缩。上下文窗口小于 8 万 token 时出一条横幅，提供「Start New Thread」。「New From Summary」带着摘要开新线程。

#### 快捷键（macOS）

| 作用 | 键 |
|---|---|
| 新线程 | ⌘N |
| 线程栏 / 聚焦线程栏 | ⌘⌥J / ⌘⌥; |
| 历史 | ⌘G |
| 归档 | ⇧⌫ |
| 线程切换器 | ⌃Tab |
| 审查改动 | ⌃⇧R |
| 模型选择 / 收藏轮换 | ⌘⌥/ / ⌥Tab |
| 加入选中内容 | ⌘> |
| 粘贴原文 | ⌘⇧V |

#### 值得抄的细节

- 排队消息上直接放「Steer」开关，一条消息在排队和插话之间切换。
- 已发出的消息可以点开重新编辑。
- 分栏的分隔线双击回到默认宽度。
- 「在跑的不能归档」「永久删除不可撤销」这类限制写得很直白。

### Warp（agent 模式）（warp）

来源：docs.warp.dev 的 agents 与 code 部分（terminal-and-agent-modes、prompt-queueing、code-diffs、interactive-code-review、code-review、通知与权限）。Warp 是终端，agent 对话是终端里的一种视图。

#### 布局

- 每个标签或分栏有两种模式：终端（默认）和 agent 对话。对话视图背景色不同，输入区多一排工具：模型选择、语音、附图。
- 左边 Conversation Panel（⌘⇧H）；右上铃铛是通知收件箱；右上「Code review」按钮打开右侧审查面板。

#### 对话列表与状态

- 两段：**Active**（你发过消息的）和 **Past**（历史，带标题、相对时间、工作目录）。顶部搜索框按标题筛。
- 每个标签上有状态图标：工作中、被卡住、完成、出错；有未读通知时加角标。
- 收件箱筛选：All tabs / Unread / Errors。
- 上下文占用：超过 20% 才显示指示，接近上限变红。用量卡片可展开「Context window used」，分 System Prompt、Tool Definitions、Conversation History、Latest Input、Images、Other。

#### 输入

- `@` 加文件、符号等上下文；`/` 命令：`/new` `/plan` `/conversations` `/compact` `/fork` `/fork-and-compact` `/fork from` `/model` `/queue`。
- `!` 开头强制当 shell 命令。⌘I 在 shell 和 agent 之间切。
- 自动识别输入是命令还是 prompt，识别结果用洋红色标「(autodetected)」；手动改过一次后这条输入就不再自动改。
- 右键输入区「Edit agent toolbelt」可以重排、隐藏那排控件。

#### 运行中（文档写得最细）

- 默认提交方式是 **Interrupt response**：发新 prompt 会取消正在进行的回复。另一种是 **Queue until response finishes**。两种默认值在设置里选。
- 自动排队开关：输入区上方状态条里的「时钟加号」图标，⌘⇧J。`/queue 〈prompt〉` 单独排一条。
- 排队面板「N queued」在状态条和输入区之间。每行：拖动手柄排序、立即发送（↑ 箭头）、编辑（铅笔；Enter 保存，Esc 取消）、删除（垃圾桶）。
- **输入框为空时按 Enter 发送队首**，数量旁边有「⏎ to send」提示。
- 出错、停止或 ⌃C 时队列暂停，队首那条回到空的输入框里。
- 队列不跨重启保存。
- agent 启动的长时间命令在跑时：默认「等命令结束再发」，可改「立即发送」。
- ⌃C 停止 agent；Esc 退出对话，进行中的对话要在约 2 秒内再按一次确认。

#### 权限

- 每个 profile 按权限类型设四档：**Agent Decides / Always ask / Always allow / Never**。
- 类型：应用代码 diff、读文件、建计划、执行命令、与运行中的命令交互、提问。
- 命令 allowlist 和 denylist 是正则；默认 denylist 有 `wget` `curl` `rm` `eval`；denylist 压过 allowlist 和 Always allow。
- 「Run until completion」（⌘⇧I）：这次任务全部自动批准。

#### 改动与审查

- 代码 diff 在内置 diff 编辑器里打开，按 hunk 分组。↑↓ 在 hunk 间移动，←→ 换文件。
- Enter 接受；**R「Refine」**用自然语言让它重新生成这份 diff；**E「Edit」**把 diff 变成可直接编辑。接受之前不落盘。
- Code Review 面板：默认看未提交改动，可对 main 或任意分支比较。每个 hunk 的边栏有「Revert diff hunk」；「Discard all」先确认。
- 交互式审查：在改动的行上加行内评论，攒一批一起发，agent 一次处理完返回新 diff。

#### 通知

- 三类：Complete、Request（等输入，含审批）、Error。
- 窗口角落的 toast 最多同时两条，自动消失，悬停暂停，点击打开那个会话。
- 应用在后台时发系统通知。子 agent 的事件不进 toast 和收件箱。

#### 快捷键（macOS）

| 作用 | 键 |
|---|---|
| 从终端开 agent 对话 | ⌘↩ |
| 对话菜单 | ⌘Y |
| 对话列表 | ⌘⇧H |
| shell / agent 切换 | ⌘I |
| 停止 | ⌃C |
| 全部自动批准 | ⌘⇧I |
| 审查面板 | ⌘⇧+ |
| 计划面板 | ⌘⌥P |
| 自动排队 | ⌘⇧J |
| 快捷键面板 | ? |

#### 值得抄的细节

- 队列面板每行四个动作齐全（排序、立即发、改、删），空输入框 Enter 发队首。
- 停止后队首回到输入框，不丢也不自动续跑。
- diff 上的「Refine」：不接受也不拒绝，说一句话让它重做这一处。
- 上下文占用低于 20% 时根本不显示指示。
- toast 悬停暂停消失。

### Windsurf Cascade 与 Devin（windsurf-devin）

来源：docs.windsurf.com（现在跳到 docs.devin.ai/desktop/*）和 docs.devin.ai。Cascade 现在被称为 Devin Desktop 里的旧版 agent，新对话默认用 Devin Local。两者的 diff 界面、状态标记、通知，文档都没写。

#### Cascade

**并行与 worktree**
- 面板左上的下拉在多个同时进行的 Cascade 之间切换。
- 文档直接警告：两个 Cascade 改同一个文件会竞争，后一个的编辑可能失败。推荐用 worktree 隔离。
- 输入框右下有「Worktree」开关，**只能在会话开始时选**。有「merge」把改动合回去。删对话会删它的 worktree。

**输入**
- 模型在输入框下方的菜单里选。模式：Code（改代码）/ Chat（只回答，可给出代码让你接受插入）。
- Devin Local 的模式更多：Code、Smart、Ask、Plan、Bypass Permissions；可在输入框下选，也可打 `/code` `/smart` `/ask` `/plan` `/bypass`。
- `@` 提及以前的对话时，带入的是摘要和检查点，不是全文。
- `/` 出命令列表；Workflow 是存在 `.devin/workflows/` 里的 markdown，以 `/名字` 调用。

**运行中**
- 运行中打字按 Enter：排队，当前任务做完后按顺序执行。
- **在空输入框上再按一次 Enter：把排队的消息立即发出去。**
- 排队的消息发出前可以删。

**对话流**
- 较长的任务会在对话里写一份 Todo 列表，后台有个规划 agent 持续修订。
- 每条 prompt 默认最多 40 次工具调用，到上限停下，出一个「continue」按钮。可设成自动继续。

**审批（Devin Local）**
- 规则三类：Deny（最高）、Ask、Allow。写法如 `Exec(git status)`。
- 命令卡片上的选择：只运行这一次 / 本会话都允许 / 永久允许（加一条规则）。**运行前可以改命令**；还有一个魔杖按钮，用一句话描述让它改写命令。按钮上直接印着快捷键提示。
- Smart 模式：让一个快模型判断命令是否安全，安全的自动放行，高风险类别照样问。
- 对会话的授权同样适用于它的子 agent。

**回退**
- 悬停某条 prompt，点右侧的回退箭头，代码库回到那一步。文档明说回退不可逆。
- 可以在对话里建「命名检查点」，之后回到它。

**其他**
- 计划存到 `~/.devin/plans/`；在新对话里提到它就能接着做。计划文件上有「Implement」按钮，切到 Code 模式开工。
- 选中报错有「Explain and Fix」。

#### Devin（云端会话）

**布局**
- 会话是一份对话式工作日志。工具有 Shell、IDE（内嵌 VS Code）、Browser 三个页。
- **Progress 页把 shell 命令、代码编辑、浏览器操作合在一个时间线里**；点任一步跳到对应工具的那个时刻。
- 旁支对话在日志旁边的面板里打开。

**输入**
- 首页有 Ask / Agent 开关：Ask 用来看代码、做规划；Agent 执行。
- Ask 模式里「Construct a Devin Prompt」帮你把范围写清楚，「Send to Devin」转成执行。
- `@` 可提：Repos、Files、Macros、Skills、Secrets、Sessions。
- 会话进行中可以换 agent 档位，下一条消息起生效。

**运行中**
- 旁支对话（`/btw`，或消息的悬停菜单）：问问题不打断主会话；只读，不能改文件、不能跑命令。
- 要自己接手 IDE：先暂停，改完恢复时告诉 Devin 你动了什么。

**Shell 页**
- 命令历史列表，每条带输出预览；三点图标复制命令；还没执行到的命令显示为灰色；点一条跳到那个时刻。

**PR 审查（Devin Review）**
- 布局二选一：文件树视图 / 导览视图（把相关改动归成一组一组看）。
- 每个文件：标记已看、展开全部行、Markdown 的渲染/源码切换。选中一段：Add to Chat / Suggest change。
- ⌘F 搜索，选项有「只看改动的行」「跳过已看的文件」。
- 列表里的 PR 状态：CI failing、Changes requested、Review required、Ready to merge。
- 不在输入时的单键快捷键：`a` 批准、`r` 要求修改、`m` 合并、`n` 载入最新。

#### 值得抄的细节

- 空输入框 Enter = 把排队的立即发出（和 Warp 一致）。
- 审批卡片上能直接改命令再运行。
- 到工具调用上限时停下并给「continue」，而不是静默跑下去或静默结束。
- 把命令、编辑、浏览器操作合成一条可点的时间线。
- 回退不可逆这件事写在界面文案里。

### OpenCode 桌面版（opencode）

来源：opencode.ai 的 changelog（v2.2.0–v2.3.0 的桌面条目）和 docs 的 tui、keybinds、permissions、agents。**没有专门的桌面版文档页**（404），桌面版的界面是从更新日志的措辞里拼出来的；TUI 的部分来自文档。

#### 窗口

- 标题栏里有搜索框和「最近关闭」菜单。
- 左侧栏：项目、Home、收件箱（Inbox）。
- 中间：会话标题 + 时间线 + 底部输入框。
- 右侧面板带标签，可以再分栏；底部还有一个可拖动的带标签面板。标签种类：浏览器、文件树、终端、审查。

#### 侧栏

- 项目有自动生成的头像，可拖动排序。
- 收件箱有「Working」一栏，按开始执行的时间排；未读分组显示。
- 「Mark as unread」：标签、Home 行、项目行、收件箱卡片的菜单里都有，点了之后小圆点一直留到你进这个会话为止。
- 失败的运行在标签上有标记。
- 活动胶囊：列出正在跑的 shell，可以直接停掉。
- 实验性的「Recent」侧栏：最近七天，最多 30 行，可本地置顶。

#### 新会话

- 新会话按钮把「选项目」和「选 worktree」分成两个动作。
- 新会话页上点当前分支名，打开「Run session in」的 worktree 选择器。⌘⌥O 选 worktree，⌘⌥B 选基准分支。
- 模型选择器可搜索：收藏、最近用过、提供方、变体。
- 内置两个主 agent：Build 和 Plan；TUI 里 Tab 切换。

#### 输入框

- 粘贴和拖入的文件成为附件。`@` 出菜单，可选文件和会话。
- 斜杠命令：`/goal` `/schedule` `/reload` `/usage` `/undo` `/redo`。
- `!` 开头直接跑 shell；⌘⇧X 切换 shell 模式。
- prompt 历史可搜索，保留最近 200 条。
- 输入框内置 Readline 键位：⌃A/E/B/F、⌥B/F、⌃K/U/W。

#### 运行中

- 排队的消息在同步期间也能编辑。
- **⌥Enter：把这条跟进排到队列最前面。⌘Enter：对下一条排队的 prompt 插话（steer）。**
- ⌃G 关掉弹层并中止正在进行的回复；TUI 里是 Esc。
- 设置里「发送后是否滚到底」可选保持不动。

#### 对话流

- 默认把一连串工具调用和思考**折叠成一行活动摘要**，可展开。「Verbosity」设置（medium / high）决定是否每一步都显示。
- 子 agent 的卡片显示在输入框附近：模型、状态、已用时间。后台子 agent 空闲时会向主会话报告。
- 从输入框按 PageUp / PageDown 翻对话。
- 命令面板里有「Jump to message」（⌘⌥G）。

#### 权限与提问

- 规则解析成 allow / deny / ask，没匹配到规则就是 ask。
- 审批三个回答：**Allow once**、**Allow always**（把工具提议的模式存成这个项目的规则）、**Reject**（同时拒掉这个会话里其他所有待处理的请求）。
- 提问显示在一个 dock 里；数字键直接选答案，多选显示复选框。

#### 审查与撤销

- 审查面板可开合；会话 diff 加载时不需要读每条消息。
- `/undo`：去掉最后一条用户消息、之后的回复，以及它们造成的文件改动（靠 Git）。`/redo` 恢复。

#### 终端与标签

- 终端属于会话，服务重启后保留滚动缓冲，并和 TUI 同步。
- 侧面板标签：⌘1–9 跳转，⇧⌘[ / ⇧⌘] 前后，⌘W 关标签，⇧⌘T 重开刚关的，菜单里有「关闭右侧标签」。
- **按住修饰键时，标签上显示它的数字。**

#### 通知

- 提问、权限请求、会话出错、会话完成四类，各自可设声音。子 agent 的事件不发通知。只在窗口失焦时发。

#### 设置

- General、Shortcuts、Notifications、About、Experimental。
- 外观：界面布局、减弱动效、时间线宽度、持久缩放。

#### 值得抄的细节

- 两个跟进键分开：排到最前（⌥Enter）和插话（⌘Enter）。
- 一串工具调用折成一行，详略由一个设置控制。
- 拒绝一个审批时，同会话其他待审批的一并拒掉。
- 「标为未读」的圆点留到你真的进去看为止。
- 按住 ⌘ 显示数字。

### T3 Code（t3code）

来源：github.com/pingdotgg/t3code 的 README 和 docs/user（thread-sidebar、permission-modes、source-control、faq）、t3.codes、Better Stack 的介绍文（2026 年 3 月，标签可能过时）。它是多个 agent（Claude Code、Codex、Cursor、OpenCode 等）的统一控制台，自己不带模型。composer 和 keybindings 两页没读。

#### 布局

- 三栏：左项目，中间是所选项目的线程，右边是对话（prompt、回复、agent 的计划、工具调用）。
- 右侧还有 Diff 面板和「线程详情卡」。详情卡列出这个线程的终端或 agent 启动的本地服务，可以在内置浏览器面板里打开，也可以 Stop。

#### 侧栏（文档写得很细）

- 项目内的线程顺序：**置顶 → 活跃 → Working（折叠；在等子 agent、等 PR 检查、或有后台命令的线程）→ Snoozed → Settled**。
- 线程完成、失败、或被审批卡住时，回到活跃列表。
- 线程图标反映 PR 状态：打开、已合并、已关闭。
- 菜单：Pin、Settle thread、Snooze › Custom…、Auto-settle behavior、Regenerate title。
- 用量受限的线程显示「Limited」和重置时间，可「Snooze until reset」「Wake now」。

#### 新线程

- 开了多个项目时，「New thread」先出项目选择；⇧点击直接在当前项目里开。
- 可以不选项目开线程，放在一个临时目录里。
- 输入框旁选工作方式（New worktree）和基准分支。
- **⇧点击多个模型：同一条 prompt 同时发给几个模型，各自一个线程、一个 worktree。**

#### 输入与运行中

- 控件：模型、推理强度、Chat / Plan 模式、权限。权限不是默认值时，显示在模型旁边。
- ⌘Enter：开始这个线程，同时立刻打开下一份草稿。
- 被用量上限卡住时，排队的消息保留。空输入框里的「Resume」继续被限流或被打断的一轮。
- 停止会连带停掉这个线程派出去的子 agent。
- 把文件拖到侧栏的某个线程行上，就附加给那个线程。

#### 权限

- 四档：Supervised、Auto-accept edits、Auto（交给提供方的自动审查）、Full access。新线程的默认值在设置里，出厂是 Full access。
- 审批出现在对话里。子 agent 的审批请求显示在父对话里，带子 agent 的名字。
- Claude 的审批待处理时，直接打字回复 = 拒绝并附上意见。

#### Diff 与 Git

- **一轮的 diff 不包含这一轮只是拉取（pull、rebase）带进来的文件。**
- Diff 面板可选来源：提交（最近 100 个）。统一/分栏两种视图。
- 提交弹窗列出改动文件和行数；「Commit & push」是 ⌘⏎。
- PR 页：审查、请求评审、合并。「Mark files as viewed」折叠文件并更新计数。
- 工具调用可展开看完整命令和输出；摘要里会把 shell 包装去掉，只留真正的命令。

#### 命令面板

- ⌘K。搜线程可按标题、分支、PR、项目；消息里的匹配单独列在「In messages」下。

#### 快速动作

- 线程工具栏的播放图标后面是快速动作；「Add Action」新建，每个动作有名字、可选快捷键、命令，可设为建 worktree 时自动跑。

#### 值得抄的细节

- **置顶、取消置顶、settle、snooze、归档、丢弃草稿，都给 5 秒的撤销 toast。**
- 侧栏把「还在等后台事情的线程」单独收成 Working 一栏，不和需要你处理的混在一起。
- 一条 prompt 多模型对比。
- 工具摘要去掉 shell 包装。

### Kero（kero）

来源：kero.sh 的文档（Shortcuts、Status & Notifications、Sessions & Projects、Configuration、Changes、Panes）和 GitHub README。Kero 是给 coding agent 用的终端，和 Talos 一样用 mygo 原生界面写，源码 MIT 可读；本机装了，可以直接看真实界面。它没有自己的对话流和输入框，每个分栏就是一个真终端，跑 agent 自己的 TUI。所以对话相关的部分不适用，值得看的是侧栏、状态、改动面板和通知。

#### 布局

- 左侧栏：项目和会话。
- 中间：会话里的一个或多个分栏，每个是终端或浏览器。
- 右侧栏两个页：**Changes** 和 **Files**（⌥⌘B 开合）。从这两页打开的文件，以卡片形式出现在分栏旁边。
- Quick Terminal：从屏幕顶部下拉，盖在别的应用上面。

#### 侧栏与状态

- 会话归在项目下面。每行最前面是这个会话里那个程序的图标（Claude Code、Codex、Gemini、Node、普通终端）。**agent 在跑时图标有颜色，shell 回到提示符后变淡。**
- 分栏状态五种：working、waiting、idle、error、exited。
- 分了栏的会话，名字后面显示分栏数。分支名和领先/落后数悬停时才显示。
- 程序可以用 `kero report` 上报一句状态，显示在会话名下面和分栏标题里。
- **done 和 error 的标记一直留着，直到你在那个分栏里打字**——结果会等你。
- 项目可拖动排序。侧栏行有 detailed / compact 两种密度。
- 双击侧栏空白处新建会话。把文件夹拖到侧栏上，每个文件夹开一个会话。

#### 新会话

- ⌘T 新会话，⌘⇧T 在 Home 新建，⌘⌥T 在 worktree 里新建（问分支名，放在 `.kero/worktrees/〈分支〉`），⌘O 打开项目。
- 关闭 worktree 会话时问「Remove Worktree」还是「Keep Worktree」：**没有未提交改动时默认删，有未提交改动时默认留。**

#### Changes 面板

- 三个范围：**Last Turn / Uncommitted / Branch**。
- 所有 diff 叠在一个滚动区里，滚到哪个文件，文件列表里就高亮哪个。
- Last Turn 里悬停一处改动出「Revert」和「Keep」。会话里有 agent 在工作时不能 Revert。
- 「Collapse All」折叠全部。diff 布局 unified / split / auto，可设是否换行。

#### 分栏

- ⌘D 右分，⌘⇧D 下分，⌘⇧↩ 放大当前分栏，⌘] / ⌘[ 切换，⌘⌥方向键按方向移动焦点。
- 关闭正在跑程序的分栏先确认（默认开）。

#### 通知

- 三个开关：等你时、做完时、出错时，默认全开。声音默认关，Dock 角标默认开（显示需要你的会话数），Dock 弹跳默认关。
- 只在你没在看的时候发。

#### 设置

- 设置窗口（⌘,）编辑的是一个纯文本配置文件，手改也会自动重载。
- 分页：General、Appearance、Terminal、Shell、Notifications、Agents、Hosts、Git、Browser、Keyboard。
- 主题：跟随系统 / 浅 / 深；可写成 `light:paper,dark:dusk` 这样一对。⌘⇧L 切换深色。

#### 快捷键

| 作用 | 键 |
|---|---|
| 跳到下一个需要你的会话 | ⌘J |
| 命令面板（Go to Session…） | ⌘P |
| 新会话 / 打开项目 | ⌘T / ⌘O |
| 改名 / 关闭会话 | ⌘R / ⌘⇧W |
| 上/下一个会话 | ⌘⇧[ / ⌘⇧] |
| 第 N 个会话 | ⌘1–9 |
| 侧栏 | ⌘B |
| Changes / Files | ⌥⌘G / ⌥⌘E |
| 字号 | ⌘= / ⌘- / ⌘0 |
| 退出但会话继续跑 / 退出并结束全部 | ⌘Q / ⌘⌥Q |

#### 值得抄的细节

- ⌘J 直接跳到下一个在等你的会话。
- 图标本身表达「是哪个 agent」和「是否在跑」，不另加状态点。
- 完成和出错的标记留到你真的去处理为止。
- 关闭对话框的默认按钮随 worktree 是否干净而变。
- 退出应用不结束会话；重启后恢复会话并接上各 agent 的对话。

### 设置页（settings）

来源：本机的 Cursor、Antigravity、Kero 设置页截图（`shots/`），加上 Zed、OpenCode、Claude Code、Codex、Cline、Warp 的文档。Kero 和 Talos 一样用 mygo，自带简体中文，中文措辞直接可参考。

#### 布局

三家长得几乎一样：

- 左栏是分页列表，Cursor 和 Kero 在最上面放一个搜索框（「搜索设置」）。Kero 左栏底部是「打开配置文件」，Antigravity 是当前登录的账号。
- 右边：页标题（不加副标题的居多），下面一组一组。**组名是组上方的小灰字**，组本身是圆角卡片，行之间细线分隔。
- 每行左边是名字加一行灰色说明，右边是控件。说明写「这项做什么」，不重复名字。
- 控件按选项数挑：开关；两三个选项用分段控件（Kero 的「跟随系统 / 浅色 / 深色」「详细 / 紧凑」，Antigravity 的「Queue / Send Immediately」）；更多选项才用弹出菜单。
- 要跳走的东西给一个「打开」按钮（Cursor 的 Cursor Account、Antigravity 的 Tool Permissions）。

#### 分页

| 产品 | 分页 |
|---|---|
| Cursor | General、Appearance、Plan & Usage、Agents、Cloud Agents、Models、Voice、Source Control、Browser & Network、Code Intelligence、Beta、Docs |
| Antigravity | General、Application、Appearance、Shortcuts、Notifications、Models、Browser，下面再按项目分 |
| Kero | 通用、外观、终端、Shell、浏览器、通知、键盘、智能体、主机、高级 |
| Codex | General、Profile、Keyboard shortcuts、Notifications、Appearance、Pets、Browser、Computer Use、Personalization、Archived chats |

共同点：通用、外观、快捷键、通知各占一页；模型单独一页；账号和用量单独一页（Cursor 的 Plan & Usage、Antigravity 的 Models & Usage）。

#### 按任务选模型

- **主模型在输入框里选**，设置里只放默认值。没有一家在设置里写「主 agent」这种行。
- 别的任务的模型是少数人才改的东西，默认都是「跟主模型一样」或「自动」：
  - Cursor：Models 页的「Task Models」组里只有一行「Explore Subagent Model」，说明是「选一个模型，或用按成本和可用性自动挑的默认值」，控件显示「Default」。
  - Zed：设置里叫「Feature-Specific Models」，分 thread summary、commit message、inline assistant、compaction、subagent，各自不设就用当前模型。
  - Claude Code：子 agent 模型、后台任务模型（标题这类小活）、`fallbackModel` 备用链。
  - Codex：`review_model`（/review 用）、`agents.default_subagent_model`。
  - Cline：「Use different models for Plan and Act」一个开关，开了才出两个选择。
- 推理强度：Cursor 单独一行「Coordinator effort」，带名字和说明；Antigravity 在输入框里写成「Gemini 3.8 Flash High」。没有不带标签的单独下拉。
- Cursor 的 Models 页下半是模型列表，每个一个开关，决定哪些出现在输入框的模型菜单里；API Keys 折叠在最下面。

#### 中文措辞（Kero）

- 用「智能体」「会话」「窗格」，不夹英文。
- 选项短：「跟随系统」「浅色」「深色」「当前文件夹」「需要你的窗格」。
- 说明是完整的一句话，口语：「在每个项目名称旁显示其包含的会话数量。」「如果后台服务停止（例如 Mac 重启），会话会在各自的文件夹中重新开始，并保留历史记录。」

#### 语言

- Kero：外观页「语言」，默认「跟随系统」。
- Trae：第一次启动选「简体中文 / English」，之后在设置里改。

#### 对 Talos 的结论

- 「模型分工」「主 agent」「子 agent」「标题与压缩」都换掉。默认模型、推理强度各占一行，带名字；别的任务的模型收成一组，默认「跟默认模型一样」。
- 运行中发消息（排队 / 立即插话）放进通用页，用分段控件（Antigravity 的做法）。
- 加语言设置：跟随系统 / 简体中文 / English。
- 左栏加搜索。
