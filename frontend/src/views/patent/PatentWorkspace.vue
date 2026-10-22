<template>
  <div class="patent-workspace"><RouterView :key="String(route.name)" /></div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useUIStore } from '@/stores/ui'
import { useSettingsStore } from '@/stores/settings'
import { isPatentMode, routeWorkspaceMode } from '@/utils/workspaceMode'
const route = useRoute()
const settings = useSettingsStore()
const auth = useAuthStore()
const ui = useUIStore()
const mobile = window.matchMedia('(max-width: 680px)')
let desktopCollapsed: boolean | null = null
const syncMobileSidebar = () => {
  if (mobile.matches) {
    if (desktopCollapsed === null) desktopCollapsed = ui.sidebarCollapsed
    ui.sidebarCollapsed = true
  } else if (desktopCollapsed !== null) {
    ui.sidebarCollapsed = desktopCollapsed
    desktopCollapsed = null
  }
}
onMounted(() => {
  syncMobileSidebar()
  mobile.addEventListener('change', syncMobileSidebar)
})
watch(() => [routeWorkspaceMode(route.name), auth.effectiveTenantId, auth.currentUserId] as const, (current, previous) => {
  if (previous && current.every((value, index) => value === previous[index])) return
  const [mode] = current
  if (isPatentMode(mode)) settings.enterPatentWorkspace(mode)
}, { immediate: true, flush: 'sync' })
watch(() => settings.settings, () => {
  if (settings._patentMode) settings.persistComposerSettings()
}, { deep: true })
onBeforeUnmount(() => {
  settings.leavePatentWorkspace()
  mobile.removeEventListener('change', syncMobileSidebar)
  if (desktopCollapsed !== null) ui.sidebarCollapsed = desktopCollapsed
})
</script>
<style scoped>
.patent-workspace { display: flex; flex-direction: column; width: 100%; height: 100%; min-width: 0; min-height: 0; }
@media (max-width: 680px) {
  .patent-workspace { overflow-y: auto; }
  .patent-workspace :deep(.dialogue-wrap) { padding: 24px 12px; box-sizing: border-box; }
  .patent-workspace :deep(.dialogue-answers) { min-width: 0; gap: 16px; }
  .patent-workspace :deep(.dialogue-title) { font-size: 22px; line-height: 1.4; text-align: center; }
  .patent-workspace :deep(.suggested-question-text) { white-space: normal; }
  .patent-workspace :deep(.suggested-questions-container) { padding: 0; }
}
</style>
