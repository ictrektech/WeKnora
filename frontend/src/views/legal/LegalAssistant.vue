<template>
  <div class="legal-assistant-shell">
    <RouterView />
  </div>
</template>

<script setup lang="ts">
import { onUnmounted } from 'vue'
import { useSettingsStore } from '@/stores/settings'

const settings = useSettingsStore()

// Apply before the nested composer mounts so its initial resource loads use
// legal preferences rather than the ordinary chat profile.
settings.enterLegalAssistant()
onUnmounted(() => settings.leaveLegalAssistant())
</script>

<style scoped>
.legal-assistant-shell {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
}
</style>
