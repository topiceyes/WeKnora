import { Extension, type Editor } from '@tiptap/core'
import { Plugin } from '@tiptap/pm/state'
import type { Node as PMNode } from '@tiptap/pm/model'
import type { OutlineItem } from './types'

export function editorHeadings(doc: PMNode): (OutlineItem & { pos: number })[] {
  const items: (OutlineItem & { pos: number })[] = []
  doc.descendants((node, pos) => {
    if (node.type.name === 'heading') items.push({ text: node.textContent, level: node.attrs.level, pos })
  })
  return items
}

/** Selection changes reuse the heading list; only document changes rescan it. */
export function createEditorOutline(change: (items: OutlineItem[]) => void, active: (index: number) => void) {
  return Extension.create({
    name: 'documentEditorOutline',
    addProseMirrorPlugins() {
      return [new Plugin({ view(view) {
        let doc: PMNode | null = null, items: ReturnType<typeof editorHeadings> = [], previous = -2
        const update = () => {
          if (view.state.doc !== doc) {
            doc = view.state.doc; items = editorHeadings(doc)
            change(items.map(({ text, level }) => ({ text, level })))
          }
          let index = -1
          for (let i = 0; i < items.length; i++) {
            if (items[i]!.pos > view.state.selection.from) break
            index = i
          }
          if (index !== previous) { previous = index; active(index) }
        }
        update()
        return { update, destroy() { change([]); active(-1) } }
      } })]
    },
  })
}

/** Resolve from the current document so inserts, duplicate titles and undo cannot leave stale anchors. */
export function jumpToEditorHeading(editor: Editor | undefined, index: number) {
  if (!editor || editor.isDestroyed || !editor.isEditable || !Number.isInteger(index) || index < 0) return
  const item = editorHeadings(editor.state.doc)[index]
  if (!item) return
  editor.commands.setTextSelection(item.pos + 1)
  editor.view.focus()
  editor.view.dispatch(editor.state.tr.scrollIntoView())
}
