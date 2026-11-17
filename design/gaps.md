# 和主流 agent 的差距

2026-11-18。对照 Claude Code、Codex、versta。依据：Talos 仓库代码（内核工具、协议方法、界面文件），不是逐项真机测试。

## 已有

- 模型：OpenAI、Anthropic、Gemini 三家；订阅登录；多账号，额度用完切账号。
- 工具：读、写、改文件，列目录，搜索，shell（沙箱），git status/diff，计划，提问，子任务（task、run_tasks），review，skill，`web_fetch`，`web_search`。
- 内核：AGENTS.md、技能目录（skills 包，含启用/禁用）、MCP（含信任、添加/编辑/移除、resources、prompts）、Hooks（`PreToolUse` / `PostToolUse` / `Stop`）、worktree 隔离（含 `.worktreeinclude`）、上下文压缩、权限三档和规则、改动记录和撤销（整轮、单块、重做）。
- 界面：会话列表和状态、审批卡、改动卡、Review 面板（变更、终端、计划、行内评论、推送与创建 PR、顶栏 PR/CI 状态）、⌘K、会话内查找、@ 文件/文件夹/会话/MCP 资源、`/` 命令/技能/自定义命令/MCP prompts、用量与费用显示、技能与 MCP 管理界面、设置、通知、菜单栏。

## 缺的，按影响排

### 一、输入框（每天都碰）

1. **/ 命令**：已有内置命令、自定义命令（`.talos/commands`、`.claude/commands`、`~/.talos/commands`）、当前文件夹技能、`/worktree` 切换以及 MCP prompts（`/server:name` 与 `/mcp__server__name`）。
2. **@ 扩展**：已有（会话、文件夹、文件、MCP 资源；气泡只显示用户写的字）。输入框仍是纯文本，没有芯片。
3. **编辑并重发 / 回退到某一轮**：已有，照 Claude Code 的 `/rewind`。支持恢复代码和对话、只恢复对话、只恢复代码、从这里起做摘要（`summarize`）。还没有「摘要到这里」。
4. **语音输入**：没有。

### 二、技能和扩展

5. **技能管理界面**：已有（设置 › 技能与 MCP）：安装（本地路径或 Git URL）、新建、启用/禁用开关、展开查看 `SKILL.md` 正文、移除。
6. **自定义命令**：已有。
7. **Hooks**：已有内核支持（`.talos/hooks.json`、`.claude/settings.json`、`~/.talos/hooks.json` 的 `PreToolUse`、`PostToolUse`、`Stop`）。缺界面编辑。
8. **MCP 添加**：已有添加、编辑、移除、项目服务器启用/停用。缺查看服务器运行日志。
9. **插件**：没有。

### 三、工具

10. **网页抓取和搜索**：`web_fetch` 和 `web_search` 均已有（`kernel/tools/web.go`，`perm.Net` 权限，Full 以外都先问；`web_search` 支持 `allowed_domains` / `blocked_domains`）。
11. **浏览器 / 预览**：没有。
12. **LSP / 诊断**：没有（改完不知道有没有编译错误，只能靠跑命令）。
13. **记忆**：没有跨会话的记忆文件。

### 四、会话和工作流

14. **worktree 隔离**：已有（新会话在 git 仓库里可切到 Worktree，创建独立分支和目录，拷贝 `.worktreeinclude`，删除会话自动清理）。缺已开始会话的 Hand off（Local 与 Worktree 互搬）。
15. **分叉会话**：没有。
16. **后台 / 定时任务**：没有（设计稿有「定时任务」入口）。
17. **开 PR**：已有提交、推送（`workspace.push`）、创建 PR（`workspace.pr`）以及顶栏 PR/CI 状态（`PR #N` 与检查通过/失败/运行中图标）。
18. **导出 / 分享会话**：没有。
19. **新会话选分支**：没有（`worktree` 会固定从 `HEAD` 拉新分支）。

### 五、可见性

20. **用量和费用**：已有上下文占比与悬停累计费用、轮次结束费用（基于模型目录单价）。仍缺订阅账号额度和重置时间的数据源。
21. **模型/推理强度每个角色单独选**：已有各角色单独选模型和推理强度（`ModelRef.Effort` 在内核和 `Sub`/`Light` 已支持，设置页仍共用主强度）。

### 六、外观（设计稿差异，见 audit.md）

22. 用户消息样式、设置分页合并、Review「文件」页签。

## 建议顺序

1. ~~`/ 命令 + 用户调用技能 + 自定义命令 + MCP prompts`~~（已有）
2. ~~`@ 扩展`~~（已有）
3. ~~`编辑重发 / 回退到某一轮 + 从这里起做摘要`~~（已有）
4. ~~`web_fetch / web_search`~~（已有）
5. ~~`用量和费用显示`~~（已有单轮/累计费用；订阅额度缺数据源）
6. ~~`worktree 隔离`~~（已有）
7. ~~`Hooks、MCP 添加界面、技能安装界面`~~（已有）
8. ~~`开 PR、看 CI`~~（已有）
9. 接下来可做：LSP/诊断、分叉会话、插件/浏览器预览、跨会话记忆、定时任务。
