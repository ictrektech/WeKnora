export interface AuthBootstrapAttempts {
  isLoggedIn: () => boolean
  hydrate: () => Promise<boolean>
  vosSSO: () => Promise<boolean>
}

export function createInitialAuthSessionValidator() {
  let validated = false
  let pending: Promise<boolean> | null = null

  return async (attempts: AuthBootstrapAttempts) => {
    if (validated) return attempts.isLoggedIn()
    if (pending) return pending

    pending = (async () => {
      const restored = await attempts.hydrate() ||
        await attempts.vosSSO()
      if (restored) validated = true
      return restored
    })().finally(() => {
      pending = null
    })

    return pending
  }
}
