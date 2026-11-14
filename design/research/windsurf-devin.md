# Windsurf Cascade 与 Devin

来源：docs.windsurf.com（现在跳到 docs.devin.ai/desktop/*）和 docs.devin.ai。Cascade 现在被称为 Devin Desktop 里的旧版 agent，新对话默认用 Devin Local。两者的 diff 界面、状态标记、通知，文档都没写。

## Cascade

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

## Devin（云端会话）

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

## 值得抄的细节

- 空输入框 Enter = 把排队的立即发出（和 Warp 一致）。
- 审批卡片上能直接改命令再运行。
- 到工具调用上限时停下并给「continue」，而不是静默跑下去或静默结束。
- 把命令、编辑、浏览器操作合成一条可点的时间线。
- 回退不可逆这件事写在界面文案里。
