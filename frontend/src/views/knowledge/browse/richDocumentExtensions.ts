import { Mark, Node, mergeAttributes } from '@tiptap/core'
import { Table } from '@tiptap/extension-table'
import { TaskItem } from '@tiptap/extension-list'
import { documentMarkdown } from './structuredDocument'
import { clipboardColor } from './editorClipboard'

// The task item's node view updates its checkbox label and attributes whenever
// ProseMirror refreshes it. Those generated DOM mutations must not be parsed
// back as user input, which otherwise causes an endless observer/update loop.
export const RichTaskItem = TaskItem.extend({
  addNodeView() {
    const createView = this.parent!()
    return props => {
      const view = createView!(props)
      return { ...view, ignoreMutation(mutation) {
        if (mutation.type === 'selection') return false
        return mutation.target === view.dom || !view.contentDOM?.contains(mutation.target)
      } }
    }
  },
})

export const Highlight = Mark.create({
  name: 'highlight',
  addAttributes: () => ({ color: { default: '#fff3a3', parseHTML: element => clipboardColor(element.style.backgroundColor) || '#fff3a3' } }),
  parseHTML: () => [{ tag: 'mark' }, { style: 'background-color', getAttrs: value => { const color = clipboardColor(String(value)); return color ? { color } : false } }],
  renderHTML: ({ mark }) => ['mark', { style: `background-color:${/^#[\da-f]{6}$/i.test(mark.attrs.color) ? mark.attrs.color : '#fff3a3'};color:inherit` }, 0],
  addStorage: () => ({ markdown: { serialize: { open: '', close: '' }, parse: {} } }),
})
export const TextColor = Mark.create({
  name: 'textColor',
  addAttributes: () => ({ color: { default: '#d4380d', parseHTML: element => clipboardColor(element.dataset.textColor || element.style.color || element.getAttribute('color') || '') } }),
  parseHTML: () => [
    { tag: 'span[data-text-color]', getAttrs: element => clipboardColor((element as HTMLElement).dataset.textColor || '') ? {} : false },
    { style: 'color', getAttrs: value => { const color = clipboardColor(String(value)); return color ? { color } : false } },
    { tag: 'font[color]', getAttrs: element => clipboardColor((element as HTMLElement).getAttribute('color') || '') ? {} : false },
  ],
  renderHTML: ({ mark }) => ['span', { 'data-text-color': mark.attrs.color, style: `color:${/^#[\da-f]{6}$/i.test(mark.attrs.color) ? mark.attrs.color : '#d4380d'}` }, 0],
  addStorage: () => ({ markdown: { serialize: { open: '', close: '' }, parse: {} } }),
})
export const Callout = Node.create({
  name: 'callout', group: 'block', content: 'block+', defining: true,
  addAttributes: () => ({ kind: { default: 'info', parseHTML: element => element.getAttribute('data-callout') || 'info' } }),
  parseHTML: () => [{ tag: 'div[data-callout]' }],
  renderHTML: ({ node, HTMLAttributes }) => ['div', mergeAttributes(HTMLAttributes, {
    'data-callout': node.attrs.kind, class: `rich-document-callout rich-document-callout--${node.attrs.kind}`,
  }), 0],
  addStorage: () => ({ markdown: { serialize(state: any, node: any) {
    state.write(documentMarkdown({ schema_version: 1, content: { type: 'doc', content: [node.toJSON()] } }))
    state.closeBlock(node)
  }, parse: {} } }),
  addKeyboardShortcuts() { return { 'Mod-Shift-x': () => this.editor.commands.lift('callout') } },
})

export const RichTable = Table.extend({
  addStorage() {
    return { markdown: { serialize(state: any, node: any) {
      state.write(documentMarkdown({ schema_version: 1, content: { type: 'doc', content: [node.toJSON()] } }))
      state.closeBlock(node)
    }, parse: {} } }
  },
})
