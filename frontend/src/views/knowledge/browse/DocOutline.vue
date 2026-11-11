<template>
  <aside class="doc-outline" :class="{ 'is-collapsed': collapsed }">
    <div class="doc-outline__header">
      <template v-if="!collapsed">
        <span class="doc-outline__title">{{ t('knowledgeBase.browse.outline') }}</span>
        <t-tooltip :content="t('knowledgeBase.folderTree.collapse')" placement="top">
          <button
            type="button"
            class="doc-outline__icon-btn"
            :aria-label="t('knowledgeBase.folderTree.collapse')"
            @click="emit('update:collapsed', true)"
          >
            <t-icon name="chevron-right-double" size="15px" />
          </button>
        </t-tooltip>
      </template>
      <t-tooltip v-else :content="t('knowledgeBase.browse.outline')" placement="left">
        <button
          type="button"
          class="doc-outline__icon-btn"
          :aria-label="t('knowledgeBase.folderTree.expand')"
          @click="emit('update:collapsed', false)"
        >
          <t-icon name="view-list" size="15px" />
        </button>
      </t-tooltip>
    </div>

    <div v-if="!collapsed" class="doc-outline__body">
      <div v-if="!items.length" class="doc-outline__empty">
        {{ t('knowledgeBase.browse.emptyOutline') }}
      </div>
      <div v-for="(item, index) in items" :key="index" class="doc-outline__row">
        <div
          class="doc-outline__item"
          :class="{ active: index === activeIndex }"
          :style="{ '--outline-level': Math.min(item.level - 1, 5) }"
          :title="item.text || t('writingTools.emptyHeading')"
          :aria-current="index === activeIndex ? 'location' : undefined"
          role="button"
          tabindex="0"
          @click="emit('jump', index)"
          @keydown.enter.self="emit('jump', index)"
          @keydown.space.prevent.self="emit('jump', index)"
        >
          {{ item.text || t('writingTools.emptyHeading') }}
        </div>
        <div v-if="item.section && (item.section.up || item.section.down)" class="doc-outline__actions">
          <button v-for="direction in sectionDirections" :key="direction" type="button" class="doc-outline__icon-btn"
            :disabled="!item.section[direction]" :title="t(`sectionTools.${direction}`)" :aria-label="t(`sectionTools.${direction}`)"
            @mousedown.prevent @click.stop="emit('move-section', { index, direction, token: item.section.token })">
            <t-icon :name="direction === 'up' ? 'chevron-up' : 'chevron-down'" size="14px" />
          </button>
        </div>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { OutlineItem, OutlineSectionMove } from './types'

const sectionDirections = ['up', 'down'] as const

withDefaults(defineProps<{
  items: OutlineItem[]
  activeIndex?: number
  collapsed?: boolean
}>(), {
  activeIndex: -1,
  collapsed: false,
})

const emit = defineEmits<{
  jump: [index: number]
  'move-section': [request: OutlineSectionMove]
  'update:collapsed': [collapsed: boolean]
}>()

const { t } = useI18n()
</script>

<style scoped lang="less">
.doc-outline {
  --outline-indent: 12px;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  width: 200px;
  min-height: 0;
  padding-left: 12px;
  margin-left: 16px;
  border-left: 1px solid var(--td-component-stroke);
  box-sizing: border-box;

  &.is-collapsed {
    width: auto;
    padding-left: 8px;
    margin-left: 8px;
  }
}

.doc-outline__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  height: 32px;
  padding-bottom: 16px;
  flex-shrink: 0;
}

.doc-outline__title {
  font-size: var(--app-text-md);
  font-weight: 500;
  color: var(--td-text-color-primary);
  white-space: nowrap;
}

.doc-outline__icon-btn {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 0;
  border-radius: var(--app-radius-sm);
  background: transparent;
  color: var(--td-text-color-secondary);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background-color var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover {
    color: var(--td-brand-color);
    background: var(--td-bg-color-container-hover);
  }
}

.doc-outline__body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 8px 0 12px;
  scrollbar-width: thin;

  &::-webkit-scrollbar {
    width: 4px;
  }

  &::-webkit-scrollbar-thumb {
    border-radius: var(--app-radius-xs);
    background: var(--td-scrollbar-color);
  }
}

.doc-outline__empty {
  padding: 24px 8px;
  font-size: var(--app-text-xs);
  color: var(--td-text-color-placeholder);
  text-align: center;
}

.doc-outline__row { display: flex; align-items: center;
  .doc-outline__item { flex: 1; min-width: 0; }
  &:hover .doc-outline__actions, &:focus-within .doc-outline__actions { display: flex; }
}
.doc-outline__actions { display: none; flex-shrink: 0;
  button { width: 22px; height: 24px; }
  button:disabled { color: var(--td-text-color-disabled); cursor: default; background: transparent; }
}
@media (hover: none) { .doc-outline__actions { display: flex; } }
.doc-outline__icon-btn:focus-visible { outline: 2px solid var(--app-focus-border); outline-offset: -2px; }

.doc-outline__item {
  box-sizing: border-box;
  width: 100%;
  padding: 5px 6px 5px calc(6px + var(--outline-level, 0) * var(--outline-indent));
  border-radius: var(--app-radius-sm);
  border-left: 2px solid transparent;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
  transition: background var(--app-motion-fast) ease, color var(--app-motion-fast) ease;

  &:hover {
    background: var(--td-bg-color-container-hover);
    color: var(--td-text-color-primary);
  }

  &.active {
    border-left-color: var(--td-brand-color);
    color: var(--td-brand-color);
    font-weight: 500;
  }
}
</style>
