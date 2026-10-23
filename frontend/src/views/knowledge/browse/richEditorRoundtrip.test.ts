// Markdown 双向转换回归测试：RichEditor 依赖 tiptap-markdown 做
// Markdown → ProseMirror → Markdown 的往返。这里用 jsdom 起真实编辑器实例，
// 盯住我们实际用到的语法（标题/列表/任务列表/引用/代码块/表格/链接/图片/
// 分割线/加粗斜体删除线），防止升级 tiptap 或 tiptap-markdown 后静默丢格式。
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

before(async () => {
  const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>');
  (globalThis as any).window = dom.window;
  (globalThis as any).document = dom.window.document;
  (globalThis as any).Node = dom.window.Node;
  (globalThis as any).HTMLElement = dom.window.HTMLElement;
  (globalThis as any).DOMParser = dom.window.DOMParser;

  ({ Editor } = await import('@tiptap/core'));
  ({ StarterKit } = await import('@tiptap/starter-kit'));
  ({ TaskList, TaskItem } = await import('@tiptap/extension-list'));
  ({ Image } = await import('@tiptap/extension-image'));
  ({ Table, TableRow, TableCell, TableHeader } = await import('@tiptap/extension-table'));
  ({ Markdown } = await import('tiptap-markdown'));
});

const roundTrip = (input: string): string => {
  const editor = new Editor({
    element: (globalThis as any).document.createElement('div'),
    extensions: [
      StarterKit.configure({ heading: { levels: [1, 2, 3, 4] }, link: { openOnClick: false } }),
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
