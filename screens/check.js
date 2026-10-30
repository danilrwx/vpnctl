// Рендерит все макеты в headless Chromium (Playwright), снимает PNG и ищет переполнения:
// элементы, вылезающие за рамку 1120×720 или за родителя без overflow: hidden.
// Запуск: node screens/check.js [папка-с-макетами] [папка-для-png]
const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const dir = path.resolve(process.argv[2] || __dirname);
const out = path.resolve(process.argv[3] || path.join(__dirname, 'shots'));
fs.mkdirSync(out, { recursive: true });

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1120, height: 720 } });
  const files = fs.readdirSync(dir).filter((f) => f.endsWith('.html'));
  let failed = 0;
  for (const f of files) {
    await page.goto('file://' + path.join(dir, f), { waitUntil: 'networkidle' });
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(200);
    const issues = await page.evaluate(() => {
      const root = document.body.firstElementChild;
      const rr = root.getBoundingClientRect();
      const res = [];
      for (const el of root.querySelectorAll('*')) {
        const r = el.getBoundingClientRect();
        if (!r.width || !r.height) continue;
        if (r.right > rr.right + 1 || r.bottom > rr.bottom + 1) {
          res.push(`вне рамки: «${(el.textContent || '').trim().slice(0, 40)}»`);
          continue;
        }
        const p = el.parentElement;
        if (!p || p === document.body) continue;
        const pr = p.getBoundingClientRect();
        if (getComputedStyle(p).overflow === 'visible' && (r.bottom > pr.bottom + 1 || r.right > pr.right + 1)) {
          res.push(`вылезает из родителя: «${(el.textContent || '').trim().slice(0, 40)}»`);
        }
      }
      return res.slice(0, 10);
    });
    await page.screenshot({ path: path.join(out, f.replace('.html', '.png')) });
    if (issues.length) {
      failed++;
      console.log(`✗ ${f}`);
      for (const i of issues) console.log('   ' + i);
    } else {
      console.log(`✓ ${f}`);
    }
  }
  await browser.close();
  process.exit(failed ? 1 : 0);
})();
