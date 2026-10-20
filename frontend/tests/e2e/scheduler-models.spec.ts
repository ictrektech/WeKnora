import { expect, test } from '@playwright/test'

test.use({ launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE } })

test('registered Scheduler models can be selected, refreshed and saved without credentials', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  const mainSource = await (await page.request.get('/src/main.ts')).text()
  const dependency = (name: string) => mainSource.match(new RegExp(`"([^"]*/${name}\\.js[^"]*)"`))![1]
  let available = true
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    let body: unknown = { success: true, data: {} }
    if (path.endsWith('/models/scheduler')) body = { success: true, enabled: true, data: available ? ['registered/qwen-chat', 'registered/bge-m3'] : ['registered/new-model'] }
    else if (path.endsWith('/models/providers')) body = { success: true, data: [{ value: 'generic', label: 'Custom', modelTypes: ['chat'], defaultUrls: {}, models: [], thinking: { format: '', levels: [] } }] }
    else if (path.includes('/ollama/status')) body = { success: true, available: false }
    return route.fulfill({ json: body })
  })
  await page.route('**/scheduler-model-test', route => route.fulfill({
    contentType: 'text/html',
    body: `<title>Scheduler model editor QA</title><div id="app"></div><script type="module">
      import { createApp, h, ref, nextTick } from '${dependency('vue')}'
      import { createPinia } from '${dependency('pinia')}'
      import TDesign from '${dependency('tdesign-vue-next')}'
      import '/node_modules/tdesign-vue-next/dist/tdesign.css'
      import i18n from '/src/i18n/index.ts'
      import ModelEditorDialog from '/src/components/ModelEditorDialog.vue'
      const app = createApp({ setup() {
        const visible = ref(false)
        nextTick(() => visible.value = true)
        return () => h(ModelEditorDialog, { visible: visible.value, modelType: 'chat',
          saveModel: async data => { window.savedModel = data },
          'onUpdate:visible': value => visible.value = value })
      } })
      app.use(createPinia()).use(TDesign).use(i18n).mount('#app')
    </script>`,
  }))
  await page.goto('/scheduler-model-test')
  await expect(page).toHaveTitle('Scheduler model editor QA')
  const scheduler = page.locator('.form-item').filter({ has: page.locator('label', { hasText: /^V-Scheduler$/ }) })
  await expect(scheduler).toBeVisible()
  await scheduler.locator('.t-select').click()
  await page.getByText('registered/qwen-chat', { exact: true }).click()
  await expect(scheduler.locator('input')).toHaveValue('registered/qwen-chat')
  expect(await page.locator('input').evaluateAll(inputs => inputs.some(input => (input as HTMLInputElement).value === 'http://v-scheduler:8080/v1'))).toBe(true)
  available = false
  await scheduler.getByRole('button').click()
  await scheduler.locator('.t-select').click()
  await page.getByText('registered/new-model', { exact: true }).click()
  await expect(scheduler.locator('input')).toHaveValue('registered/new-model')
  await page.screenshot({ path: testInfo.outputPath('scheduler-models.png'), fullPage: true })
  await page.locator('.t-drawer__footer').getByRole('button', { name: /Save|保存|Confirm|确认/i }).last().click()
  await expect.poll(() => page.evaluate(() => (window as any).savedModel)).toMatchObject({
    modelName: 'registered/new-model', source: 'remote', provider: 'generic',
    baseUrl: 'http://v-scheduler:8080/v1', apiKey: '',
  })
  expect(errors).toEqual([])
})
