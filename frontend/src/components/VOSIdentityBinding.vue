<template>
  <div v-if="available" class="setting-row">
    <div class="setting-info">
      <label>{{ t('vosIdentityBinding.title') }}</label>
      <p class="desc">{{ t('vosIdentityBinding.description') }}</p>
      <p v-if="message" role="status" class="desc">{{ message }}</p>
    </div>
    <div class="setting-control">
      <t-button variant="outline" :disabled="linked" @click="message = ''; confirming = true">
        {{ t(linked ? 'vosIdentityBinding.linked' : 'vosIdentityBinding.action') }}
      </t-button>
    </div>
    <t-dialog v-model:visible="confirming" :header="t('vosIdentityBinding.title')"
      :confirm-btn="{ content: t('vosIdentityBinding.action'), loading: busy }"
      :cancel-btn="{ content: t('common.cancel'), disabled: busy }" :close-on-overlay-click="!busy"
      :close-btn="!busy" @confirm="bind">
      <p>{{ t('vosIdentityBinding.confirm', { username }) }}</p>
      <p v-if="message" role="alert">{{ message }}</p>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { bindVOSIdentity } from '@/api/auth'
import { acquireVOSFastpathToken, waitForVOSFastpathPlatform } from '@/utils/vos-fastpath'
import { getVOSAccessTokenForIframeSSO } from '@/utils/vos-sso'

defineProps<{ username: string }>()
const { t } = useI18n()
const available = ref(false)
onMounted(async () => { available.value = !!(await waitForVOSFastpathPlatform()) || !!getVOSAccessTokenForIframeSSO() })
const confirming = ref(false)
const busy = ref(false)
const linked = ref(false)
const message = ref('')

const bind = async () => {
  if (busy.value) return
  busy.value = true
  message.value = ''
  try {
    const session = localStorage.getItem('weknora_token')
    if (!session) throw new Error('missing_hybrag_session')
    let proof: string | null = null
    try { proof = (await acquireVOSFastpathToken({ force: true }))?.access_token || null } catch { /* Existing VOS SSO can also supply a human proof. */ }
    proof ||= getVOSAccessTokenForIframeSSO()
    if (!proof) throw new Error('missing_vos_user_proof')
    if (localStorage.getItem('weknora_token') !== session) throw new Error('hybrag_session_changed')
    const result = await bindVOSIdentity(proof, session)
    if (!result.success) throw new Error('identity_binding_failed')
    linked.value = true
    confirming.value = false
    message.value = t('vosIdentityBinding.success')
  } catch {
    message.value = t('vosIdentityBinding.failed')
  } finally { busy.value = false }
}
</script>

<style scoped>
.setting-row { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 20px 0; border-bottom: 1px solid var(--td-component-border); }
.setting-info { flex: 1; min-width: 0; }
label { font-weight: 500; }
.desc { margin: 6px 0 0; color: var(--td-text-color-secondary); font-size: 13px; line-height: 1.6; }
.setting-control { flex-shrink: 0; }
</style>
