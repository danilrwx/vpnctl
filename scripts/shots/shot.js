// shot.js <in.html> <out.png> — PNG строго по размеру блока .w, 2x (SCALE=…)
// Нужны playwright-core и chromium headless shell: PLAYWRIGHT_CORE=<путь к модулю>, CHROME=<путь к бинарнику>
const pw = require(process.env.PLAYWRIGHT_CORE || 'playwright-core');
(async () => {
  const b = await pw.chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {});
  const p = await b.newPage({ viewport: { width: 1400, height: 900 }, deviceScaleFactor: Number(process.env.SCALE || 2) });
  await p.goto('file://' + require('path').resolve(process.argv[2]));
  await p.evaluate(() => document.fonts.ready); await p.waitForTimeout(200);
  await (await p.$('.w')).screenshot({ path: process.argv[3], omitBackground: true });
  await b.close();
})();
