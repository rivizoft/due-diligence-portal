import { defineConfig, devices } from '@playwright/test';

// Isolated UI coverage: API responses are intercepted; no cloud keys or DB mutation.
export default defineConfig({
	testDir: './tests/e2e',
	testMatch: '**/08-storage.e2e.ts',
	fullyParallel: true,
	workers: 2,
	timeout: 30_000,
	reporter: 'list',
	use: {
		baseURL: 'http://127.0.0.1:5176',
		...devices['Desktop Chrome'],
		launchOptions: {
			executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,
			args: ['--no-sandbox', '--disable-dev-shm-usage']
		}
	},
	webServer: {
		command: 'npm run dev -- --host 127.0.0.1 --port 5176 --strictPort',
		url: 'http://127.0.0.1:5176',
		reuseExistingServer: !process.env.CI,
		timeout: 60_000
	}
});
