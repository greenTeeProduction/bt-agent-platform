const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  try {
    await page.goto('http://localhost:9800');
    await page.waitForSelector('h1', { timeout: 10000 });
    const title = await page.textContent('h1');
    console.log('SUCCESS: Dashboard loaded with title:', title);
  } catch (error) {
    console.log('FAILED:', error.message);
  }
  await browser.close();
})();
