# 交接（2026-11-18）

## 当前

- 分支 `main`：最新已推送提交 `cb1a794`（前序重要提交 `f76fdfb`）。
- 工作区未提交：`tasks/handoff.md`（按规则不提交）、`design/fidelity/out/`、`screenshot.png`（未动）。

## 用户的规则（照做）

- 回复用中文，按 STE：结论先说，一句一事，状态词固定（已做 / 没做 / 通过 / 失败 / 没验证），去 AI 味。
- 功能完全参考 Codex、Claude Code CLI、Claude 桌面版、versta、opencode、devin。不要自己发明交互。拿不准就去看这些产品，在回复里说参考了谁。
- 额度：周额度和 5 小时额度都要留 5%–10%。快到线就写交接，不要硬做。
- 测试账号：不准用用户的 Claude 账号测。其他服务也不要用用户账号。只用假实现和演示环境（`scripts/demo.sh`，`TALOS_DEMO`）。
- 提交：只有用户说「提交」或「推送」才提交，直接推 `main`。`git add` 写具体路径。`tasks/`、`design/fidelity/out/`、`screenshot.png` 不提交，除非用户这次明确要求提交其中某一个。提交信息末尾加 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。
- 用户在对话框里用 `!scripts/demo.sh` 跑，会话结束就退出。让用户在自己终端里跑。

## 已完成并推送

1. **`f76fdfb`（核心路线图能力）**：
   - 持久化 LSP 语言服务器接入与工具（[kernel/lsp/lsp.go](file:///Users/shqingda/Projects/talos/kernel/lsp/lsp.go)、[kernel/tools/lsp.go](file:///Users/shqingda/Projects/talos/kernel/tools/lsp.go)）。
   - 内置浏览器 / 本地预览（[kernel/preview/preview.go](file:///Users/shqingda/Projects/talos/kernel/preview/preview.go)、[kernel/tools/preview.go](file:///Users/shqingda/Projects/talos/kernel/tools/preview.go)、[app/preview.go](file:///Users/shqingda/Projects/talos/app/preview.go)）。
   - 后台 / 定时任务 Automations（[kernel/session/automations.go](file:///Users/shqingda/Projects/talos/kernel/session/automations.go)、[app/automations.go](file:///Users/shqingda/Projects/talos/app/automations.go)）。
   - 已开始会话 Local ↔ Worktree Handoff（[kernel/session/worktree.go](file:///Users/shqingda/Projects/talos/kernel/session/worktree.go)、[app/slash.go](file:///Users/shqingda/Projects/talos/app/slash.go)）。
   - Hooks 可视化管理与插件包系统（[kernel/session/hooks.go](file:///Users/shqingda/Projects/talos/kernel/session/hooks.go)、[kernel/session/plugins.go](file:///Users/shqingda/Projects/talos/kernel/session/plugins.go)、[app/mcp.go](file:///Users/shqingda/Projects/talos/app/mcp.go)）。
2. **`cb1a794`（OpenCode 对齐与细节补齐）**：
   - OpenCode 规范 Agents / Subagents 加载与 `task`/`run_tasks` 子代理路由（[kernel/agents/agents.go](file:///Users/shqingda/Projects/talos/kernel/agents/agents.go)、[kernel/session/session.go](file:///Users/shqingda/Projects/talos/kernel/session/session.go)、[kernel/session/runner.go](file:///Users/shqingda/Projects/talos/kernel/session/runner.go)）。
   - 全局与项目多目录 Skills 发现（`~/.agents/skills`、`~/.claude/skills`、`.agents/skills`、`.claude/skills`、`.talos/skills`）（[kernel/skills/skills.go](file:///Users/shqingda/Projects/talos/kernel/skills/skills.go)）。
   - 折叠侧边栏标题与按钮组重叠修复（[app/transcript.go](file:///Users/shqingda/Projects/talos/app/transcript.go)）。
   - 订阅账号额度与重置时间极速缓存、响应头无感更新与后台刷新（[llm/auth/quota.go](file:///Users/shqingda/Projects/talos/llm/auth/quota.go)、[llm/auth/providers.go](file:///Users/shqingda/Projects/talos/llm/auth/providers.go)、[kernel/boot/boot.go](file:///Users/shqingda/Projects/talos/kernel/boot/boot.go)、[app/settings.go](file:///Users/shqingda/Projects/talos/app/settings.go)、[app/sidebar.go](file:///Users/shqingda/Projects/talos/app/sidebar.go)）。
   - 文件写入后自动 Formatter 引擎（[kernel/session/formatters.go](file:///Users/shqingda/Projects/talos/kernel/session/formatters.go)）。
   - 输入框 `@` 芯片化与用户消息左侧引用块样式（[app/mention.go](file:///Users/shqingda/Projects/talos/app/mention.go)、[app/composer.go](file:///Users/shqingda/Projects/talos/app/composer.go)、[app/transcript.go](file:///Users/shqingda/Projects/talos/app/transcript.go)）。
   - 定时任务桌面通知与插件包深度加载 skills / MCP（[app/desktop.go](file:///Users/shqingda/Projects/talos/app/desktop.go)、[kernel/session/plugins.go](file:///Users/shqingda/Projects/talos/kernel/session/plugins.go)、[kernel/mcp/mcp.go](file:///Users/shqingda/Projects/talos/kernel/mcp/mcp.go)）。

## 没做的（下一会话优先接手）

1. **真机与无沙箱验证（用户已明确留到最后）**：
   - 在无沙箱环境运行全量 `go test ./...`（覆盖需 `127.0.0.1` 回环、`os.OpenRoot` 或嵌套 `sandbox-exec` 的集成测试）。
   - 在真机运行 `scripts/demo.sh` 验收界面：侧栏折叠标题间距、`@` 芯片、引用块气泡、预览面板、定时任务及桌面通知、Hooks/Plugins 页面、账号额度角标。
2. **OpenCode Agents 与 Configure 遗留缺口**：
   - **主代理切换与 Tab 快捷键**：[kernel/agents/agents.go](file:///Users/shqingda/Projects/talos/kernel/agents/agents.go) 已定义 `build`/`plan` 主代理，但主会话与输入框 UI 尚未接入切换，当前仅子代理 `task`/`run_tasks` 生效。
   - **输入框 `@agent` 点名调用**：[app/mention.go](file:///Users/shqingda/Projects/talos/app/mention.go) 目前仅补全会话、文件夹、文件、MCP 资源，不支持 `@agent` 补全与显式路由。
   - **Agent 参数与权限未完全打通**：[kernel/session/session.go](file:///Users/shqingda/Projects/talos/kernel/session/session.go) 尚未将 `custom.Model` 映射到已登录账号来覆盖子代理模型；`Temperature`/`TopP` 未透传到 [kernel/agent/agent.go](file:///Users/shqingda/Projects/talos/kernel/agent/agent.go) 和 `llm.Request`；`Permission` 尚无按 bash 命令前缀通配匹配；缺 Agents 配置 UI。
   - **OpenCode Configure 其他缺口**：缺自定义 `Themes`（当前仅 3 档亮暗主题）、缺 `Formatters` 设置界面（仅有后端引擎）、缺 `Policies` 与 `Warming` 独立配置界面、插件包暂未合并包内 `commands`/`agents` 目录。
   - **不做的 / 原因**：语音输入（用户已明确跳过）；OpenCode 原生 JS/TS 插件脚本钩子运行时（Talos 为 Go 原生内核，不内嵌 JS 运行时）。
3. **小缺陷与打磨**：
   - Hooks UI（[app/mcp.go](file:///Users/shqingda/Projects/talos/app/mcp.go)）目前支持新增和删除，缺行内编辑已有 Hook。
   - Apple Design 细节巡检（其余列表右键菜单、空状态引导统一、长列表滚动性能与内存占用）尚未系统排查。
   - [design/gaps.md](file:///Users/shqingda/Projects/talos/design/gaps.md) 仍为旧进度，待同步更新。
