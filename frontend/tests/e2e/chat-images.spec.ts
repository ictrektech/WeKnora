import { expect, test } from '@playwright/test'
import { compileStyleAsync } from '@vue/compiler-sfc'
import { fileURLToPath } from 'node:url'

test('answers show decoded images and hide failed images without losing text or resource retries', async ({ page }) => {
  const style = await compileStyleAsync({
    filename: fileURLToPath(new URL('../../src/components/css/ChatImageFixture.vue', import.meta.url)),
    source: '@import "./chat-markdown.less"; .markdown-content { .chat-markdown-typography(); }',
    id: 'data-v-fixture', scoped: true, preprocessLang: 'less',
  })
  expect(style.errors).toEqual([])
  await page.route('**/chat-image-test', route => route.fulfill({
    contentType: 'text/html', body: `<style>${style.code}</style>
      <div id="root" class="markdown-content" data-v-fixture></div><script type="module">
        import { sanitizeMarkdownHTML, hydrateProtectedFileImages, clearProtectedFileFailureCache } from '/src/utils/security.ts';
        import { vStableHtml } from '/src/directives/stableHtml.ts';
        const root = document.querySelector('#root'); let previous;
        window.render = async (html, access) => {
          const value = sanitizeMarkdownHTML(html);
          if (previous === undefined) vStableHtml.beforeMount(root, { value });
          else vStableHtml.updated(root, { value, oldValue: previous });
          previous = value;
          await hydrateProtectedFileImages(root, access);
        };
        window.retry = async access => { clearProtectedFileFailureCache(); await hydrateProtectedFileImages(root, access); };
      </script>`,
  }))
  // The invalid Wikimedia link from the reported conversation, without network dependency.
  const broken = 'https://upload.wikimedia.org/wikipedia/commons/thumb/8/85/Flag_of_PRC.svg/1200px-Flag_of_PRC.svg.png'
  await page.route(broken, route => route.fulfill({ status: 400, contentType: 'text/html', body: 'Invalid thumbnail' }))
  await page.route('**/missing.png', route => route.fulfill({ status: 404, body: '' }))
  await page.route('**/error-page.png', route => route.fulfill({ contentType: 'text/html', body: '<h1>Error</h1>' }))
  await page.route('**/corrupt.png', route => route.fulfill({ contentType: 'image/png', body: 'not an image' }))
  await page.goto('/chat-image-test')
  await page.waitForFunction(() => typeof (window as any).render === 'function')
  const good = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = canvas.height = 8;
    return canvas.toDataURL('image/png')
  })
  const html = `<p>五星红旗。</p><p data-case="broken"><img src="${broken}" alt="中华人民共和国国旗"></p>
    <p data-case="mixed">保留说明<img src="/error-page.png"></p><p data-case="missing"><img src="/missing.png"></p>
    <p data-case="corrupt"><img src="/corrupt.png"></p><p data-case="good"><img src="${good}"></p>`
  await page.evaluate(html => (window as any).render(html), html)
  await expect(page.locator('[data-case="good"] img')).toBeVisible()
  for (const id of ['broken', 'mixed', 'missing', 'corrupt']) {
    await expect(page.locator(`[data-case="${id}"] img`)).toHaveAttribute('data-chat-image-state', 'failed')
    await expect(page.locator(`[data-case="${id}"] img`)).toBeHidden()
  }
  await expect(page.locator('[data-case="broken"]')).toBeHidden()
  await expect(page.locator('[data-case="mixed"]')).toBeVisible()
  await expect(page.locator('[data-case="mixed"]')).toHaveText('保留说明')
  await page.evaluate(html => (window as any).render(html + '<p>继续回答。</p>'), html)
  await expect(page.locator('[data-case="broken"]')).toBeHidden()
  await expect(page.locator('[data-case="good"] img')).toBeVisible()
  await page.evaluate(html => (window as any).render(html), html.replace(broken, good))
  await expect(page.locator('[data-case="broken"] img')).toBeVisible()

  let releaseImage!: () => void
  const imageReady = new Promise<void>(resolve => { releaseImage = resolve })
  await page.route('**/pending.png', async route => {
    await imageReady
    await route.fulfill({ contentType: 'image/png', body: Buffer.from(good.split(',')[1], 'base64') })
  })
  await page.evaluate(() => (window as any).render('<p data-case="pending"><img src="/pending.png"></p>'))
  await expect(page.locator('[data-case="pending"] img')).toHaveAttribute('data-chat-image-state', 'pending')
  await expect(page.locator('[data-case="pending"]')).toBeHidden()
  releaseImage()
  await expect(page.locator('[data-case="pending"] img')).toBeVisible()

  // A missing protected resource can recover on the same node after completion.
  let resourceReady = false
  let office = false
  await page.route('**/api/v1/sessions/*/messages/*/files?*', route => route.fulfill(resourceReady
    ? { contentType: office ? 'application/pdf' : 'image/png', body: office ? Buffer.from('%PDF') : Buffer.from(good.split(',')[1], 'base64') }
    : { status: 404, body: '' }))
  const access = { mode: 'message', sessionId: 'session', messageId: 'assistant' }
  await page.evaluate(access => (window as any).render('<p data-case="resource"><img src="local://1/image.png"></p>', access), access)
  await expect(page.locator('[data-case="resource"]')).toBeHidden()
  resourceReady = true
  await page.evaluate(access => (window as any).retry(access), access)
  await expect(page.locator('[data-case="resource"] img')).toBeVisible()
  office = true
  await page.evaluate(access => (window as any).render('<p data-case="file"><img src="local://1/document.pdf"></p>', access), access)
  await expect(page.locator('[data-case="file"] .protected-resource-card')).toBeVisible()
})
