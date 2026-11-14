# Warp（agent 模式）

来源：docs.warp.dev 的 agents 与 code 部分（terminal-and-agent-modes、prompt-queueing、code-diffs、interactive-code-review、code-review、通知与权限）。Warp 是终端，agent 对话是终端里的一种视图。

## 布局

- 每个标签或分栏有两种模式：终端（默认）和 agent 对话。对话视图背景色不同，输入区多一排工具：模型选择、语音、附图。
- 左边 Conversation Panel（⌘⇧H）；右上铃铛是通知收件箱；右上「Code review」按钮打开右侧审查面板。

## 对话列表与状态

- 两段：**Active**（你发过消息的）和 **Past**（历史，带标题、相对时间、工作目录）。顶部搜索框按标题筛。
- 每个标签上有状态图标：工作中、被卡住、完成、出错；有未读通知时加角标。
- 收件箱筛选：All tabs / Unread / Errors。
- 上下文占用：超过 20% 才显示指示，接近上限变红。用量卡片可展开「Context window used」，分 System Prompt、Tool Definitions、Conversation History、Latest Input、Images、Other。

## 输入

- `@` 加文件、符号等上下文；`/` 命令：`/new` `/plan` `/conversations` `/compact` `/fork` `/fork-and-compact` `/fork from` `/model` `/queue`。
- `!` 开头强制当 shell 命令。⌘I 在 shell 和 agent 之间切。
- 自动识别输入是命令还是 prompt，识别结果用洋红色标「(autodetected)」；手动改过一次后这条输入就不再自动改。
- 右键输入区「Edit agent toolbelt」可以重排、隐藏那排控件。

## 运行中（文档写得最细）

- 默认提交方式是 **Interrupt response**：发新 prompt 会取消正在进行的回复。另一种是 **Queue until response finishes**。两种默认值在设置里选。
- 自动排队开关：输入区上方状态条里的「时钟加号」图标，⌘⇧J。`/queue 〈prompt〉` 单独排一条。
- 排队面板「N queued」在状态条和输入区之间。每行：拖动手柄排序、立即发送（↑ 箭头）、编辑（铅笔；Enter 保存，Esc 取消）、删除（垃圾桶）。
- **输入框为空时按 Enter 发送队首**，数量旁边有「⏎ to send」提示。
- 出错、停止或 ⌃C 时队列暂停，队首那条回到空的输入框里。
- 队列不跨重启保存。
- agent 启动的长时间命令在跑时：默认「等命令结束再发」，可改「立即发送」。
- ⌃C 停止 agent；Esc 退出对话，进行中的对话要在约 2 秒内再按一次确认。

## 权限

- 每个 profile 按权限类型设四档：**Agent Decides / Always ask / Always allow / Never**。
- 类型：应用代码 diff、读文件、建计划、执行命令、与运行中的命令交互、提问。
- 命令 allowlist 和 denylist 是正则；默认 denylist 有 `wget` `curl` `rm` `eval`；denylist 压过 allowlist 和 Always allow。
- 「Run until completion」（⌘⇧I）：这次任务全部自动批准。

## 改动与审查

- 代码 diff 在内置 diff 编辑器里打开，按 hunk 分组。↑↓ 在 hunk 间移动，←→ 换文件。
- Enter 接受；**R「Refine」**用自然语言让它重新生成这份 diff；**E「Edit」**把 diff 变成可直接编辑。接受之前不落盘。
- Code Review 面板：默认看未提交改动，可对 main 或任意分支比较。每个 hunk 的边栏有「Revert diff hunk」；「Discard all」先确认。
- 交互式审查：在改动的行上加行内评论，攒一批一起发，agent 一次处理完返回新 diff。

## 通知

- 三类：Complete、Request（等输入，含审批）、Error。
- 窗口角落的 toast 最多同时两条，自动消失，悬停暂停，点击打开那个会话。
- 应用在后台时发系统通知。子 agent 的事件不进 toast 和收件箱。

## 快捷键（macOS）

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

## 值得抄的细节

- 队列面板每行四个动作齐全（排序、立即发、改、删），空输入框 Enter 发队首。
- 停止后队首回到输入框，不丢也不自动续跑。
- diff 上的「Refine」：不接受也不拒绝，说一句话让它重做这一处。
- 上下文占用低于 20% 时根本不显示指示。
- toast 悬停暂停消失。
