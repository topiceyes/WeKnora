import { Extension } from '@tiptap/core'
import { Plugin, PluginKey, type EditorState } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'

export interface TextMatch { from: number; to: number }

/** Literal, non-overlapping matches, including text split across formatting marks. */
export function findTextMatches(doc: ProseMirrorNode, query: string): TextMatch[] {
  if (!query) return []
  const matches: TextMatch[] = []
  doc.descendants((node, pos) => {
    if (!node.isTextblock) return true
    const text = node.textBetween(0, node.content.size, '', '\ufffc')
    let offset = 0
    while (offset <= text.length - query.length) {
      const index = text.indexOf(query, offset)
      if (index < 0) break
      matches.push({ from: pos + 1 + index, to: pos + 1 + index + query.length })
      offset = index + query.length
    }
    return false
  })
  return matches
}

/** Commands only start at the beginning of a plain paragraph, never in code or a URL. */
export function slashQuery(state: EditorState): { from: number; to: number; query: string } | null {
  const { $from, empty } = state.selection
  if (!empty || $from.parent.type.name !== 'paragraph') return null
  const text = $from.parent.textBetween(0, $from.parentOffset, '', '\ufffc')
  const match = /^\/([^/\n]{0,32})$/.exec(text)
  return match ? { from: $from.start(), to: $from.pos, query: match[1]!.trim() } : null
}

export function normalizeTableHeaders(state: EditorState) {
  const tr = state.tr
  state.doc.descendants((node, pos) => {
    if (node.type.name !== 'table') return true
    node.forEach((row, rowOffset, index) => row.forEach((cell, cellOffset) => {
      const type = index === 0 ? state.schema.nodes.tableHeader! : state.schema.nodes.tableCell!
      if (cell.type !== type) tr.setNodeMarkup(pos + 2 + rowOffset + cellOffset, type, cell.attrs)
    }))
    return false
  })
  return tr
}

interface ToolkitState {
  query: string
  active: number
  matches: TextMatch[]
  uploads: Map<string, number>
}
type ToolkitMeta = { query?: string; active?: number; addUpload?: { id: string; pos: number }; removeUpload?: string }
export const editorToolkitKey = new PluginKey<ToolkitState>('document-editor-toolkit')

export function createEditorToolkit(options: {
  placeholder: () => string
  uploading: () => string
  onFiles: (files: File[], pos: number) => void
  onKeyDown: (event: KeyboardEvent) => boolean
}) {
  return Extension.create({
    name: 'documentEditorToolkit',
    priority: 1000,
    addProseMirrorPlugins() {
      return [new Plugin<ToolkitState>({
        key: editorToolkitKey,
        state: {
          init: () => ({ query: '', active: 0, matches: [], uploads: new Map() }),
          apply(tr, previous) {
            const meta = tr.getMeta(editorToolkitKey) as ToolkitMeta | undefined
            const uploads = new Map<string, number>()
            previous.uploads.forEach((pos, id) => {
              const mapped = tr.mapping.mapResult(pos, 1)
              if (!mapped.deleted) uploads.set(id, mapped.pos)
            })
            if (meta?.addUpload) uploads.set(meta.addUpload.id, meta.addUpload.pos)
            if (meta?.removeUpload) uploads.delete(meta.removeUpload)
            const query = meta?.query ?? previous.query
            const matches = tr.docChanged || query !== previous.query ? findTextMatches(tr.doc, query) : previous.matches
            const active = Math.max(0, Math.min(meta?.active ?? previous.active, matches.length - 1))
            return { query, matches, active, uploads }
          },
        },
        props: {
          handleKeyDown(view, event) {
            if (!view.editable || view.composing || event.isComposing) return false
            return options.onKeyDown(event)
          },
          handlePaste(view, event) {
            if (!view.editable) return false
            const files = Array.from(event.clipboardData?.files ?? []).filter(file => file.type.startsWith('image/'))
            if (!files.length) return false
            // Office/web clipboards may contain both a preview image and the
            // actual HTML document. Preserve the document instead of replacing
            // the entire paste with its preview.
            const html = event.clipboardData?.getData('text/html') || ''
            if (html && new DOMParser().parseFromString(html, 'text/html').body.textContent?.trim()) return false
            event.preventDefault()
            options.onFiles(files, view.state.selection.from)
            return true
          },
          handleDrop(view, event, _slice, moved) {
            if (moved || !view.editable) return false
            const files = Array.from(event.dataTransfer?.files ?? []).filter(file => file.type.startsWith('image/'))
            if (!files.length) return false
            event.preventDefault()
            options.onFiles(files, view.posAtCoords({ left: event.clientX, top: event.clientY })?.pos ?? view.state.selection.from)
            return true
          },
          decorations(state) {
            const value = editorToolkitKey.getState(state)!
            const decorations: Decoration[] = value.matches.map((match, index) => Decoration.inline(match.from, match.to,
              { class: index === value.active ? 'editor-search-match is-current' : 'editor-search-match' }))
            if (state.doc.childCount === 1 && state.doc.firstChild?.type.name === 'paragraph' && !state.doc.firstChild.content.size) {
              decorations.push(Decoration.node(0, state.doc.firstChild.nodeSize,
                { class: 'is-editor-empty', 'data-placeholder': options.placeholder() }))
            }
            value.uploads.forEach((pos, id) => decorations.push(Decoration.widget(pos, () => {
              const span = document.createElement('span')
              span.className = 'editor-image-uploading'
              span.textContent = options.uploading()
              span.setAttribute('contenteditable', 'false')
              return span
            }, { key: id })))
            return DecorationSet.create(state.doc, decorations)
          },
        },
      })]
    },
  })
}
