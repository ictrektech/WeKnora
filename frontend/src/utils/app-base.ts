const VOS_APP_PREFIX = '/app/com.ictrek.hybrag/'

declare global {
  interface Window {
    __HYBRAG_BASE_PATH__?: string
  }
}

function normalizeBasePath(value?: string): string {
  if (!value || value === './' || value === '.') return '/'
  let base = value
  if (!base.startsWith('/')) base = `/${base}`
  if (!base.endsWith('/')) base = `${base}/`
  return base
}

export function getAppBasePath(): string {
  if (typeof window !== 'undefined') {
    const runtime = normalizeBasePath(window.__HYBRAG_BASE_PATH__)
    if (runtime !== '/') return runtime

    const pathname = window.location.pathname || '/'
    const vosPrefixIndex = pathname.indexOf(VOS_APP_PREFIX)
    if (vosPrefixIndex >= 0) {
      return pathname.slice(0, vosPrefixIndex) + VOS_APP_PREFIX
    }
  }

  return normalizeBasePath(import.meta.env.BASE_URL)
}

export function getAppBaseUrlPrefix(): string {
  const base = getAppBasePath().replace(/\/+$/, '')
  return base === '' ? '' : base
}

export function withAppBasePath(path: string): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  const prefix = getAppBaseUrlPrefix()
  return prefix ? `${prefix}${normalizedPath}` : normalizedPath
}
