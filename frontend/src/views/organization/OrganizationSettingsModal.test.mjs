import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const source = readFileSync(new URL('./OrganizationSettingsModal.vue', import.meta.url), 'utf8')
// 遮罩层由共享壳 SettingsModalShell 渲染，overscroll 规则也随之搬到那里。
const shellSource = readFileSync(new URL('../../components/SettingsModalShell.vue', import.meta.url), 'utf8')

test('reviewing join requests goes through the store and refreshes modal data', () => {
  assert.match(
    source,
    /const refreshOrganizationAfterReview = async \(\) => \{\s*await Promise\.all\(\[\s*fetchOrgDetail\(\),\s*fetchMembers\(\)\s*\]\)\s*\}/
  )

  assert.match(source, /orgStore\.reviewOrganizationJoinRequest\(/)
  const refreshCalls = source.match(/await refreshOrganizationAfterReview\(\)/g) ?? []
  assert.equal(refreshCalls.length, 2)
})

test('organization settings use one outer content scroller and reset it on navigation', () => {
  assert.match(source, /ref="contentWrapperRef" class="content-wrapper"/)
  assert.match(source, /class="data-table-shell members-table-shell"/)
  assert.match(source, /contentWrapperRef\.value\?\.scrollTo\(\{ top: 0, behavior: 'auto' \}\)/)
  assert.match(source, /watch\(\(\) => props\.visible,[\s\S]*?void scrollContentToTop\(\)/)
  assert.match(source, /\}, \{ immediate: true \}\)\s*\n\s*watch\(\(\) => props\.orgId/)
  assert.match(source, /watch\(\(\) => props\.orgId,[\s\S]*?void scrollContentToTop\(\)/)
  assert.match(source, /watch\(currentSection,[\s\S]*?void scrollContentToTop\(\)/)
  assert.match(source, /\.members-table-shell\s*\{[\s\S]*?overflow: visible;[\s\S]*?\.t-table__content/)
})

test('organization settings lock and restore background scrolling', () => {
  assert.match(source, /document\.body\.style\.overflow = 'hidden'/)
  assert.match(source, /document\.body\.style\.overflow = previousBodyOverflow/)
  assert.match(source, /onBeforeUnmount\(\(\) => \{\s*unlockBackgroundScroll\(\)/)
  assert.match(shellSource, /\.settings-overlay\s*\{[\s\S]*?overscroll-behavior: none;/)
  assert.match(source, /\.content-wrapper\s*\{[\s\S]*?overscroll-behavior: contain;/)
})

test('dingtalk import button is a separate admin-only entry wired to the dialog', () => {
  // 独立按钮（不与现有添加成员 popup 合并），管理员 + 钉钉启用才可见
  assert.match(source, /v-if="dingtalkImportEnabled"/)
  assert.match(source, /const dingtalkImportEnabled = computed\(\s*\(\) => isAdmin\.value && dingtalkEnabled\.value && !!props\.orgId && !isCreateMode\.value\s*\)/)
  // 弹窗打开时拉一次公开配置 /auth/dingtalk/config 决定可见性
  assert.match(source, /getDingTalkConfig\(\)/)
  assert.match(source, /fetchSharedKBs\(\)\s*\n\s*ensureDingTalkConfig\(\)/)
  // 对话框挂载并回传刷新成员列表
  assert.match(source, /<DingTalkImportDialog[\s\S]*?v-model:visible="dingtalkImportVisible"[\s\S]*?:org-id="props\.orgId"[\s\S]*?@done="fetchMembers"/)
})
