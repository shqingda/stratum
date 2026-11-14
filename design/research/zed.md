# Zed Agent Panel

来源：zed.dev/docs/ai 的 agent-panel、zed-agent、tools、tool-permissions、parallel-agents、agent-settings、external-agents。思考和工具卡片具体怎么画，文档没写。

## 布局

- Agent Panel 顶栏右上「+」开「New Thread…」菜单；底部是消息编辑器；编辑器上方有一条可折叠的条，列出被改的文件。
- 面板底部有滚动箭头，跳到最新一条或回到开头。左下的准星图标让编辑器跟随 agent 正在看的位置。
- Threads Sidebar 是单独一栏（⌘⌥J）。「Panel Layout › Agentic」把 agent 和线程栏放左边、项目和 Git 放右边；「Classic」是传统编辑器布局。

## 线程栏

- 按项目分组，每组有标题；每行是标题 + 状态标记 + 是哪个 agent。worktree 的线程挂在主项目下面。
- 项目标题上的「+」直接在这个项目里开线程。
- ⌃Tab 线程切换器。
- 归档：悬停点归档图标，或选中按 ⇧⌫。**在跑的线程不能归档**。
- Thread History（⌘G）含已归档的，模糊匹配标题；垃圾桶图标是永久删除，文档明说不可撤销。
- 归档一个线程会连它的 worktree 一起从磁盘删掉；恢复线程时 worktree 也恢复。

## 输入

- Enter 发送。⇧⌥Esc 展开编辑器。
- `@` 可加：文件、目录、符号、以前的线程、技能、诊断信息、分支 diff、URL。
- 粘贴多行代码会自动变成 @ 引用；⌘⇧V 粘贴原文。图片可拖入、粘贴或 @。
- 模型选择 ⌘⌥/；悬停模型点星标收藏，⌥Tab 在收藏间轮换。
- 模型不支持工具调用时显示「No tools」。

## 运行中

- 运行中发的消息**默认排队**，agent 做完再发。
- 排队的消息上有「Steer」开关：打开后在下一步（通常是工具调用和回复之间）送达。
- 排队消息可以改、可以删，单条或全部。
- 打断：点停止，或对排队消息「Send Now」（连按两次 Enter）。
- **任何已发出的消息都能改**：点它的卡片，改 prompt 或上下文，重新提交。

## 对话流

- 流式输出，带「正在用哪个工具」的指示。
- 右键回复：Copy、Copy as Markdown、Copy This Agent Response、Scroll to Top / Bottom、Open Thread as Markdown。
- 压缩显示为一条「Context Compacted」，可展开看摘要。
- 每条回复下有 👍👎；token 用量显示在 profile 选择器旁边。
- 标题自动生成，点标题可改；菜单里有「Regenerate Thread Title」。

## 工具权限

- 全局默认 confirm，可设 allow / deny；每个工具可单独设默认，并有 `always_allow` / `always_deny` / `always_confirm` 三组正则。
- 优先级：内置安全规则 › always_deny › always_confirm › always_allow › 工具默认 › 全局默认。
- 审批卡片的菜单：**Allow once / Deny once**、**Always for 〈工具〉**、能抽出安全模式时还有 **Always for 〈模式〉**。
- 内置规则（只针对终端，不可覆盖）：对 `/`、`~`、`.`、`..` 的 `rm -rf`。
- 串联的命令会拆开逐条检查。设置页带一个「测试你的规则」的小工具。

## 审查改动

- 面板里报出改了哪些文件、多少行。「Review Changes」（⌃⇧R）打开一个多文件合并的 diff 页。
- 每个 hunk 接受或拒绝，或整体接受/拒绝。
- `single_file_review` 打开后，文件自己的标签页里也直接显示 agent 的 diff 和同样的按钮。

## 通知

- 只在发出 prompt 后把 Zed 切到后台时才提醒：桌面通知、声音，或两者。
- 生成期间阻止系统休眠；等确认时不阻止。

## 自动压缩

- 到 90% 自动压缩。上下文窗口小于 8 万 token 时出一条横幅，提供「Start New Thread」。「New From Summary」带着摘要开新线程。

## 快捷键（macOS）

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

## 值得抄的细节

- 排队消息上直接放「Steer」开关，一条消息在排队和插话之间切换。
- 已发出的消息可以点开重新编辑。
- 分栏的分隔线双击回到默认宽度。
- 「在跑的不能归档」「永久删除不可撤销」这类限制写得很直白。
