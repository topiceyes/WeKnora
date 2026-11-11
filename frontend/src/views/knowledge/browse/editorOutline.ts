import { Extension, type Editor } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import type { Node as PMNode } from '@tiptap/pm/model'
import type { OutlineItem, OutlineSectionMove } from './types'
import { moveTopLevelBlock } from './editorBlocks'

export function editorHeadings(doc: PMNode): (OutlineItem & { pos: number })[] {
  const items: (OutlineItem & { pos: number })[] = []
  doc.descendants((node, pos) => {
    if (node.type.name === 'heading') items.push({ text: node.textContent, level: node.attrs.level, pos })
  })
  return items
}

/** A section ends at the next outer heading of equal or lower level. */
export function editorSections(doc: PMNode) {
  const sections: { from: number; to: number; level: number; parent: number; up?: number; down?: number }[] = []
  const stack: number[] = [], peers = new Map<string, number>()
  doc.forEach((node, pos) => {
    if (node.type.name !== 'heading') return
    const level = Number(node.attrs.level)
    while (stack.length && sections[stack.at(-1)!]!.level >= level) sections[stack.pop()!]!.to = pos
    const parent = stack.length ? sections[stack.at(-1)!]!.from : -1
    const index = sections.length, key = `${parent}:${level}`, previous = peers.get(key)
    const section = { from: pos, to: doc.content.size, level, parent, up: previous, down: undefined as number | undefined }
    if (previous !== undefined) sections[previous]!.down = index
    sections.push(section); peers.set(key, index); stack.push(index)
  })
  return sections
}

let outlineToken = 0
function outlineSnapshot(doc: PMNode, blocked: () => boolean) {
  const headings = editorHeadings(doc), sections = editorSections(doc)
  const byPosition = new Map(sections.map(section => [section.from, section]))
  return { token: ++outlineToken, headings, sections, byPosition, blocked }
}
export const editorOutlineKey = new PluginKey<ReturnType<typeof outlineSnapshot>>('document-editor-outline')

/** Selection changes reuse the heading list; only document changes rescan it. */
export function createEditorOutline(change: (items: OutlineItem[]) => void, active: (index: number) => void, blocked: () => boolean = () => false) {
  return Extension.create({
    name: 'documentEditorOutline',
    addProseMirrorPlugins() {
      return [new Plugin({
        key: editorOutlineKey,
        state: { init: (_, state) => outlineSnapshot(state.doc, blocked), apply: (tr, previous) => tr.docChanged ? outlineSnapshot(tr.doc, blocked) : previous },
        view(view) {
          let snapshot: ReturnType<typeof outlineSnapshot> | null = null, previous = -2, wasAllowed: boolean | null = null
          const update = () => {
            const current = editorOutlineKey.getState(view.state)!, allowed = view.editable && !blocked()
            if (snapshot !== current || allowed !== wasAllowed) {
              snapshot = current; wasAllowed = allowed
              change(current.headings.map(({ text, level, pos }) => {
                const section = current.byPosition.get(pos)
                return { text, level, ...(allowed && section ? { section: { token: current.token, up: section.up !== undefined, down: section.down !== undefined } } : {}) }
              }))
            }
            let index = -1
            for (let i = 0; i < current.headings.length; i++) {
              if (current.headings[i]!.pos > view.state.selection.from) break
              index = i
            }
            if (index !== previous) { previous = index; active(index) }
          }
          update()
          return { update, destroy() { change([]); active(-1) } }
        },
      })]
    },
  })
}

/** Reject an old outline event before resolving positions in the current document. */
export function moveEditorSection(editor: Editor | undefined, request: OutlineSectionMove, limit: () => void = () => {}) {
  if (!editor || editor.isDestroyed || !editor.isEditable || editor.view.composing ||
    !Number.isInteger(request.index) || request.index < 0 || !['up', 'down'].includes(request.direction)) return false
  const snapshot = editorOutlineKey.getState(editor.state)
  if (!snapshot || snapshot.blocked() || snapshot.token !== request.token) return false
  const heading = snapshot.headings[request.index], section = heading && snapshot.byPosition.get(heading.pos)
  if (!section) return false
  const peerIndex = section[request.direction]
  if (peerIndex === undefined) return false
  const peer = snapshot.sections[peerIndex]!, destination = request.direction === 'up' ? peer.from : peer.to
  const position = destination > section.from ? destination - (section.to - section.from) : destination
  if (!moveTopLevelBlock(editor.view, section, destination, limit)) return false
  editor.commands.setTextSelection(position + 1)
  return true
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
