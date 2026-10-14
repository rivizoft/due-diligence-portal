import { test, expect, type Page } from '@playwright/test';

const defaults = {
	enabled: false, bucket: '', region: '', endpoint: '', path_style: false,
	key_prefix: '', credentials_configured: false, masked_access_key_id: '', location_locked: false
};
async function mockStorage(page: Page, initial = defaults) {
	let settings = { ...initial };
	const requests: { method: string; path: string; body: Record<string, unknown> }[] = [];
	await page.addInitScript(() => localStorage.setItem('dd_auth_token', 'storage-ui-test'));
	await page.route('**/api/v1/**', async (route) => {
		const req = route.request();
		const path = new URL(req.url()).pathname.replace('/api/v1', '');
		let data: unknown = {};
		if (path === '/auth/me') data = { user_id: 'admin', email: 'admin@test', name: 'Admin', role: 'admin' };
		if (path === '/storage') {
			if (req.method() === 'PUT') {
				const body = req.postDataJSON();
				requests.push({ method: req.method(), path, body });
				settings = { ...settings, ...body, credentials_configured: true, masked_access_key_id: '••••1234' };
				delete (settings as Record<string, unknown>).secret_access_key;
				delete (settings as Record<string, unknown>).access_key_id;
			}
			data = settings;
		}
		if (path === '/storage/test') requests.push({ method: req.method(), path, body: req.postDataJSON() });
		await route.fulfill({ json: { success: true, data, message: path === '/storage/test' ? 'Connection verified: upload, read and delete succeeded' : 'OK' } });
	});
	return requests;
}

test('storage page includes navigation and short connection instructions', async ({ page }) => {
	await mockStorage(page);
	await page.goto('/admin/storage');
	await expect(page.getByRole('heading', { name: 'Storage Settings' })).toBeVisible();
	await expect(page.locator('.admin-nav a[href="/admin/storage"]')).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Connect S3 in four steps' })).toBeVisible();
	await expect(page.getByText('GetObject, PutObject and DeleteObject', { exact: false })).toBeVisible();
	await expect(page.getByLabel('Secret Access Key', { exact: true })).toHaveAttribute('type', 'password');
});

test('tests unsaved fields and saves keys without retaining the secret', async ({ page }) => {
	const requests = await mockStorage(page);
	await page.goto('/admin/storage');
	await page.getByLabel('Bucket', { exact: true }).fill('private-bucket');
	await page.getByLabel('Region', { exact: true }).fill('eu-central-1');
	await page.getByLabel('Access Key ID', { exact: true }).fill('test1234');
	await page.getByLabel('Secret Access Key', { exact: true }).fill('test-secret');
	await page.getByLabel('Key prefix', { exact: true }).fill('draft/');
	await page.getByRole('button', { name: 'Test Connection', exact: true }).click();
	await expect(page.getByRole('status')).toContainText('Connection verified');
	expect(requests).toHaveLength(1);
	expect(requests[0].body).toMatchObject({ bucket: 'private-bucket', key_prefix: 'draft/', secret_access_key: 'test-secret', enabled: false });
	await page.getByLabel('Use S3 for new uploads').check();
	await page.getByRole('button', { name: 'Save Settings', exact: true }).click();
	await expect(page.getByRole('status')).toContainText('saved');
	await expect(page.getByLabel('Secret Access Key', { exact: true })).toHaveValue('');
	await expect(page.getByLabel('Access Key ID', { exact: true })).toHaveValue('');
	expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('test-secret');
	await page.reload();
	await expect(page.getByLabel('Bucket', { exact: true })).toHaveValue('private-bucket');
	await expect(page.getByLabel('Secret Access Key', { exact: true })).toHaveValue('');
});

test('saved credentials remain empty and location locking still allows disable', async ({ page }) => {
	const requests = await mockStorage(page, { ...defaults, enabled: true, bucket: 'private-bucket', region: 'eu-central-1', credentials_configured: true, masked_access_key_id: '••••1234', location_locked: true });
	await page.goto('/admin/storage');
	await expect(page.getByLabel('Bucket', { exact: true })).toBeDisabled();
	await expect(page.getByLabel('Secret Access Key', { exact: true })).toHaveValue('');
	await page.getByLabel('Use S3 for new uploads').uncheck();
	await page.getByRole('button', { name: 'Save Settings', exact: true }).click();
	await expect(page.getByRole('status')).toContainText('saved');
	expect(requests[0].body).toMatchObject({ enabled: false, access_key_id: '', secret_access_key: '', bucket: 'private-bucket' });
});

test('shows connection failures and retains entered fields for correction', async ({ page }) => {
	await mockStorage(page, { ...defaults, bucket: 'private-bucket', region: 'eu-central-1', credentials_configured: true });
	await page.route('**/api/v1/storage/test', (route) => route.fulfill({ status: 503, json: { success: false, error: 'S3 test read failed. Check object permissions.' } }));
	await page.goto('/admin/storage');
	await page.getByLabel('Key prefix', { exact: true }).fill('draft/');
	await page.getByRole('button', { name: 'Test Connection', exact: true }).click();
	await expect(page.getByRole('alert')).toContainText('S3 test read failed');
	await expect(page.getByLabel('Key prefix', { exact: true })).toHaveValue('draft/');
});
