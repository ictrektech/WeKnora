import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { localizedApiDocs } from '@/utils/apiDocs'

const modules = {
  ...import.meta.glob('../../../website-docs/04-api/*.md', { eager: true, query: '?raw', import: 'default' }),
  ...import.meta.glob('../../../docs/api/*.md', { eager: true, query: '?raw', import: 'default' }),
  ...import.meta.glob('../../../ictrek.app/docs/api/*.md', { eager: true, query: '?raw', import: 'default' }),
} as Record<string, string>

export function useApiDocs() {
  const { locale } = useI18n()
  return computed(() => localizedApiDocs(modules, locale.value))
}
