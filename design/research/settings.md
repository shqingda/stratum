# 设置页

来源：本机的 Cursor、Antigravity、Kero 设置页截图（`shots/`），加上 Zed、OpenCode、Claude Code、Codex、Cline、Warp 的文档。Kero 和 Talos 一样用 mygo，自带简体中文，中文措辞直接可参考。

## 布局

三家长得几乎一样：

- 左栏是分页列表，Cursor 和 Kero 在最上面放一个搜索框（「搜索设置」）。Kero 左栏底部是「打开配置文件」，Antigravity 是当前登录的账号。
- 右边：页标题（不加副标题的居多），下面一组一组。**组名是组上方的小灰字**，组本身是圆角卡片，行之间细线分隔。
- 每行左边是名字加一行灰色说明，右边是控件。说明写「这项做什么」，不重复名字。
- 控件按选项数挑：开关；两三个选项用分段控件（Kero 的「跟随系统 / 浅色 / 深色」「详细 / 紧凑」，Antigravity 的「Queue / Send Immediately」）；更多选项才用弹出菜单。
- 要跳走的东西给一个「打开」按钮（Cursor 的 Cursor Account、Antigravity 的 Tool Permissions）。

## 分页

| 产品 | 分页 |
|---|---|
| Cursor | General、Appearance、Plan & Usage、Agents、Cloud Agents、Models、Voice、Source Control、Browser & Network、Code Intelligence、Beta、Docs |
| Antigravity | General、Application、Appearance、Shortcuts、Notifications、Models、Browser，下面再按项目分 |
| Kero | 通用、外观、终端、Shell、浏览器、通知、键盘、智能体、主机、高级 |
| Codex | General、Profile、Keyboard shortcuts、Notifications、Appearance、Pets、Browser、Computer Use、Personalization、Archived chats |

共同点：通用、外观、快捷键、通知各占一页；模型单独一页；账号和用量单独一页（Cursor 的 Plan & Usage、Antigravity 的 Models & Usage）。

## 按任务选模型

- **主模型在输入框里选**，设置里只放默认值。没有一家在设置里写「主 agent」这种行。
- 别的任务的模型是少数人才改的东西，默认都是「跟主模型一样」或「自动」：
  - Cursor：Models 页的「Task Models」组里只有一行「Explore Subagent Model」，说明是「选一个模型，或用按成本和可用性自动挑的默认值」，控件显示「Default」。
  - Zed：设置里叫「Feature-Specific Models」，分 thread summary、commit message、inline assistant、compaction、subagent，各自不设就用当前模型。
  - Claude Code：子 agent 模型、后台任务模型（标题这类小活）、`fallbackModel` 备用链。
  - Codex：`review_model`（/review 用）、`agents.default_subagent_model`。
  - Cline：「Use different models for Plan and Act」一个开关，开了才出两个选择。
- 推理强度：Cursor 单独一行「Coordinator effort」，带名字和说明；Antigravity 在输入框里写成「Gemini 3.8 Flash High」。没有不带标签的单独下拉。
- Cursor 的 Models 页下半是模型列表，每个一个开关，决定哪些出现在输入框的模型菜单里；API Keys 折叠在最下面。

## 中文措辞（Kero）

- 用「智能体」「会话」「窗格」，不夹英文。
- 选项短：「跟随系统」「浅色」「深色」「当前文件夹」「需要你的窗格」。
- 说明是完整的一句话，口语：「在每个项目名称旁显示其包含的会话数量。」「如果后台服务停止（例如 Mac 重启），会话会在各自的文件夹中重新开始，并保留历史记录。」

## 语言

- Kero：外观页「语言」，默认「跟随系统」。
- Trae：第一次启动选「简体中文 / English」，之后在设置里改。

## 对 Talos 的结论

- 「模型分工」「主 agent」「子 agent」「标题与压缩」都换掉。默认模型、推理强度各占一行，带名字；别的任务的模型收成一组，默认「跟默认模型一样」。
- 运行中发消息（排队 / 立即插话）放进通用页，用分段控件（Antigravity 的做法）。
- 加语言设置：跟随系统 / 简体中文 / English。
- 左栏加搜索。
