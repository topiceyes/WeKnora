import { Extension } from '@tiptap/core'
import { Fragment, type Node as PMNode, type Schema } from '@tiptap/pm/model'
import { NodeSelection, Plugin, PluginKey, Selection, type EditorState, type Transaction } from '@tiptap/pm/state'
import { closeHistory } from '@tiptap/pm/history'
import { Decoration, DecorationSet, type EditorView } from '@tiptap/pm/view'
import { withinDocumentLimits } from './editorDocumentLimits'
import { randomUUID } from '../../../utils/randomUUID'

export type BlockAction = 'duplicate' | 'delete' | 'up' | 'down'
export type BlockKind = 'paragraph' | 'heading1' | 'heading2' | 'heading3' | 'heading4' | 'bulletList' | 'orderedList' | 'blockquote'
export type BlockRange = { from: number; to: number }
type BlockTarget = number | BlockRange
export function topLevelBlocks(doc: PMNode) {
  const blocks: { node: PMNode; pos: number; index: number }[] = []
  doc.forEach((node, pos, index) => blocks.push({ node, pos, index }))
  return blocks
}
export function selectedTopLevelBlock(state: EditorState) {
  return state.selection.$from.depth ? state.selection.$from.before(1) : state.selection.from
}

/** A block action always includes whole outer nodes, even for a partial text selection. */
export function selectedBlockRange(state: EditorState, handlePos?: number): BlockRange {
  const blocks = topLevelBlocks(state.doc)
  const selected = state.selection.empty
    ? blocks.filter(b => b.pos === selectedTopLevelBlock(state))
    : blocks.filter(b => b.pos < state.selection.to && b.pos + b.node.nodeSize > state.selection.from)
  if (selected.length > 1 && (handlePos === undefined || selected.some(b => b.pos === handlePos))) {
    return { from: selected[0]!.pos, to: selected.at(-1)!.pos + selected.at(-1)!.node.nodeSize }
  }
  const single = blocks.find(b => b.pos === (handlePos ?? selected[0]?.pos ?? selectedTopLevelBlock(state))) || blocks.at(-1)!
  return { from: single.pos, to: single.pos + single.node.nodeSize }
}

export function resolveBlockRange(doc: PMNode, target: BlockTarget) {
  const blocks = topLevelBlocks(doc), first = blocks.findIndex(b => b.pos === (typeof target === 'number' ? target : target.from))
  if (first < 0) return null
  const end = typeof target === 'number' ? target + blocks[first]!.node.nodeSize : target.to
  const last = blocks.findIndex(b => b.pos + b.node.nodeSize === end)
  if (last < first) return null
  return { from: blocks[first]!.pos, to: end, blocks: blocks.slice(first, last + 1), first, last, all: blocks }
}

/** Only flat text blocks can be converted without losing nested content. */
export function convertedBlock(node: PMNode, kind: BlockKind, schema: Schema): Fragment | null {
  if (node.type.name === kind) return null
  const texts: PMNode[] = []
  const textBlock = (n: PMNode) => ['paragraph', 'heading'].includes(n.type.name)
  if (textBlock(node)) texts.push(node)
  else if (node.type.name === 'blockquote' && [...node.content.content].every(textBlock)) node.forEach(n => texts.push(n))
  else if (['bulletList', 'orderedList'].includes(node.type.name) && node.content.content.every(item =>
    item.type.name === 'listItem' && item.childCount === 1 && item.firstChild?.type.name === 'paragraph')) {
    node.forEach(item => texts.push(item.firstChild!))
  } else return null
  if (!texts.length) return null
  const s = schema.nodes
  const paragraphs = () => texts.map(n => s.paragraph!.create(null, n.content))
  let result: Fragment
  if (kind === 'paragraph') result = Fragment.fromArray(paragraphs())
  else if (/^heading[1-4]$/.test(kind) && s.heading) result = Fragment.fromArray(texts.map(n => s.heading!.create({ level: Number(kind.slice(-1)) }, n.content)))
  else if (kind === 'blockquote' && s.blockquote) result = Fragment.from(s.blockquote.create(null, paragraphs()))
  else if (['bulletList', 'orderedList'].includes(kind) && s[kind] && s.listItem) {
    result = Fragment.from(s[kind]!.create(null, paragraphs().map(p => s.listItem!.create(null, p))))
  } else return null
  return result.eq(Fragment.from(node)) ? null : result
}

function selectBlock(tr: Transaction, pos: number) {
  const node = tr.doc.nodeAt(pos)
  return node && NodeSelection.isSelectable(node) ? NodeSelection.create(tr.doc, pos) : Selection.near(tr.doc.resolve(pos))
}
function commit(view: EditorView, tr: EditorState['tr'], pos: number, limit: () => void) {
  if (!withinDocumentLimits(tr.doc, true)) { limit(); return false }
  tr.setSelection(selectBlock(tr, pos))
  view.dispatch(closeHistory(tr).scrollIntoView())
  view.dispatch(closeHistory(view.state.tr))
  view.focus()
  return true
}

/** destination is a boundary in the original document, never a nested position. */
export function moveTopLevelBlock(view: EditorView, target: BlockTarget, destination: number, limit: () => void = () => {}) {
  if (!view.editable || view.composing) return false
  const source = resolveBlockRange(view.state.doc, target)
  if (!source || ![...source.all.map(b => b.pos), view.state.doc.content.size].includes(destination)) return false
  if (destination >= source.from && destination <= source.to) return false
  const insertAt = destination > source.from ? destination - (source.to - source.from) : destination
  const content = view.state.doc.slice(source.from, source.to).content
  const tr = view.state.tr.delete(source.from, source.to).insert(insertAt, content)
  return commit(view, tr, insertAt, limit)
}

export function canBlockAction(state: EditorState, target: BlockTarget, action: BlockAction) {
  const source = resolveBlockRange(state.doc, target)
  return !!source && (action !== 'up' || source.first > 0) && (action !== 'down' || source.last < source.all.length - 1)
}
export function runBlockAction(view: EditorView, target: BlockTarget, action: BlockAction | BlockKind, limit: () => void = () => {}) {
  if (!view.editable || view.composing) return false
  const source = resolveBlockRange(view.state.doc, target)
  if (!source) return false
  if (action === 'up') return source.first > 0 && moveTopLevelBlock(view, source, source.all[source.first - 1]!.pos, limit)
  if (action === 'down') {
    const next = source.all[source.last + 1]
    return !!next && moveTopLevelBlock(view, source, next.pos + next.node.nodeSize, limit)
  }
  const tr = view.state.tr
  let selected = source.from
  if (action === 'duplicate') {
    selected = source.to; tr.insert(selected, view.state.doc.slice(source.from, source.to).content)
  } else if (action === 'delete') {
    if (source.blocks.length === source.all.length) tr.replaceWith(source.from, source.to, view.state.schema.nodes.paragraph!.create())
    else tr.delete(source.from, source.to)
    selected = Math.min(source.from, tr.doc.content.size)
  } else {
    if (source.blocks.length !== 1) return false
    const content = convertedBlock(source.blocks[0]!.node, action, view.state.schema)
    if (!content) return false
    tr.replaceWith(source.from, source.to, content)
  }
  return commit(view, tr, selected, limit)
}

const dragType = 'application/x-weknora-block'
export const editorBlocksKey = new PluginKey<DecorationSet>('document-block-tools')
export function createEditorBlocks(options: {
  label: string
  blocked: () => boolean
  open: (pos: number, rect: DOMRect) => void
  close: () => void
  limit: () => void
}) {
  return Extension.create({
    name: 'documentBlockTools', priority: 1200,
    addProseMirrorPlugins() {
      let drag: { range: BlockRange; doc: PMNode; token: string } | null = null
      let currentView: EditorView | null = null
      let boundary: number | null = null
      const usable = (view: EditorView) => view.editable && !view.composing && !options.blocked()
      const decorations = (state: EditorState) => {
        const doc = state.doc
        const items = topLevelBlocks(doc).map(({ pos }) => Decoration.widget(pos, (view, getPos) => {
          const anchor = document.createElement('span')
          anchor.className = 'editor-block-anchor'; anchor.contentEditable = 'false'
          const button = document.createElement('button')
          button.type = 'button'; button.className = 'editor-block-handle'; button.draggable = true
          button.textContent = '⠿'; button.title = options.label; button.setAttribute('aria-label', options.label)
          anchor.append(button)
          button.addEventListener('click', event => {
            event.stopPropagation()
            const position = getPos()
            if (usable(view) && position !== undefined) options.open(position, button.getBoundingClientRect())
          })
          button.addEventListener('dragstart', event => {
            event.stopPropagation()
            const position = getPos()
            if (!usable(view) || !event.dataTransfer || position === undefined) { event.preventDefault(); return }
            options.close()
            drag = { range: selectedBlockRange(view.state, position), doc: view.state.doc, token: randomUUID() }
            event.dataTransfer.setData(dragType, drag.token)
            event.dataTransfer.effectAllowed = 'move'
          })
          button.addEventListener('dragend', () => {
            drag = null
            if (currentView && boundary !== null) currentView.dispatch(currentView.state.tr.setMeta(editorBlocksKey, null))
          })
          return anchor
        }, { side: -1, key: `block-handle-${pos}`, stopEvent: () => true }))
        const selected = resolveBlockRange(doc, selectedBlockRange(state))!
        if (selected.blocks.length > 1) {
          for (const block of selected.blocks) items.push(Decoration.node(block.pos, block.pos + block.node.nodeSize, { class: 'editor-block-range' }))
        }
        if (boundary !== null) items.push(Decoration.widget(boundary, () => {
          const line = document.createElement('span'); line.className = 'editor-block-drop'; line.contentEditable = 'false'; return line
        }, { side: -2 }))
        return DecorationSet.create(doc, items)
      }
      const dropAt = (view: EditorView, y: number) => {
        for (const block of topLevelBlocks(view.state.doc)) {
          const element = view.nodeDOM(block.pos)
          if (!(element instanceof HTMLElement)) continue
          const rect = element.getBoundingClientRect()
          if (y < rect.top + rect.height / 2) return block.pos
        }
        return view.state.doc.content.size
      }
      const recognized = (event: DragEvent) => Array.from(event.dataTransfer?.types || []).includes(dragType)
      const validDrag = (view: EditorView) => drag && drag.doc === view.state.doc && usable(view)
      return [new Plugin<DecorationSet>({
        key: editorBlocksKey,
        state: {
          init: (_, state) => decorations(state),
          apply(tr, previous, _before, next) {
            if (tr.docChanged) { drag = null; boundary = null; return decorations(next) }
            if (tr.getMeta(editorBlocksKey) !== undefined) { boundary = tr.getMeta(editorBlocksKey); return decorations(next) }
            if (tr.selectionSet) return decorations(next)
            return previous
          },
        },
        props: {
          decorations: state => editorBlocksKey.getState(state),
          handleDOMEvents: {
            dragover(view, event) {
              if (!recognized(event)) return false
              event.preventDefault()
              // Only the top-level boundary indicator applies to internal block moves.
              event.stopImmediatePropagation()
              if (!validDrag(view)) { if (event.dataTransfer) event.dataTransfer.dropEffect = 'none'; return true }
              if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
              const next = dropAt(view, event.clientY)
              if (boundary !== next) view.dispatch(view.state.tr.setMeta(editorBlocksKey, next))
              return true
            },
            drop(view, event) {
              if (!recognized(event)) return false
              event.preventDefault()
              event.stopImmediatePropagation()
              const source = drag
              const valid = validDrag(view) && event.dataTransfer?.getData(dragType) === source?.token
              drag = null
              view.dispatch(view.state.tr.setMeta(editorBlocksKey, null))
              if (valid && source) moveTopLevelBlock(view, source.range, dropAt(view, event.clientY), options.limit)
              return true
            },
            dragleave(view, event) {
              if (boundary !== null && !view.dom.contains(event.relatedTarget as Node | null)) view.dispatch(view.state.tr.setMeta(editorBlocksKey, null))
              return false
            },
          },
        },
        view(view) {
          currentView = view
          return { update(next, before) {
            if (next.state.doc !== before.doc || !next.state.selection.eq(before.selection) || !next.editable) options.close()
            if (!next.editable) drag = null
          }, destroy() { currentView = null; drag = null; options.close() } }
        },
      })]
    },
  })
}
