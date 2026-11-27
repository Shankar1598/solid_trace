// Prints whether a 100 ms CSS animation finishes, how many animation frames run
// in 1 s, and the media and visibility state, in Playwright's Chromium.
import { chromium } from '@playwright/test'

const browser = await chromium.launch({ headless: true, ignoreDefaultArgs: ['--disable-dev-shm-usage'] })
const page = await browser.newPage()
await page.setContent(`<style>@keyframes x { from { opacity: 1 } to { opacity: 0 } } #a { animation: x 100ms forwards }</style><div id="a">a</div>`)
const out = await page.evaluate(async () => {
  const el = document.getElementById('a')
  const anims = el.getAnimations()
  const finished = await Promise.race([
    Promise.all(anims.map(a => a.finished)).then(() => 'finished'),
    new Promise(r => setTimeout(() => r('not finished after 3 s'), 3000)),
  ])
  let frames = 0
  const t0 = performance.now()
  await new Promise(r => { const f = () => { frames++; performance.now() - t0 < 1000 ? requestAnimationFrame(f) : r() }; requestAnimationFrame(f) })
  return {
    animations: anims.length, finished, framesIn1s: frames,
    reducedMotion: matchMedia('(prefers-reduced-motion: reduce)').matches,
    visibility: document.visibilityState, cpus: navigator.hardwareConcurrency,
  }
})
console.log(JSON.stringify({ version: browser.version(), ...out }))
await browser.close()
