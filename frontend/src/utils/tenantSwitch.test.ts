import assert from 'node:assert/strict'
import test from 'node:test'

import { tenantSwitchTargetPath } from './tenantSwitchPath'

const fallback = '/platform/knowledge-bases'

test('tenant switch defaults to the knowledge-base landing page', () => {
  assert.equal(tenantSwitchTargetPath('/platform/settings'), fallback)
  assert.equal(tenantSwitchTargetPath('/platform/settings', undefined), fallback)
})

test('tenant switch accepts same-origin platform resource deep-links', () => {
  assert.equal(
    tenantSwitchTargetPath('/platform/settings', '/platform/agents?edit=builtin-agent&section=model'),
    '/platform/agents?edit=builtin-agent&section=model',
  )
  assert.equal(
    tenantSwitchTargetPath('/platform/settings', '/platform/knowledge-bases/kb-1?model_usage_section=asr'),
    '/platform/knowledge-bases/kb-1?model_usage_section=asr',
  )
})

test('tenant switch rejects external, protocol-relative and malformed destinations', () => {
  for (const destination of [
    'https://example.com/platform/agents',
    '//example.com/platform/agents',
    '/other/path',
    '/platform/../login',
    '/platform/%2e%2e/login',
    '/platform\\evil.example/path',
    '/platform/agents\n?edit=agent-1',
    '',
  ]) {
    assert.equal(tenantSwitchTargetPath('/platform/settings', destination), fallback)
  }
})
