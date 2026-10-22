/**
 * 浏览模式文档图标：按知识类型/文件扩展名映射到 tdesign 图标名。
 * 文档树行与阅读器头部共用，保证同一文档在两处图标一致。
 */
import type { KnowledgeTreeDocument } from '@/api/knowledge-base';

const EXT_ICON_MAP: Record<string, string> = {
  pdf: 'file-pdf',
  doc: 'file-word',
  docx: 'file-word',
  xls: 'file-excel',
  xlsx: 'file-excel',
  csv: 'file-excel',
  ppt: 'file-powerpoint',
  pptx: 'file-powerpoint',
  png: 'file-image',
  jpg: 'file-image',
  jpeg: 'file-image',
  gif: 'file-image',
  md: 'file-paste',
  markdown: 'file-paste',
  txt: 'file',
};

export const browseDocIcon = (doc: Pick<KnowledgeTreeDocument, 'type' | 'file_type'>): string => {
  if (doc.type === 'manual') return 'edit';
  if (doc.type === 'url') return 'link';
  if (doc.type === 'faq') return 'chat-bubble-help';
  const ext = (doc.file_type || '').toLowerCase();
  return EXT_ICON_MAP[ext] || 'file';
};
