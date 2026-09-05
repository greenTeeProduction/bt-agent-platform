const { test, expect } = require('@playwright/test');

test.describe('DoorMate Page-First AI Assistant Tab E2E Tests', () => {
  test('verify tab switches, interacts, types intent, renders blocks, and toggles bookmarks', async ({ page }) => {
    // Listen for console logs/errors
    page.on('console', msg => {
      console.log(`BROWSER CONSOLE [${msg.type()}]: ${msg.text()}`);
    });

    // Listen for page errors
    page.on('pageerror', err => {
      console.error(`BROWSER ERROR: ${err.message}`);
    });

    // Navigate to the dashboard
    await page.goto('http://localhost:9800');

    // 1. Verify DoorMate Tab Exists and Click It
    const doormateTab = page.locator('button[data-tab="doormate"]');
    await expect(doormateTab).toBeVisible();
    await doormateTab.click();

    // Wait 500ms for initDoormateTab to complete its setTimeout(..., 100) and register all listeners
    await page.waitForTimeout(500);

    // Verify Canvas layout is now visible
    const canvasBox = page.locator('.intent-canvas-box');
    await expect(canvasBox).toBeVisible();
    
    // Verify default workspace empty state exists
    const emptyState = page.locator('.workspace-empty-state');
    await expect(emptyState).toBeVisible();

    // 2. Test Interactive Click Actions (AV Indicators)
    const micBtn = page.locator('#doormate-mic-btn');
    await expect(micBtn).toBeVisible();
    await micBtn.click();
    await expect(page.locator('#mic-status')).toHaveText('Listening...');

    const camBtn = page.locator('#doormate-cam-btn');
    await expect(camBtn).toBeVisible();
    await camBtn.click();
    await expect(page.locator('#cam-status')).toHaveText('Calibrating...');

    // 3. Test Text Typing & Submission Flow
    const inputEl = page.locator('#doormate-input');
    await expect(inputEl).toBeVisible();
    await inputEl.fill('advanced biometric door lock security');
    
    const sendBtn = page.locator('#doormate-send-btn');
    await expect(sendBtn).toBeVisible();
    await sendBtn.click();

    // 4. Verify Generation & Rendering of Page Blocks
    // This should change from empty state to the generated-page-container
    const pageContainer = page.locator('.generated-page-container');
    await expect(pageContainer).toBeVisible({ timeout: 5000 });

    // Verify the page title and summary
    const pageTitle = page.locator('.title-section h2');
    await expect(pageTitle).toHaveText('Smart Lock & Door Security Blueprint');

    // Verify page blocks are rendered
    await expect(page.locator('.block-overview')).toBeVisible();
    await expect(page.locator('.block-cards')).toBeVisible();
    await expect(page.locator('.block-comparison')).toBeVisible();
    await expect(page.locator('.block-chart')).toBeVisible();
    await expect(page.locator('.block-diagram')).toBeVisible();

    // Verify predicted bubbles are now populated on the left
    const predictedBubbles = page.locator('.predicted-bubble-btn');
    await expect(predictedBubbles).toHaveCount(4);
    await expect(predictedBubbles.first()).toHaveText('Biometric Lock');

    // 5. Test Bookmark Toggling
    const bookmarkBtn = page.locator('#btn-bookmark');
    await expect(bookmarkBtn).toBeVisible();
    // Initially not bookmarked
    await expect(bookmarkBtn).not.toHaveClass(/active/);
    
    // Click bookmark -> expect active state
    await bookmarkBtn.click();
    await expect(bookmarkBtn).toHaveClass(/active/);
    await expect(bookmarkBtn).toHaveText(/★ Bookmarked/);

    // Click bookmark again -> expect inactive state
    await bookmarkBtn.click();
    await expect(bookmarkBtn).not.toHaveClass(/active/);
    await expect(bookmarkBtn).toHaveText(/☆ Bookmark/);

    // 6. Test Star Rating System
    const star4 = page.locator('.star-rating-icon').nth(3); // 4th star
    await expect(star4).toBeVisible();
    await star4.click();
    await expect(star4).toHaveClass(/active/);

    // Take screenshot for visual verification
    await page.screenshot({ path: '.playwright-mcp/doormate_snapshot.png' });
  });
});
