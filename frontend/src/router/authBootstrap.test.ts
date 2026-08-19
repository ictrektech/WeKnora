import assert from 'node:assert/strict'
import test from 'node:test'
import { createInitialAuthSessionValidator } from './authBootstrap'

test('validates a cached login before protected pages start loading', async () => {
  const validate = createInitialAuthSessionValidator()
  let hydrateCalls = 0

  const restored = await validate({
    isLoggedIn: () => true,
    hydrate: async () => {
      hydrateCalls += 1
      return true
    },
    vosSSO: async () => false,
  })

  assert.equal(restored, true)
  assert.equal(hydrateCalls, 1)
})

test('replaces a stale cached session through VOS SSO', async () => {
  const validate = createInitialAuthSessionValidator()
  const attempts: string[] = []

  const restored = await validate({
    isLoggedIn: () => true,
    hydrate: async () => {
      attempts.push('hydrate')
      return false
    },
    vosSSO: async () => {
      attempts.push('vos-sso')
      return true
    },
  })

  assert.equal(restored, true)
  assert.deepEqual(attempts, ['hydrate', 'vos-sso'])
})

test('shares one initial validation across concurrent route guards', async () => {
  const validate = createInitialAuthSessionValidator()
  let hydrateCalls = 0
  let releaseHydrate: (() => void) | undefined
  const hydrateGate = new Promise<void>((resolve) => {
    releaseHydrate = resolve
  })
  const attempts = {
    isLoggedIn: () => true,
    hydrate: async () => {
      hydrateCalls += 1
      await hydrateGate
      return true
    },
    vosSSO: async () => false,
  }

  const first = validate(attempts)
  const second = validate(attempts)
  releaseHydrate?.()

  assert.deepEqual(await Promise.all([first, second]), [true, true])
  assert.equal(hydrateCalls, 1)
})
