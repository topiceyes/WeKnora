// Markdown 双向转换回归测试：RichEditor 依赖 tiptap-markdown 做
// Markdown → ProseMirror → Markdown 的往返。这里用 jsdom 起真实编辑器实例，
// 盯住我们实际用到的语法（标题/列表/任务列表/引用/代码块/表格/链接/图片/
// 分割线/加粗斜体删除线/公式/mermaid），防止升级 tiptap 或 tiptap-markdown
// 后静默丢格式。
import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';

let Editor: any;
let StarterKit: any;
let TaskList: any;
let TaskItem: any;
let Image: any;
let Table: any;
let TableRow: any;
let TableCell: any;
let TableHeader: any;
let Markdown: any;
let MermaidCodeBlock: any;
let buildBlockMathExtension: any;
let buildInlineMathExtension: any;

before(async () => {
  // url 必须给：opaque origin 下 jsdom 的 localStorage 会抛 SecurityError
  // （MermaidCodeBlock → @/i18n 模块顶层要读 localStorage）。
  const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost/' });
  (globalThis as any).window = dom.window;
  (globalThis as any).document = dom.window.document;
  (globalThis as any).Node = dom.window.Node;
  (globalThis as any).HTMLElement = dom.window.HTMLElement;
  (globalThis as any).DOMParser = dom.window.DOMParser;
  // MermaidCodeBlock → @/i18n 在模块顶层读 localStorage（记忆语言偏好）。
  (globalThis as any).localStorage = dom.window.localStorage;

  ({ Editor } = await import('@tiptap/core'));
  ({ StarterKit } = await import('@tiptap/starter-kit'));
  ({ TaskList, TaskItem } = await import('@tiptap/extension-list'));
  ({ Image } = await import('@tiptap/extension-image'));
  ({ Table, TableRow, TableCell, TableHeader } = await import('@tiptap/extension-table'));
  ({ Markdown } = await import('tiptap-markdown'));
  ({ MermaidCodeBlock } = await import('./MermaidCodeBlock'));
  ({ buildBlockMathExtension, buildInlineMathExtension } = await import('./editorMathExtensions'));
});

const noopMathOpener = () => {};

const roundTrip = (input: string): string => {
  const editor = new Editor({
    element: (globalThis as any).document.createElement('div'),
    extensions: [
      StarterKit.configure({
        heading: { levels: [1, 2, 3, 4] },
        link: { openOnClick: false },
        codeBlock: false,
      }),
      MermaidCodeBlock,
      buildBlockMathExtension(noopMathOpener),
      buildInlineMathExtension(noopMathOpener),
      TaskList,
      TaskItem.configure({ nested: true }),
      Image,
      Table.configure({ resizable: false }),
      TableRow,
      TableCell,
      TableHeader,
      Markdown.configure({ html: false, transformPastedText: true, breaks: false }),
    ],
    content: input,
  });
  try {
    return editor.storage.markdown.getMarkdown();
  } finally {
    editor.destroy();
  }
};

test('heading and inline marks survive the round-trip', () => {
  const out = roundTrip('# 标题\n\n正文 **加粗** *斜体* ~~删除线~~ `code`。\n');
  assert.match(out, /^# 标题/m);
  assert.match(out, /\*\*加粗\*\*/);
  assert.match(out, /\*斜体\*/);
  assert.match(out, /~~删除线~~/);
  assert.match(out, /`code`/);
});

test('bullet, ordered and nested lists survive', () => {
  const out = roundTrip('- 甲\n- 乙\n  - 子项\n\n1. 第一\n2. 第二\n');
  assert.match(out, /- 甲/);
  assert.match(out, / {2}- 子项/);
  assert.match(out, /1\. 第一/);
  assert.match(out, /2\. 第二/);
});

test('task list items keep their checked state (not escaped as text)', () => {
  const out = roundTrip('- [ ] 待办\n- [x] 已完成\n');
  assert.match(out, /- \[ \] 待办/);
  assert.match(out, /- \[x\] 已完成/);
  assert.doesNotMatch(out, /\\\[ /);
});

test('code block keeps language and content', () => {
  const out = roundTrip('```go\nfmt.Println("hi")\n```\n');
  assert.match(out, /```go\n/);
  assert.match(out, /fmt\.Println\("hi"\)/);
  assert.match(out, /\n```/);
});

test('table keeps header and cell text', () => {
  const out = roundTrip('| 名称 | 数量 |\n| --- | --- |\n| 苹果 | 3 |\n');
  assert.match(out, /\| 名称 \| 数量 \|/);
  assert.match(out, /\| 苹果 \| 3 \|/);
});

test('link and image keep href/src including resource:// handles', () => {
  const out = roundTrip('[文档](https://example.com)\n\n![图](resource://abc123)\n');
  assert.match(out, /\[文档\]\(https:\/\/example\.com\)/);
  assert.match(out, /!\[图\]\(resource:\/\/abc123\)/);
});

test('blockquote and horizontal rule survive', () => {
  const out = roundTrip('> 引用一句\n\n---\n');
  assert.match(out, /> 引用一句/);
  assert.match(out, /\n---(\n|$)/);
});

test('empty content stays empty', () => {
  assert.equal(roundTrip('').trim(), '');
});

// ---------- P4：公式 / mermaid ----------

test('block math $$…$$ round-trips byte-stable', () => {
  const out = roundTrip('前文\n\n$$\nE=mc^2\n$$\n\n后文\n');
  assert.match(out, /\$\$\nE=mc\^2\n\$\$/);
  assert.match(out, /前文/);
  assert.match(out, /后文/);
});

test('inline math $…$ survives inside a paragraph', () => {
  const out = roundTrip('质能方程 $E=mc^2$ 不变\n');
  assert.match(out, /\$E=mc\^2\$/);
});

test('\\(…\\) and \\[…\\] normalize to $…$ / $$…$$', () => {
  const out = roundTrip('面积 \\(\\pi r^2\\) 恒定\n\n\\[\n\\sum_{i=1}^n i\n\\]\n');
  assert.match(out, /\$\\pi r\^2\$/);
  assert.match(out, /\$\$\n\\sum_\{i=1\}\^n i\n\$\$/);
});

test('escaped dollar stays literal text, not math', () => {
  const out = roundTrip('价格 \\\$5 不变\n');
  // 不产生数学节点；$ 保留为字面文本（转义形式可能被规范成 \$ 或 $5 前的普通 $）。
  assert.doesNotMatch(out, /data-latex/);
  assert.match(out, /5/);
});

test('currency dollars are not parsed as math', () => {
  const out = roundTrip('价格 $5 和 $6 之间\n');
  // 没生成公式节点 → 原文按普通文本原样回环。
  assert.match(out, /价格 \$5 和 \$6 之间/);
});

test('mermaid fence survives byte-stable (codeBlock node view path)', () => {
  const out = roundTrip('```mermaid\nflowchart TD\n  A --> B\n```\n');
  assert.match(out, /```mermaid\nflowchart TD\n  A --> B\n```/);
});

// ---------- 打字路径（input rule）----------
// insertContentAt(pos, text, { applyInputRules: true }) 会经 inputRulesPlugin
// 的模拟输入 meta 走真实 input rule 分支——jsdom 无法派发原生 textInput，
// 这是唯一能覆盖打字转换的路径。回归 tiptap nodeInputRule 曾产出的
// 「$ + 节点 + $$」脏文档（序列化成 $$$E=mc^2$$$）。
const typedRoundTrip = async (typed: string): Promise<string> => {
  const editor = new Editor({
    element: (globalThis as any).document.createElement('div'),
    extensions: [
      StarterKit.configure({
        heading: { levels: [1, 2, 3, 4] },
        link: { openOnClick: false },
        codeBlock: false,
      }),
      MermaidCodeBlock,
      buildBlockMathExtension(noopMathOpener),
      buildInlineMathExtension(noopMathOpener),
      TaskList,
      TaskItem.configure({ nested: true }),
      Image,
      Table.configure({ resizable: false }),
      TableRow,
      TableCell,
      TableHeader,
      Markdown.configure({ html: false, transformPastedText: true, breaks: false }),
    ],
    content: '<p>正文</p>',
  });
  try {
    // 模拟用户在段末继续打字（input rule 在最后一个 $ 输入时触发）
    editor.commands.setTextSelection(editor.state.doc.content.size - 1);
    editor.commands.insertContentAt(
      editor.state.selection.from,
      typed,
      { applyInputRules: true } as any,
    );
    // applyInputRules 走 setTimeout(0) 异步派发，等一拍再取结果。
    await new Promise((resolve) => setTimeout(resolve, 10));
    return editor.storage.markdown.getMarkdown();
  } finally {
    editor.destroy();
  }
};

test('typing $x^2$ converts to inline math without stray dollars', async () => {
  const out = await typedRoundTrip(' 质能方程 $E=mc^2$');
  assert.match(out, /\$E=mc\^2\$/);
  assert.doesNotMatch(out, /\$\$\$/);
  assert.doesNotMatch(out, /质能方程 \$\{/);
});

test('typing $$x$$ converts to block math cleanly', async () => {
  const out = await typedRoundTrip('公式 $$x^2$$');
  assert.match(out, /\$\$\n?x\^2\n?\$\$/);
  assert.doesNotMatch(out, /\$\$\$/);
});

test('typing currency $5 和 $6 does not create math', async () => {
  const out = await typedRoundTrip(' 价格 $5 和 $6');
  assert.match(out, /价格 \$5 和 \$6/);
  assert.doesNotMatch(out, /data-latex/);
});
