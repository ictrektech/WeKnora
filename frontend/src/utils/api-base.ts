import { getAppBaseUrlPrefix } from './app-base'

export function getApiBaseUrl(): string {
  // The VOS app is served below /app/com.ictrek.hybrag/ while plain Docker
  // deployments are served at /. Route axios through the same runtime base so
  // /api requests reach the frontend nginx proxy in both modes.
  return getAppBaseUrlPrefix();
}
