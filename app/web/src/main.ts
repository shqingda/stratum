import { File, FileDiff } from "@pierre/diffs";
import { FileTree, type GitStatusEntry } from "@pierre/trees";
import { marked } from "marked";

declare const mygo: {
  call(method: string, ...args: unknown[]): Promise<unknown>;
  on<T>(event: string, fn: (payload: T) => void): () => void;
};

interface StringsPayload {
  preview: string;
  raw: string;
  openExternal: string;
  toggleTree: string;
  selectFile: string;
  findPlaceholder: string;
  findPrev: string;
  findNext: string;
  close: string;
  comment: string;
  revert: string;
  addRemark: string;
  remarkPlaceholder: string;
  cancel: string;
  add: string;
}

interface ThemePayload {
  dark: boolean;
  bg: string;
  sheet: string;
  card: string;
  fg: string;
  fg2: string;
  mute: string;
  edge: string;
  edge2: string;
  sel: string;
  hover: string;
  chip: string;
  live: string;
  plus: string;
  minus: string;
  primaryContainer: string;
  onPrimary: string;
  onPrimaryContainer: string;
  onSecondaryContainer: string;
  strings?: StringsPayload;
}

interface ModePayload {
  mode: "diffs" | "files";
  diffStyle: "unified" | "split";
  treeHidden: boolean;
}

interface DiffFilePayload {
  key: string;
  hash: string;
  turnSeq: number;
  path: string;
  relPath: string;
  status: string;
  added: number;
  removed: number;
  before?: string;
  after?: string;
  reverted: boolean;
}

interface DiffsPayload {
  diffStyle: "unified" | "split";
  files: DiffFilePayload[];
}

interface TreePayload {
  cwd: string;
  paths: string[];
  gitStatus: GitStatusEntry[];
  hidden: boolean;
}

interface FilePayload {
  path: string;
  content: string;
  isMarkdown: boolean;
  raw: boolean;
  tabs: string[];
  err?: string;
}

const diffsView = document.getElementById("diffs-view")!;
const filesView = document.getElementById("files-view")!;
const filesTabs = document.getElementById("files-tabs")!;
const filesEmpty = document.getElementById("files-empty")!;
const filesCode = document.getElementById("files-code")!;
const filesMd = document.getElementById("files-md")!;
const mdToggle = document.getElementById("md-toggle")!;
const mdPreviewBtn = document.getElementById("md-preview-btn")!;
const mdRawBtn = document.getElementById("md-raw-btn")!;
const openExtBtn = document.getElementById("open-ext-btn")!;
const treeToggleBtn = document.getElementById("tree-toggle-btn")!;
const treePane = document.getElementById("tree-pane")!;
const treeHost = document.getElementById("tree-host")!;

const findBar = document.getElementById("find-bar")!;
const findInput = document.getElementById("find-input") as HTMLInputElement;
const findPrev = document.getElementById("find-prev")!;
const findNext = document.getElementById("find-next")!;
const findClose = document.getElementById("find-close")!;

const remarkModal = document.getElementById("remark-modal") as HTMLDialogElement;
const remarkTitle = document.getElementById("remark-title")!;
const remarkText = document.getElementById("remark-text") as HTMLTextAreaElement;
const remarkCancel = document.getElementById("remark-cancel")!;
const remarkSubmit = document.getElementById("remark-submit")!;

let uiStrings: StringsPayload = {
  preview: "Preview",
  raw: "Raw",
  openExternal: "Open externally",
  toggleTree: "Toggle Workspace Tree",
  selectFile: "Select a file to preview",
  findPlaceholder: "Find in view...",
  findPrev: "Previous",
  findNext: "Next",
  close: "Close",
  comment: "Comment",
  revert: "Revert",
  addRemark: "Add line comment",
  remarkPlaceholder: "Comment for the agent...",
  cancel: "Cancel",
  add: "Add",
};

let isDark = false;
let currentMode: "diffs" | "files" = "diffs";
let currentDiffStyle: "unified" | "split" = "unified";
let currentTreeCwd = "";
let currentFilePath = "";
let currentMdRaw = false;
let remarkTarget: { path: string; line: number; side: string } | null = null;

interface CachedDiffEntry {
  hash: string;
  style: "unified" | "split";
  card: HTMLElement;
  instance: FileDiff;
}

const diffCache = new Map<string, CachedDiffEntry>();
let fileInstance: File | null = null;
let treeInstance: FileTree | null = null;

// Safe markdown renderer: escape raw HTML blocks/tags.
const mdRenderer = new marked.Renderer();
mdRenderer.html = ({ text }: { text: string }) => {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
};
marked.setOptions({ renderer: mdRenderer, gfm: true, breaks: false });

function pierreThemeObj() {
  return { dark: "pierre-dark", light: "pierre-light" } as const;
}

function applyTheme(t: ThemePayload) {
  isDark = t.dark;
  const r = document.documentElement.style;
  r.setProperty("--bg", t.bg);
  r.setProperty("--sheet", t.sheet);
  r.setProperty("--card", t.card);
  r.setProperty("--fg", t.fg);
  r.setProperty("--fg2", t.fg2);
  r.setProperty("--mute", t.mute);
  r.setProperty("--edge", t.edge);
  r.setProperty("--edge2", t.edge2);
  r.setProperty("--sel", t.sel);
  r.setProperty("--hover", t.hover);
  r.setProperty("--chip", t.chip);
  r.setProperty("--live", t.live);
  r.setProperty("--plus", t.plus);
  r.setProperty("--minus", t.minus);
  r.setProperty("--primary-container", t.primaryContainer);
  r.setProperty("--on-primary", t.onPrimary);
  r.setProperty("--on-primary-container", t.onPrimaryContainer);
  r.setProperty("--on-secondary-container", t.onSecondaryContainer);
  document.documentElement.dataset.theme = isDark ? "dark" : "light";

  if (t.strings) {
    uiStrings = t.strings;
    mdPreviewBtn.textContent = uiStrings.preview;
    mdRawBtn.textContent = uiStrings.raw;
    openExtBtn.title = uiStrings.openExternal;
    openExtBtn.setAttribute("aria-label", uiStrings.openExternal);
    treeToggleBtn.title = uiStrings.toggleTree;
    treeToggleBtn.setAttribute("aria-label", uiStrings.toggleTree);
    if (!currentFilePath) {
      filesEmpty.textContent = uiStrings.selectFile;
    }
    findInput.placeholder = uiStrings.findPlaceholder;
    findPrev.title = uiStrings.findPrev;
    findPrev.setAttribute("aria-label", uiStrings.findPrev);
    findNext.title = uiStrings.findNext;
    findNext.setAttribute("aria-label", uiStrings.findNext);
    findClose.title = uiStrings.close;
    findClose.setAttribute("aria-label", uiStrings.close);
    remarkText.placeholder = uiStrings.remarkPlaceholder;
    remarkCancel.textContent = uiStrings.cancel;
    remarkSubmit.textContent = uiStrings.add;
    for (const entry of diffCache.values()) {
      const cbtn = entry.card.querySelector<HTMLButtonElement>(".diff-comment-btn");
      if (cbtn) cbtn.textContent = uiStrings.comment;
      const rbtn = entry.card.querySelector<HTMLButtonElement>(".diff-revert-btn");
      if (rbtn) rbtn.textContent = uiStrings.revert;
    }
  }

  const mode = isDark ? "dark" : "light";
  for (const entry of diffCache.values()) {
    entry.instance.setThemeType(mode);
  }
  if (fileInstance) {
    fileInstance.setThemeType(mode);
  }
}

function applyMode(m: ModePayload) {
  currentMode = m.mode;
  currentDiffStyle = m.diffStyle === "split" ? "split" : "unified";
  diffsView.classList.toggle("hidden", currentMode !== "diffs");
  filesView.classList.toggle("hidden", currentMode !== "files");
  treePane.classList.toggle("collapsed", m.treeHidden);
}

function renderDiffs(payload: DiffsPayload) {
  try {
    currentDiffStyle = payload.diffStyle === "split" ? "split" : "unified";
    const files = payload.files ?? [];
    const nextKeys = new Set<string>();
    const frag = document.createDocumentFragment();

    for (const f of files) {
    nextKeys.add(f.key);
    const cached = diffCache.get(f.key);
    if (cached && cached.hash === f.hash && cached.style === currentDiffStyle) {
      frag.appendChild(cached.card);
      continue;
    }
    if (cached) {
      cached.instance.cleanUp();
      diffCache.delete(f.key);
    }

    const card = document.createElement("div");
    card.className = "diff-card";

    const head = document.createElement("div");
    head.className = "diff-head";

    const pathEl = document.createElement("span");
    pathEl.className = "diff-path";
    pathEl.textContent = f.relPath || f.path;
    pathEl.title = f.path;
    head.appendChild(pathEl);

    const stats = document.createElement("span");
    stats.className = "diff-stats";
    stats.innerHTML = `<span class="stat-plus">+${f.added}</span><span class="stat-minus">−${f.removed}</span>`;
    head.appendChild(stats);

    const commentBtn = document.createElement("button");
    commentBtn.type = "button";
    commentBtn.className = "diff-btn diff-comment-btn";
    commentBtn.textContent = uiStrings.comment;
    commentBtn.onclick = () => openRemarkModal(f.relPath || f.path, 1, "new");
    head.appendChild(commentBtn);

    if (!f.reverted && f.turnSeq > 0) {
      const revertBtn = document.createElement("button");
      revertBtn.type = "button";
      revertBtn.className = "diff-btn diff-revert-btn";
      revertBtn.textContent = uiStrings.revert;
      revertBtn.onclick = () => {
        void mygo.call("Pierre.RevertFile", f.turnSeq, f.path);
      };
      head.appendChild(revertBtn);
    }

    const body = document.createElement("div");
    body.className = "diff-body";

    card.appendChild(head);
    card.appendChild(body);

    const inst = new FileDiff({
      theme: pierreThemeObj(),
      themeType: isDark ? "dark" : "light",
      diffStyle: currentDiffStyle,
      disableFileHeader: true,
      overflow: "scroll",
      preferredHighlighter: "highlights",
    });
    inst.render({
      oldFile: { name: f.relPath || f.path, contents: f.before ?? "" },
      newFile: { name: f.relPath || f.path, contents: f.after ?? "" },
      containerWrapper: body,
    });

    diffCache.set(f.key, {
      hash: f.hash,
      style: currentDiffStyle,
      card,
      instance: inst,
    });
    frag.appendChild(card);
  }

    for (const [key, entry] of diffCache.entries()) {
      if (!nextKeys.has(key)) {
        entry.instance.cleanUp();
        diffCache.delete(key);
      }
    }

    diffsView.replaceChildren(frag);
  } catch (err) {
    diffsView.textContent = `Diff render error: ${String(err)}`;
  }
}

function renderTree(payload: TreePayload) {
  treePane.classList.toggle("collapsed", payload.hidden);
  const paths = payload.paths ?? [];
  const gitStatus = payload.gitStatus ?? [];
  try {
    if (!treeInstance || currentTreeCwd !== payload.cwd) {
      if (treeInstance) {
        treeInstance.cleanUp();
        treeInstance = null;
      }
      currentTreeCwd = payload.cwd;
      treeHost.replaceChildren();
      treeInstance = new FileTree({
        paths,
        gitStatus,
        initialExpansion: "open",
        search: true,
        flattenEmptyDirectories: true,
        onSelectionChange: (selected) => {
          if (selected.length > 0) {
            const path = selected[0];
            if (!path.endsWith("/")) {
              void mygo.call("Pierre.SelectFile", path);
            }
          }
        },
      });
      treeInstance.render({ containerWrapper: treeHost });
      return;
    }
    treeInstance.resetPaths(paths);
    treeInstance.setGitStatus(gitStatus);
  } catch (err) {
    treeHost.textContent = `Tree error: ${String(err)}`;
  }
}

function renderTabs(tabs: string[], active: string) {
  filesTabs.replaceChildren();
  for (const tabPath of tabs) {
    const tab = document.createElement("div");
    tab.className = "file-tab" + (tabPath === active ? " active" : "");
    tab.title = tabPath;

    const name = tabPath.split("/").pop() || tabPath;
    const label = document.createElement("span");
    label.textContent = name;
    tab.appendChild(label);

    const close = document.createElement("button");
    close.type = "button";
    close.className = "tab-close";
    close.textContent = "✕";
    close.onclick = (ev) => {
      ev.stopPropagation();
      void mygo.call("Pierre.CloseTab", tabPath);
    };
    tab.appendChild(close);

    tab.onclick = () => {
      void mygo.call("Pierre.SelectFile", tabPath);
    };
    filesTabs.appendChild(tab);
  }
}

function renderFile(payload: FilePayload) {
  currentFilePath = payload.path;
  currentMdRaw = payload.raw;
  renderTabs(payload.tabs ?? [], payload.path);

  if (!payload.path) {
    mdToggle.classList.add("hidden");
    openExtBtn.classList.add("hidden");
    filesCode.classList.add("hidden");
    filesMd.classList.add("hidden");
    filesEmpty.classList.remove("hidden");
    return;
  }

  filesEmpty.classList.add("hidden");
  openExtBtn.classList.remove("hidden");

  mdToggle.classList.toggle("hidden", !payload.isMarkdown);
  mdPreviewBtn.classList.toggle("active", !payload.raw);
  mdRawBtn.classList.toggle("active", payload.raw);

  if (payload.err) {
    filesCode.classList.add("hidden");
    filesMd.classList.add("hidden");
    filesEmpty.classList.remove("hidden");
    filesEmpty.textContent = payload.err;
    return;
  }

  if (payload.isMarkdown && !payload.raw) {
    filesCode.classList.add("hidden");
    filesMd.classList.remove("hidden");
    filesMd.innerHTML = marked.parse(payload.content) as string;
    return;
  }

  filesMd.classList.add("hidden");
  filesCode.classList.remove("hidden");
  if (!fileInstance) {
    fileInstance = new File({
      theme: pierreThemeObj(),
      themeType: isDark ? "dark" : "light",
      disableFileHeader: true,
      overflow: "scroll",
      preferredHighlighter: "highlights",
    });
  } else {
    fileInstance.setThemeType(isDark ? "dark" : "light");
  }
  fileInstance.render({
    file: { name: payload.path, contents: payload.content },
    containerWrapper: filesCode,
  });
}

function openRemarkModal(path: string, line: number, side: string) {
  remarkTarget = { path, line, side };
  remarkTitle.textContent = `${uiStrings.addRemark} · ${path}:${line}`;
  remarkText.value = "";
  if (!remarkModal.open) {
    remarkModal.showModal();
  }
  remarkText.focus();
}

function closeRemarkModal() {
  remarkTarget = null;
  if (remarkModal.open) {
    remarkModal.close();
  }
}

remarkModal.addEventListener("click", (e) => {
  if (e.target === remarkModal) {
    closeRemarkModal();
  }
});

function openFindBar() {
  findBar.classList.remove("hidden");
  findInput.focus();
  findInput.select();
}

function closeFindBar() {
  findBar.classList.add("hidden");
  window.getSelection()?.removeAllRanges();
}

function runFind(backwards: boolean) {
  const q = findInput.value;
  if (!q) return;
  (window as unknown as { find(s: string, cs?: boolean, back?: boolean, wrap?: boolean): boolean }).find(
    q,
    false,
    backwards,
    true,
  );
}

mdPreviewBtn.onclick = () => {
  if (currentMdRaw) void mygo.call("Pierre.ToggleMarkdownRaw", false);
};
mdRawBtn.onclick = () => {
  if (!currentMdRaw) void mygo.call("Pierre.ToggleMarkdownRaw", true);
};
openExtBtn.onclick = () => {
  if (currentFilePath) void mygo.call("Pierre.OpenExternal", currentFilePath);
};
treeToggleBtn.onclick = () => {
  void mygo.call("Pierre.ToggleTree");
};

findPrev.onclick = () => runFind(true);
findNext.onclick = () => runFind(false);
findClose.onclick = closeFindBar;
findInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    runFind(e.shiftKey);
  } else if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    closeFindBar();
  }
});

remarkCancel.onclick = closeRemarkModal;
remarkSubmit.onclick = () => {
  const text = remarkText.value.trim();
  if (remarkTarget && text) {
    void mygo.call("Pierre.AddRemark", remarkTarget.path, remarkTarget.line, remarkTarget.side, text);
  }
  closeRemarkModal();
};
remarkText.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    closeRemarkModal();
  } else if (e.key === "Enter" && e.metaKey) {
    e.preventDefault();
    remarkSubmit.click();
  }
});

window.addEventListener("keydown", (e) => {
  if (e.metaKey && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "f") {
    e.preventDefault();
    openFindBar();
    return;
  }
  if (e.key === "Escape") {
    if (remarkModal.open) {
      e.preventDefault();
      closeRemarkModal();
      return;
    }
    if (!findBar.classList.contains("hidden")) {
      e.preventDefault();
      closeFindBar();
      return;
    }
    e.preventDefault();
    void mygo.call("Pierre.EscapeToComposer");
  }
});

mygo.on<ThemePayload>("pierre:theme", applyTheme);
mygo.on<ModePayload>("pierre:mode", applyMode);
mygo.on<DiffsPayload>("pierre:diffs", renderDiffs);
mygo.on<TreePayload>("pierre:tree", renderTree);
mygo.on<FilePayload>("pierre:file", renderFile);

void mygo.call("Pierre.Ready");
