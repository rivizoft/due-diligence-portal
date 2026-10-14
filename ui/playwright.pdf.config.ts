import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
 testDir: './tests/e2e',testMatch:'07-pdf-preview.e2e.ts',timeout:30000,workers:1,retries:0,
 use:{...devices['Desktop Chrome'],baseURL:process.env.PDF_TEST_URL||'http://localhost:5173',headless:true,launchOptions:{executablePath:process.env.PDF_BROWSER_PATH}},
 reporter:[['list']]
});
