import test from 'node:test'
import assert from 'node:assert/strict'
import { isPatentMode, routeWorkspaceMode, workspaceChatPath, workspaceHome } from './workspaceMode'

test('conversation routing preserves the owning workspace', () => {
  for (const [route, mode, home] of [
    ['chat', 'platform', '/platform/creatChat'],
    ['legalAssistantChat', 'legal_assistant', '/platform/legal-assistant'],
    ['patentSearchChat', 'patent_search', '/platform/patent/search'],
    ['patentWritingChat', 'patent_writing', '/platform/patent/writing'],
  ]) {
    assert.equal(routeWorkspaceMode(route), mode)
    assert.equal(workspaceHome(mode), home)
    assert.equal(workspaceChatPath(mode, 's1'), mode === 'platform' ? '/platform/chat/s1' : `${home}/chat/s1`)
  }
  assert.equal(isPatentMode('legal_assistant'), false)
  assert.equal(isPatentMode('patent_search'), true)
})
