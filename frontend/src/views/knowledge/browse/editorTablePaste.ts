import { Fragment, Slice, type Node as PMNode } from '@tiptap/pm/model'
import { TextSelection, type Transaction } from '@tiptap/pm/state'
import { closeHistory } from '@tiptap/pm/history'
import { CellSelection, TableMap, cellAround, handlePaste, isInTable, selectedRect } from '@tiptap/pm/tables'
import type { EditorView } from '@tiptap/pm/view'
import { withinDocumentLimits } from './editorDocumentLimits'

export type TablePasteProblem = 'selection' | 'merged' | 'limit'

/** Measure table-shaped clipboard slices without interpreting ordinary text as cells. */
function tableSize(slice: Slice, schema: EditorView['state']['schema']) {
  let content = slice.content, open = slice.openStart
  while (content.childCount === 1 && open > 0 && !content.firstChild?.type.spec.tableRole) {
    content = content.firstChild!.content; open--
  }
  const first = content.firstChild
  if (!first) return null
  let table: PMNode
  const role = first.type.spec.tableRole
  if (role === 'table' && content.childCount === 1) table = first
  else if (role === 'row') table = schema.nodes.table!.create(null, content)
  else if (role === 'cell' || role === 'header_cell') table = schema.nodes.table!.create(null, schema.nodes.tableRow!.create(null, content))
  else return null
  const map = TableMap.get(table)
  return { width: map.width, height: map.height }
}

/** Use the table editor's atomic paste, guarded against clipping and unsavable results. */
export function pasteTableCells(view: EditorView, event: ClipboardEvent, slice: Slice,
  reject: (problem: TablePasteProblem) => void): boolean {
  if (!view.editable || !isInTable(view.state) || view.state.selection.$from.parent.type.spec.code) return false
  const size = tableSize(slice, view.state.schema)
  const selection = view.state.selection
  if (!slice.size || (!size && event.clipboardData?.files.length)) return false
  if (!size && !(selection instanceof CellSelection)) return false
  if (!(selection instanceof CellSelection) && cellAround(selection.$from)?.pos !== cellAround(selection.$to)?.pos) {
    reject('selection'); return true
  }
  const rect = selectedRect(view.state)
  const rows = size?.height ?? 1, cols = size?.width ?? 1
  const multiple = selection instanceof CellSelection && (rect.right - rect.left > 1 || rect.bottom - rect.top > 1)
  if (multiple && size && rows * cols > 1 && (rows !== rect.bottom - rect.top || cols !== rect.right - rect.left)) {
    reject('selection'); return true
  }
  const bottom = multiple ? rect.bottom : rect.top + rows
  const right = multiple ? rect.right : rect.left + cols
  if (bottom > 500 || right > 100) { reject('limit'); return true }
  for (let y = rect.top; y < Math.min(bottom, rect.map.height); y++) {
    for (let x = rect.left; x < Math.min(right, rect.map.width); x++) {
      const cell = rect.table.nodeAt(rect.map.map[y * rect.map.width + x]!)!
      if (cell.attrs.colspan > 1 || cell.attrs.rowspan > 1) { reject('merged'); return true }
    }
  }
  // One explicitly selected cell is an insertion anchor, not a clipping rectangle.
  const state = selection instanceof CellSelection && !multiple
    ? view.state.apply(view.state.tr.setSelection(TextSelection.near(selection.$anchorCell, 1))) : view.state
  let pasted: Transaction | undefined
  const proxy = new Proxy(view, { get(target, key) {
    if (key === 'state') return state
    if (key === 'dispatch') return (tr: Transaction) => { pasted = tr }
    return Reflect.get(target, key)
  } })
  if (!handlePaste(proxy, event, slice) || !pasted) return false
  // Keep the destination's headers and column sizing; cell text/marks come from the source.
  const tr: Transaction = pasted
  const nextTable = tr.doc.nodeAt(rect.tableStart - 1)!
  const nextMap = TableMap.get(nextTable)
  const seen = new Set<number>()
  for (let y = rect.top; y < bottom; y++) {
    for (let x = rect.left; x < right; x++) {
      const pos = nextMap.map[y * nextMap.width + x]!
      if (seen.has(pos)) continue
      seen.add(pos)
      if (y >= rect.map.height) continue
      const old = x < rect.map.width ? rect.table.nodeAt(rect.map.map[y * rect.map.width + x]!)! : rect.table.child(y).lastChild!
      const cell = nextTable.nodeAt(pos)!
      const widths = Array.from({ length: cell.attrs.colspan }, (_, i) => {
        const previous = x + i < rect.map.width ? rect.table.nodeAt(rect.map.map[y * rect.map.width + x + i]!) : null
        return previous?.attrs.colwidth?.[0] || 0
      })
      tr.setNodeMarkup(rect.tableStart + pos, old.type, {
        ...cell.attrs,
        align: cell.attrs.align || old.attrs.align,
        colwidth: widths.some(Boolean) ? widths : cell.attrs.colwidth,
      })
    }
  }
  if (!withinDocumentLimits(tr.doc, true)) { reject('limit'); return true }
  view.dispatch(closeHistory(tr).setMeta('paste', true).setMeta('uiEvent', 'paste').scrollIntoView())
  // A subsequent keystroke must not become part of the paste's undo group.
  view.dispatch(closeHistory(view.state.tr))
  return true
}

export function spreadsheetSlice(rows: string[][], schema: EditorView['state']['schema']) {
  const s = schema.nodes
  const table = s.table!.create(null, rows.map(row => s.tableRow!.create(null, row.map(text => {
    const content = text.split('\n').map(line => s.paragraph!.create(null, line ? schema.text(line) : null))
    return s.tableCell!.create(null, content)
  }))))
  return new Slice(Fragment.from(table), 0, 0)
}
