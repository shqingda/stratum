# Codex 桌面应用（OpenAI）

来源：官方文档（developers.openai.com/codex/app/* 现在跳到 learn.chatgpt.com/docs/*）。只有文字，没看到截图；视觉样式、对话流里每种条目怎么画，文档没写。

## 窗口

- 同一个 ChatGPT 桌面应用里有 Chat、Work、Codex 三种模式，⌃1/2/3 切换。
- 左：侧栏（⌘B 开合），放会话、项目、Scheduled（定时任务）、Activity（铃铛）。
- 中：会话标题栏 + 对话流 + 底部输入框。worktree 会话的标题栏有「Hand off」「Create branch here」「Open」。
- 下：集成终端（⌃` 开合；⌘J 开合整个底部面板）。
- 右：标签式侧面板——Review（⌃⇧G）、文件树（⌘⇧E）、内置浏览器（⌘T）。⌘P 搜文件。
- 布局三档：全屏对话 / 分栏 / 隐藏标签，⌘⇧B 轮换；⌘⌥B 在对话与标签之间切。
- 会话可以弹成独立小窗，可「Always on top」。

## 侧栏

- 「Chats」旁有筛选图标（含按时间排序）；「Add new project」加项目；悬停项目名出三点菜单可移除。
- 状态：Activity 里列出未读、运行中、等你回复的会话。浮窗宠物用四种状态：Running、Needs input、Ready、Blocked。
- 未读：⌘⇧U 标未读，⇧Esc 清掉全部未读标记，⌘⌥A 跳到下一个需要处理的会话。
- 置顶 ⌘⌥P，归档 ⌘⇧A（归档后在设置里可恢复，回到原来的位置），重命名 ⌘⌥R。
- ⌘1–9 跳到第 N 个会话；⌘⌥1–6 打开最近的会话。
- ⌘F 会话内查找，⌘G / ⌘⇧G 上下一个。搜索全部会话默认没快捷键，能匹配内容和分支名。

## 新会话

- ⌘N 新会话；⌘⌥O 新建不属于任何项目的会话。
- 输入框下方选运行位置：Local / Worktree / Cloud；选 Worktree 时再选基准分支和本地环境（环境带 setup 脚本）。
- 也能用斜杠命令：`/local` `/worktree` `/cloud` `/project` `/model` `/reasoning`。
- 选错了位置：取消，输入框为空时按 ↑ 取回刚才的 prompt。
- 设置里有「Suggested prompts」：打开应用时给出结合上下文的建议。

## 输入框

- 里面的控件：运行位置、分支、本地环境、模型与推理强度、权限、语音、发送/停止。
- 权限控件在输入框下方，有「Approve for me」（自动审查）。
- `/` 出命令列表，边打边筛：`/review` `/plan` `/goal` `/side` `/fork` `/compact` `/status` `/mcp` `/rename` `/usage` `/pin` `/archive`。启用的技能也在列表里。`$` 显式调用技能。
- 空输入框按 ↑ 取回上一条 prompt。
- 模型选择 ⌃⇧M，最近用过的模型 ⌃⌘M。

## 运行中

- 设置 › General › 「Follow-up behavior」：运行中发的消息是「引导当前这一轮」还是「等下一轮」，用户自己选默认。
- 同页还能选 Enter 是直接发送，还是多行时/总是要 ⌘Enter。
- Goal 模式：输入框上方有一条进度行，带暂停/继续、改目标、清除。运行中仍可继续发消息。
- 「Prevent sleep while running」防止运行时休眠。
- 文档没写排队消息怎么显示、怎么改删，也没给停止的快捷键。

## 审批

- 审批请求打开时：⏎ 批准，Esc 拒绝。
- `/approve`：对最近一次被自动审查拒掉的操作放行一次重试。
- 浏览器权限按站点分「Browse / Download / Upload」，各有「Requires approval / Always allow / Block」。
- 权限请求和提问的通知可以分别开关。

## Review 面板

- 需要项目在 Git 仓库里；不是仓库就提示创建。
- 范围：Unstaged（默认）、Staged、Commit（选某个提交）、Branch（对基准分支）、Last turn（上一轮的改动）。
- 顶部「Stage all」「Revert all」；每个文件、每个 hunk 都能 stage / unstage / revert。
- 点文件名在外部编辑器打开；点文件名那一行的空白处折叠/展开这个文件的 diff；⌘点某一行跳到编辑器的那一行。
- 行内评论：悬停某行点「+」，写意见，提交；然后在对话里发一句，Codex 把评论当修改意见处理。
- `/review` 的结果以行内评论出现；可设成在单独的会话里跑（Detached）。
- 提交、推分支、开 PR 都在应用内完成。

## 其他面板

- 终端：聚焦时 ⌃L 或 ⌘K 清屏。项目动作（本地环境里定义的脚本）在这里跑，顶栏有快捷按钮，第一个动作是 ⌘⇧D。
- `/plan` 进计划模式；没有单独的计划面板。
- 浏览器：⌘T 开标签，⌘⇧. 在浏览与评论模式间切。
- 旁支对话：`/side` 或 ⌘⌥S，问一句不打断主会话。

## Worktree 与定时任务

- 托管的 worktree 放在 `$CODEX_HOME/worktrees`，默认 detached HEAD。「Hand off」把会话连同代码在 Local 和 Worktree 之间搬。
- 仓库根的 `.worktreeinclude` 列出要拷进 worktree 的被忽略文件（如 `.env`）。
- 默认只保留最近 15 个托管 worktree；置顶或在跑的不删。归档会话会删它的 worktree，删前存快照，重新打开时可恢复。
- Scheduled：侧栏的一个收件箱。独立任务每次开新会话；会话内的任务回到那个会话。应用要一直开着。

## 通知

- 一轮完成的提醒：从不 / 只在应用在后台时 / 总是。权限请求和提问各有开关。

## 设置分页

General、Profile、Keyboard Shortcuts（可按名字搜，也可直接按键搜）、Notifications、Appearance（主题、强调色、界面字体和代码字体）、Pets、Browser、Computer Use、Personalization（AGENTS.md、记忆）、Archived chats。

## 快捷键（macOS）

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

## 值得抄的细节

- 「取回上一条 prompt」用空输入框的 ↑。
- 会话卡住时的排查提示：先看是不是有审批在等。
- 快捷键设置可以直接按组合键来搜。
- 归档会话删 worktree 前存快照，重开可恢复。
- 一轮完成的通知分「从不 / 后台时 / 总是」三档。
