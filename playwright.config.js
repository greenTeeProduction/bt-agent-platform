const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/*.js',
  use: {
    baseURL: 'http://localhost:9800',
    headless: true,
  },
});
