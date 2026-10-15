import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
 testDir: './tests/i18n',
 testMatch: '**/*.e2e.ts',
 fullyParallel: true,
 workers: 2,
 timeout: 30_000,
 reporter: 'list',
 use: {
  ...devices['Desktop Chrome'],
  baseURL:'http://127.0.0.1:4174',
  locale:'ru-RU',
  launchOptions: process.env.CHROMIUM_PATH ? {executablePath:process.env.CHROMIUM_PATH} : {},
  screenshot:'only-on-failure'
 },
 webServer:{command:'npm run dev -- --host 127.0.0.1 --port 4174',url:'http://127.0.0.1:4174/login',reuseExistingServer:!process.env.CI}
});
