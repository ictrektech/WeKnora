import { expect, test } from '@playwright/test'

test.use({ locale: 'en-US' })

test('automatic follows a delayed VOS bridge while manual language and form state survive', async ({ page }) => {
  const mainSource = await (await page.request.get('/src/main.ts')).text()
  const dependency = (name: string) => mainSource.match(new RegExp(`"([^"]*/${name}\\.(?:js|ts)[^"]*)"`))![1]
  await page.route('**/vos-locale-test', (route) => route.fulfill({
    contentType: 'text/html',
    body: '<iframe src="/app/com.ictrek.hybrag/locale-test" style="width:100%;height:900px"></iframe>',
  }))
  // Mount the real settings component without requiring backend authentication.
  await page.route('**/app/com.ictrek.hybrag/locale-test', (route) => route.fulfill({
    contentType: 'text/html',
    body: `<div id="app"></div><script type="module">
      import { createApp, h, ref } from '${dependency('vue')}'
      import { createPinia } from '${dependency('pinia')}'
      import TDesign from '${dependency('tdesign-vue-next')}'
      import '/node_modules/tdesign-vue-next/dist/tdesign.css'
      import i18n from '${dependency('i18n/index')}'
      import GeneralSettings from '/src/views/settings/GeneralSettings.vue'
      const app = createApp({
        setup() {
          const draft = ref('')
          return () => h('div', [
            h('input', { id: 'draft', value: draft.value, onInput: e => { draft.value = e.target.value } }),
            h(GeneralSettings),
          ])
        },
      })
      app.use(createPinia()).use(TDesign).use(i18n).mount('#app')
      window.testI18n = i18n
    </script>`,
  }))
  await page.goto('/vos-locale-test')
  const frame = page.frameLocator('iframe')
  const heading = frame.locator('h2')
  await expect(heading).toHaveText('General Settings')
  const child = page.frames().find((item) => item.url().endsWith('/locale-test'))!
  await frame.locator('#draft').fill('unsaved form')
  await child.evaluate(() => {
    const w = window as any
    w.originalDraft = document.querySelector('#draft')
    w.bridgeOrder = []
    w.vos_platform = {
      getState: async () => { w.bridgeOrder.push('snapshot'); return { locale: 'zh-CN' } },
      broadcast: { on: (_event: string, handler: unknown) => {
        w.bridgeOrder.push('subscribe')
        w.emitLocale = handler
      } },
    }
  })
  await expect(heading).toHaveText('常规设置')
  expect(await child.evaluate(() => (window as any).bridgeOrder)).toEqual(['subscribe', 'snapshot'])
  const language = frame.locator('.setting-row').first().locator('.t-select')
  await expect(language.locator('input')).toHaveValue('自动（浏览器／VOS）')
  await child.evaluate(() => (window as any).emitLocale({ locale: 'en-US' }))
  await expect(heading).toHaveText('General Settings')
  await expect(language.locator('input')).toHaveValue('Automatic (browser / VOS)')
  await language.click()
  await frame.getByText('日本語', { exact: true }).click()
  await expect(heading).toHaveText('一般設定')
  await child.evaluate(() => (window as any).emitLocale({ locale: 'zh-CN' }))
  await expect(heading).toHaveText('一般設定')
  await language.click()
  await frame.getByText('自動（ブラウザー／VOS）', { exact: true }).click()
  await expect(heading).toHaveText('常规设置')
  await expect(language.locator('input')).toHaveValue('自动（浏览器／VOS）')
  await expect(frame.locator('#draft')).toHaveValue('unsaved form')
  expect(await child.evaluate(() => ({
    sameInput: (window as any).originalDraft === document.querySelector('#draft'),
    locale: (window as any).testI18n.global.locale.value,
    manual: localStorage.getItem('locale'),
  }))).toEqual({ sameInput: true, locale: 'zh-CN', manual: null })
})
