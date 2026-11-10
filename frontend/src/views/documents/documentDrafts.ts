import { readStructuredDocument, sameDocument, type StructuredDocument } from '../knowledge/browse/structuredDocument'
export interface DraftScope { userId: string; tenantId: string; kbId: string; documentId: string }
export interface DraftContent { title: string; content: string; baseVersion: number; document?: StructuredDocument | null; folderPath?: string }
export interface DocumentDraft extends DraftContent { schema: 1; writer: string; savedAt: number }
export interface StoredDraft extends DocumentDraft { key: string }
export type DraftStorage = Pick<Storage, 'length' | 'key' | 'getItem' | 'setItem' | 'removeItem'>

const PREFIX = 'weknora:document-draft:v1:'
export function draftPrefix(scope: DraftScope): string {
  return PREFIX + [scope.userId, scope.tenantId, scope.kbId, scope.documentId].map(encodeURIComponent).join(':') + ':'
}
export function draftKey(scope: DraftScope, writer: string): string {
  return draftPrefix(scope) + encodeURIComponent(writer)
}
function parseDraft(raw: string | null): DocumentDraft | null {
  if (!raw) return null
  try {
    const value = JSON.parse(raw)
    if (value.schema !== 1 || typeof value.writer !== 'string' || typeof value.title !== 'string' ||
      typeof value.content !== 'string' || !Number.isSafeInteger(value.baseVersion) || value.baseVersion < 0 ||
      !Number.isFinite(value.savedAt) || value.savedAt <= 0) return null
    const document = readStructuredDocument(value.document)
    return { schema: 1, writer: value.writer, title: value.title, content: value.content,
      ...(typeof value.folderPath === 'string' ? { folderPath: value.folderPath } : {}),
      ...(document ? { document } : {}), baseVersion: value.baseVersion, savedAt: value.savedAt }
  } catch { return null }
}
export function readDocumentDrafts(storage: DraftStorage, scope: DraftScope): StoredDraft[] {
  const prefix = draftPrefix(scope)
  const drafts: StoredDraft[] = []
  for (let index = 0; index < storage.length; index++) {
    const key = storage.key(index)
    if (!key?.startsWith(prefix)) continue
    const value = parseDraft(storage.getItem(key))
    if (value && key === draftKey(scope, value.writer)) drafts.push({ ...value, key })
  }
  return drafts.sort((a, b) => b.savedAt - a.savedAt)
}
export function writeDocumentDraft(storage: DraftStorage, scope: DraftScope, writer: string, value: DraftContent, now = Date.now()): StoredDraft {
  const draft: DocumentDraft = { ...value, schema: 1, writer, savedAt: now }
  const key = draftKey(scope, writer)
  storage.setItem(key, JSON.stringify(draft))
  return { ...draft, key }
}
/** Never delete a newer draft written by a still-open tab. */
export function removeDocumentDraft(storage: DraftStorage, draft: StoredDraft): boolean {
  const current = parseDraft(storage.getItem(draft.key))
  if (!current || current.savedAt !== draft.savedAt || current.content !== draft.content || current.title !== draft.title ||
    current.baseVersion !== draft.baseVersion || current.writer !== draft.writer || current.folderPath !== draft.folderPath || !sameDocument(current.document, draft.document)) return false
  storage.removeItem(draft.key)
  return true
}
