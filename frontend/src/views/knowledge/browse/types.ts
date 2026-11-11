/** 阅读和编辑共用的标题大纲；编辑器位置仅在编辑器内解析。 */
export interface OutlineItem {
  text: string
  level: number // 1-6
  section?: { token: number; up: boolean; down: boolean }
}

export interface OutlineSectionMove {
  index: number
  direction: 'up' | 'down'
  token: number
}
