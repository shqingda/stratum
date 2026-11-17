# 交接（2026-11-17）

## 当前

- 分支 `main`，已推送到 `origin/main`。
- 最新提交 `b784c29`：`@` 可以引用会话和 MCP 资源；`/rewind` 可以恢复代码、对话，或两者一起。
- 上一提交 `05aeb39`：`/` 命令、自定义命令、`/compact`、`@` 文件夹和最近文件、`web_fetch`。
- 工作区未跟踪：`design/fidelity/out/`、`screenshot.png`。不要动 `screenshot.png`。
- 本文件按用户要求单独提交。平时 `tasks/` 仍不提交，除非用户明确要求。

## 用户的规则（照做）

- 回复用中文，按 STE：结论先说，一句一事，状态词固定（已做 / 没做 / 通过 / 失败 / 没验证），去 AI 味。
- 功能完全参考 Codex、Claude Code CLI、Claude 桌面版、versta、opencode、devin。不要自己发明交互。拿不准就去看这些产品，在回复里说参考了谁。
- 额度：周额度和 5 小时额度都要留 5%–10%。快到线就写交接，不要硬做。开工先用 `get_usage` 看一眼。
- 测试账号：不准用用户的 Claude 账号测。其他服务也不要用用户账号。只用假实现和演示环境（`scripts/demo.sh`，`TALOS_DEMO`）。
- 提交：只有用户说「提交」或「推送」才提交，直接推 `main`。`git add` 写具体路径。`tasks/`、`design/fidelity/out/`、`screenshot.png` 不提交，除非用户这次明确要求提交其中某一个。提交信息末尾加 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。
- 用户在对话框里用 `!scripts/demo.sh` 跑，会话结束就退出。让用户在自己终端里跑。

## 已在 main 里的行为

### `05aeb39`

- `/` 菜单（`app/slash.go`）：草稿是「/词」时弹出。↑↓ 选，Enter/Tab 执行，Esc 收起。左对齐，无图标，命令名宽 `slashName=140`。
- 内置：`/new` `/changes` `/find` `/search` `/settings` `/skills` `/model` `/effort` `/ask` `/auto` `/full`。运行中多 `/stop`。有可撤销轮次时多 `/undo`。会话有内容且没在跑时多 `/compact`。
- 自定义命令：`.talos/commands`、`.claude/commands`、`~/.talos/commands` 下的 `*.md`。`$ARGUMENTS` 在发送时替换。
- `/compact`：`Prompt.Compact`。有历史就压缩，压缩完不跑 agent。手动压缩的提示和自动压缩不同。
- `@` 文件和文件夹：空查询先列本会话改过的文件，最近一轮在前，最多 8 条。文件夹以 `/` 结尾。
- `web_fetch`：只允许 http/https。权限 `perm.Net`，只有 Full 不问。界面标签「读取网页」。

### `b784c29`

- `@` 菜单分组：会话、文件夹、文件、MCP 资源。参考 Claude 桌面版的前三组，Claude Code 的 `@server:uri`。
- 选会话插入 `@session:<id>`。发送时 `kernel/session/mention.go` 的 `expandMentions` 在原文后附上标题和对话纯文本，保留最近 4000 字。当前会话和找不到的 id 不展开。
- 选资源插入 `@server:uri`。发送时读取该资源。服务器必须是当前文件夹已信任的 MCP。读失败时附上错误，这一轮继续。
- 气泡只显示用户写的字。附件留在发给模型的记录里。输入框是纯文本，没有芯片。
- `/rewind`：参考 Claude Code 的检查点（code.claude.com/docs/en/checkpointing）。协议 `session.rewind`，实现在 `kernel/session/rewind.go`，删日志用 `store.Truncate`。
- 打开方式：`/rewind`，或空输入框在 400 毫秒内连按两次 Esc。列出已结束的用户消息，旧的在前。运行中的不列出。一轮里后补的消息没有自己的检查点。
- 三个动作：恢复代码和对话、只恢复对话、只恢复代码。没有未撤销的文件改动时，只留「只恢复对话」。
- 恢复对话会从那条 `turn.start` 起删掉日志，并把原文放进草稿。客户端再读一遍日志。
- 恢复代码从最近一轮往回。文件和记录对不上时停住，对话不删。
- 用户气泡悬停有「回退到这里」。Esc 从动作退回列表。从气泡打开时，Esc 直接关掉。
- 终端命令的效果不在改动记录里，菜单上写明回不去。
- 没做 Claude Code 的「从这里起做摘要」和「摘要到这里」。`/compact` 仍是整段摘要。「算了」用 Esc，没有单独一行。

## 测试

- 推送 `b784c29` 之前，`go vet ./...` 通过，`go test ./...` 通过。
- 相关测试：`TestMentionSession`、`TestMentionResource`、`TestMentionGroups`、`TestExpandSessionMention`、`TestUserMentionHidesAttachment`、`TestHub` 的资源读取、`TestRewindConversation`、`TestRewindCodeAndBoth`、`TestSlashRewind`、`TestRewindDoubleEsc`、`TestRewindActionsWithoutCode`。
- 真机没看。`/` 菜单、裸 `@`、`/compact`、`/model`、`/effort`、`web_fetch` 审批卡、`/rewind`、两次 Esc 都没在窗口里看过。

## 下一步

1. 让用户在自己终端跑 `scripts/demo.sh`。要看的：
   - 输入 `/`：列表左对齐、无图标。
   - 只输入 `@`：马上出分组列表。输入过滤。↑↓ Enter 插入。
   - `/rewind` 和空输入框两次 Esc。
   - `/compact`、`/model`、`/effort`、`web_fetch` 审批卡。
   - hello 页上列表会把输入框顶高，不要点到权限菜单。
2. `design/gaps.md` 的下一项是 `web_search`。要接搜索服务，不能用用户账号测。
3. 其后仍按 `design/gaps.md`：用量和费用、worktree、Hooks、技能和 MCP 安装界面、开 PR 和看 CI、MCP prompts。每项先查参考产品的做法。
4. 做完一项就更新 `design/gaps.md` 和本文件。
