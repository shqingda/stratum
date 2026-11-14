# Claude Code 桌面版（Claude 桌面应用的 Code 页）

来源：code.claude.com/docs/en/desktop、desktop-quickstart、interactive-mode、permission-modes、checkpointing。desktop 页很长，只读到前 2/3。本机装了这个应用，可以直接看真实界面补全。

## 窗口

- 每个会话有标题栏：标题（点击改名）、标题旁的小箭头（会话菜单）、图标按钮 Terminal / Changes / Browser / Files（悬停显示名字）、⋮ 溢出菜单。
- 中间是对话流；旁边是「会话详情栏」（session rail），几张卡片：在哪运行、Changes、后台任务、Context。窗口够宽才出现，可隐藏。
- 底部输入框；输入框下面一行：环境、项目文件夹、权限模式、模型、用量环。
- 面板种类：对话、diff、浏览器、终端、文件、计划、任务、子 agent。拖面板标题可移动，拖边缘改大小，⌘\ 关掉聚焦的面板。面板可以最小化到角落，也可以弹成独立窗口再停靠回来。
- 窗口窄时放不下的按钮收进 ⋮。没有专门按钮的面板（后台任务、计划）有内容时才出现在 ⋮ 里。

## 侧栏

- 「+ New」新会话（⌘N）。悬停分组标题出「Group and sort」。
- 归档：悬停会话 › 菜单 › Archive。worktree 里有未提交改动时会提示，「Archive anyway」丢弃。Archived 页里可 Unarchive。可设置 PR 合并或关闭后自动归档。
- 并排看：⌥点击会话，或右键 › Open in › Split view；⌘点击在新窗口打开。

## 新会话

- 输入框下面一行从左到右：环境（Local / Cloud / SSH）、项目文件夹（菜单里有 Open folder…）、权限模式、模型。
- ⌘⌥1–9 选环境；⌘⌥0 打开「New environment」对话框。
- Worktree 开关：这个会话在自己的 worktree 里跑，默认在 `<项目>/.claude/worktrees/`；`.worktreeinclude` 拷贝被忽略的文件。
- 建议任务卡片：Start / Do it here / Options / Start all。

## 输入框

- Enter 发送。可拖入图片、PDF 和其他文件。
- 左边「+」：Add files or photos、Add folder、Slash commands、Add connectors、Add plugins。
- `@文件名` 加文件；`/` 出命令和技能（`/compact` `/context` `/code-review` `/rewind` `/btw`）。
- **提示建议**：回复之后，空输入框里出现灰色建议文字；Tab 或 → 接受；没接受就按 Enter 不会把它发出去；一打字或加附件它就消失。
- 模型 ⌘⇧I，权限模式 ⌘⇧M，推理强度 ⌘⇧E；点用量环看套餐用量。打开的菜单里按 1–9 直接选第 N 项。

## 运行中

- Enter 发一条纠正，不停止当前运行；Claude 在当前动作做完后读到它，下一步之前调整。
- 停止按钮立即打断；Esc 停止回复，已做的工作保留。
- CLI 的说法（桌面版文档没确认）：运行中发的消息显示为灰色排队项；在第一行按 ↑ 可以把它取回来编辑；Ctrl+Enter 立即发送排队消息。

## 对话流

- 三档显示（会话菜单 › Transcript view，或 ⌃O 轮换）：
  - Normal：工具调用折叠成摘要，回复全文显示。
  - Thinking：再加上思考。出现过思考之后菜单里才有这一档。
  - Verbose：每次工具调用、读文件、中间步骤全显示。
- 对话流里会出现的卡片：代码审查结果（按文件分组）、预览服务器卡片（带 Open）、建议任务、来自其他会话的消息。

## 权限模式与审批

- 模式：Manual（改文件、跑命令前都问，能看到 diff 再接受或拒绝）、Accept edits（自动接受编辑和 mkdir/touch/mv）、Plan（只探索不改）、Auto（后台分类器把关）、Bypass permissions。
- 浏览器站点：Allow once / Always allow / Deny。电脑操作按应用：Allow for this session / Deny，并显示权限档（只看 / 只点 / 完全控制）。
- 计划审批的选项（CLI 文案）：批准并切到自动模式 / 批准但编辑逐个确认 / 继续规划。
- 即使在 Auto 和 Bypass 下，危险路径的 `rm` 也会被拒。

## Diff 与审查

- 入口：Changes 卡片上的 `+12 -1`，或标题栏的 Changes（⌘⇧D）。按文件逐个看。
- 评论：悬停某行点行号旁的「+」，输入，Enter 加上；⌘Enter 把所有评论一起提交。Claude 改完后给出新的 diff。
- 右键任意文件路径（对话里、diff 里、文件面板里都行）：Attach as context、Open in（VS Code / Cursor / Zed）、Show in Finder、Copy › 绝对路径 / 相对路径 / 文件名。
- `/code-review` 出一张审查卡片：Walk through in diff、Fix this one、Apply fixes。
- PR：出现在 Changes 卡片里，带 CI 状态；⋮ 里有 Auto-fix、Merge when ready。
- 检查点（CLI）：`/rewind` 或空输入框连按两次 Esc 打开菜单：恢复代码和对话 / 只恢复对话 / 只恢复代码 / 从这里起做摘要。shell 命令造成的改动恢复不了。

## 终端、浏览器、文件

- 终端：⌃`，在会话文件夹里打开，面板标题栏的「+」加标签。右键对话里的文件夹 › Open in terminal。
- 浏览器：⌘⇧B，带标签；有 Dev servers 菜单；⌘⇧S 选页面元素发给 Claude。dev server 配在 `.claude/launch.json`。
- 文件面板：可直接编辑，自动保存；和磁盘冲突时给 Override / Discard。

## 并行与其他

- 旁支对话：⌘; 或 `/btw`，不存盘。
- 会话结束时如果你没在看它，发系统通知。
- Remote Control：每个会话一个开关，可在别的设备上接着操作。

## 快捷键（macOS）

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

## 值得抄的细节

- `+12 -1` 这个数字本身就是进入审查的入口。
- 提示建议只在空输入框里出现，灰色，不会被误发。
- 思考没出现过时，「Thinking」这一档就不出现在菜单里。
- 文件路径在任何地方右键都是同一个菜单。
- 没内容的面板不占按钮位置，有内容了才出现。
- Context 卡片标题上直接显示上下文占用百分比。
