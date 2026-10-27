<template><CreatChat :workspace-mode="mode" /></template>
<script setup lang="ts">
import CreatChat from '@/views/creatChat/creatChat.vue'
import type { PatentMode } from '@/utils/workspaceMode'
defineProps<{ mode: PatentMode }>()
</script>
