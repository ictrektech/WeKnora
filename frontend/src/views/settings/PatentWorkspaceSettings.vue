<template>
  <div class="patent-workspace-settings">
    <div class="section-header">
      <h2>{{ t('integrations.tabs.patent') }}</h2>
      <p class="section-description">{{ t('patentWorkspaceSettings.description') }}</p>
    </div>

    <div class="intro">
      <t-icon name="info-circle" class="intro-icon" />
      <div>
        <p class="intro-title">{{ t('patentWorkspaceSettings.introTitle') }}</p>
        <p class="intro-desc">{{ t('patentWorkspaceSettings.introDescription') }}</p>
      </div>
    </div>

    <div v-if="patentWorkspace.loadError" role="alert" class="intro">
      <span>{{ t('patentWorkspace.loadFailed') }}</span>
      <t-button size="small" @click="patentWorkspace.load(true).catch(() => undefined)">{{ t('patentWorkspace.retry') }}</t-button>
    </div>
    <div class="settings-group">
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ t('patentWorkspaceSettings.enableLabel') }}</label>
          <p class="desc">{{ t('patentWorkspaceSettings.enableDescription') }}</p>
        </div>
        <div class="setting-control">
          <t-switch
            :value="patentWorkspace.enabled"
            :loading="patentWorkspace.saving"
            :disabled="!canEdit || patentWorkspace.loading || !!patentWorkspace.loadError"
            @change="handleToggle"
          />
        </div>
      </div>
    </div>

    <div class="danger-zone">
      <div>
        <h3>{{ t('patentWorkspaceSettings.deleteTitle') }}</h3>
        <p>{{ t('patentWorkspaceSettings.deleteDescription') }}</p>
      </div>
      <t-button
        theme="danger"
        variant="outline"
        :disabled="!canDelete || deleteWorking"
        :loading="deleteWorking"
        @click="confirmDelete"
      >
        {{ t('patentWorkspaceSettings.deleteButton') }}
      </t-button>
    </div>

    <p v-if="!canEdit" class="permission-hint">
      {{ t('patentWorkspaceSettings.adminOnly') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { DialogPlugin, MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'

import { useAuthStore } from '@/stores/auth'
import { usePatentWorkspaceStore } from '@/stores/patentWorkspace'

const { t } = useI18n()
const authStore = useAuthStore()
const patentWorkspace = usePatentWorkspaceStore()
const deleteWorking = ref(false)

const canEdit = computed(() => authStore.canAccessAllTenants || authStore.hasRole('admin'))
const canDelete = computed(() => authStore.canAccessAllTenants || authStore.hasRole('owner'))

onMounted(() => {
  void patentWorkspace.load().catch(() => undefined)
})

async function handleToggle(value: boolean) {
  if (!canEdit.value || patentWorkspace.saving) return
  try {
    await patentWorkspace.setEnabled(value)
    MessagePlugin.success(t('patentWorkspaceSettings.saveSuccess'))
  } catch (error: any) {
    MessagePlugin.error(
      t('patentWorkspaceSettings.saveFailed', { message: error?.message || '' }),
    )
  }
}

function confirmDelete() {
  if (!canDelete.value || deleteWorking.value) return

  let dialog: { destroy: () => void } | null = null
  dialog = DialogPlugin.confirm({
    header: t('patentWorkspaceSettings.deleteConfirmTitle'),
    body: t('patentWorkspaceSettings.deleteConfirmBody'),
    theme: 'warning',
    confirmBtn: {
      content: t('patentWorkspaceSettings.deleteButton'),
      theme: 'danger',
    },
    cancelBtn: t('patentWorkspaceSettings.cancel'),
    onConfirm: async () => {
      deleteWorking.value = true
      try {
        await patentWorkspace.deleteData()
        MessagePlugin.success(t('patentWorkspaceSettings.deleteSuccess'))
        dialog?.destroy()
      } catch (error: any) {
        MessagePlugin.error(
          t('patentWorkspaceSettings.deleteFailed', { message: error?.message || '' }),
        )
      } finally {
        deleteWorking.value = false
      }
    },
  })
}
</script>

<style scoped lang="less">
.patent-workspace-settings {
  width: 100%;
}

.section-header {
  margin-bottom: 24px;

  h2 {
    margin: 0 0 8px;
    color: var(--td-text-color-primary);
    font-size: 20px;
    font-weight: 600;
  }

  .section-description {
    margin: 0;
    color: var(--td-text-color-secondary);
    font-size: 14px;
    line-height: 1.5;
  }
}

.intro {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  margin-bottom: 8px;
  padding: 14px 16px;
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
}

.intro-icon {
  flex-shrink: 0;
  margin-top: 2px;
  color: var(--td-brand-color);
}

.intro-title {
  margin: 0 0 4px;
  color: var(--td-text-color-primary);
  font-size: 14px;
  font-weight: 500;
}

.intro-desc,
.desc,
.permission-hint {
  margin: 0;
  color: var(--td-text-color-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.settings-group {
  display: flex;
  flex-direction: column;
}

.setting-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
  padding: 20px 0;
  border-bottom: 1px solid var(--td-component-border);
}

.setting-info {
  min-width: 0;

  label {
    display: block;
    margin-bottom: 6px;
    color: var(--td-text-color-primary);
    font-size: 14px;
    font-weight: 500;
  }
}

.setting-control {
  flex-shrink: 0;
  padding-top: 2px;
}

.danger-zone {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  margin-top: 32px;
  padding: 18px 20px;
  border: 1px solid var(--td-error-color-3);
  border-radius: 8px;
  background: var(--td-error-color-1);

  h3 {
    margin: 0 0 6px;
    color: var(--td-error-color-7);
    font-size: 15px;
    font-weight: 600;
  }

  p {
    max-width: 680px;
    margin: 0;
    color: var(--td-text-color-secondary);
    font-size: 13px;
    line-height: 1.6;
  }
}

.permission-hint {
  margin-top: 14px;
  font-size: 12px;
}

@media (max-width: 680px) {
  .danger-zone {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
