import { test, expect } from '@playwright/test';

// A valid PDF with a standard font exercises both the worker and local font assets.
function pdfBytes(): Buffer {
 const objects = [
  '<< /Type /Catalog /Pages 2 0 R >>',
  '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
  '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
  '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
 ];
 const stream = 'BT /F1 18 Tf 30 100 Td (PDF preview test) Tj ET';
 objects.push(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`);
 let text = '%PDF-1.4\n';
 const offsets = [0];
 for (let i = 0; i < objects.length; i++) {
  offsets.push(Buffer.byteLength(text));
  text += `${i + 1} 0 obj\n${objects[i]}\nendobj\n`;
 }
 const xref = Buffer.byteLength(text);
 text += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
 text += offsets.slice(1).map(offset => `${String(offset).padStart(10, '0')} 00000 n \n`).join('');
 text += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
 return Buffer.from(text);
}

test('document lifecycle, PDF preview and investor View Only', async ({ page, request, browser, baseURL }) => {
 const suffix = `${Date.now()}`;
 const login = await request.post('/api/v1/auth/login', { data: {
  email: process.env.DD_TEST_ADMIN_EMAIL || 'admin@localhost',
  password: process.env.DD_TEST_ADMIN_PASSWORD || 'testpass123'
 } });
 expect(login.ok()).toBeTruthy();
 const admin = (await login.json()).data.access_token;
 const headers = { Authorization: `Bearer ${admin}` };
 const categoryResponse = await request.post('/api/v1/categories', { headers, data: { name: `Lifecycle ${suffix}`, slug: `lifecycle-${suffix}` } });
 expect(categoryResponse.ok()).toBeTruthy();
 const category = (await categoryResponse.json()).data;
 // Install before page scripts run so the landing redirect/guard cannot race
 // token injection while navigating to the protected upload route.
 await page.addInitScript(token => localStorage.setItem('dd_auth_token', token), admin);
 const errors: string[] = [];
 page.on('pageerror', err => errors.push(err.message));
 page.on('dialog', dialog => dialog.accept());
 await page.goto('/documents/upload');
 await page.getByLabel('File', { exact: true }).setInputFiles({ name: 'preview.pdf', mimeType: 'application/pdf', buffer: pdfBytes() });
 await page.getByLabel('Name', { exact: true }).fill(`PDF ${suffix}`);
 await page.getByLabel('Category', { exact: true }).selectOption(category.id);
 await page.getByRole('button', { name: 'Upload', exact: true }).click();
 await expect(page).toHaveURL(/\/documents$/);
 await page.getByRole('link', { name: `PDF ${suffix}`, exact: true }).click();
 await expect(page).toHaveURL(/\/documents\/[a-f0-9]+$/);
 const documentPath = new URL(page.url()).pathname;
 const staffTabPromise = page.context().waitForEvent('page');
 await page.getByRole('link', { name: 'Open PDF', exact: true }).click();
 const staffPreview = await staffTabPromise;
 await expect(staffPreview.locator('iframe[title="PDF preview"]')).toBeVisible();
 await staffPreview.close();
 await page.getByLabel('Updated file').setInputFiles({ name: 'updated.pdf', mimeType: 'application/pdf', buffer: pdfBytes() });
 await page.getByLabel('Change note').fill('Second version');
 await page.getByRole('button', { name: 'Upload new version', exact: true }).click();
 await expect(page.locator('.meta')).toContainText('v2');
 await expect(page.locator('tbody tr')).toHaveCount(2);
 const invite = await request.post('/api/v1/users/invite', { headers, data: { email: `investor-${suffix}@example.test`, role: 'investor' } });
 expect(invite.ok()).toBeTruthy();
 const registration = await request.post('/api/v1/auth/register', { data: { token: (await invite.json()).data.token, name: 'View-only investor', password: 'investor-test-password' } });
 expect(registration.ok()).toBeTruthy();
 const investor = (await registration.json()).data;
 const userID = investor.user.id;
 expect((await request.post('/api/v1/nda/exemptions', { headers, multipart: { user_id: userID, reason: 'Isolated browser test' } })).ok()).toBeTruthy();
 expect((await request.post('/api/v1/permissions', { headers, data: { user_id: userID, resource_type: 'document', resource_id: documentPath.split('/').pop(), access_level: 'view' } })).ok()).toBeTruthy();
 const context = await browser.newContext();
 await context.addInitScript(token => localStorage.setItem('dd_auth_token', token), investor.access_token);
 const viewPage = await context.newPage();
 viewPage.on('pageerror', err => errors.push(err.message));
 await viewPage.goto(`${baseURL}${documentPath}`);
 const viewOnlyPDFResponses: string[] = [];
 context.on('response', response => {
  if (response.url().includes('/api/v1/documents/') && response.headers()['content-type']?.startsWith('application/pdf')) viewOnlyPDFResponses.push(response.url());
 });
 const viewTabPromise = context.waitForEvent('page');
 await viewPage.getByRole('link', { name: 'Open PDF', exact: true }).click();
 const viewTab = await viewTabPromise;
 await expect(viewTab.getByAltText('Page 1')).toBeVisible();
 await expect(viewTab.locator('iframe')).toHaveCount(0);
 expect(viewOnlyPDFResponses).toEqual([]);
 await expect(viewPage.getByRole('button', { name: 'Download', exact: true })).toHaveCount(0);
 await expect(viewPage.getByRole('button', { name: /Delete/ })).toHaveCount(0);
 await expect(viewPage.getByRole('button', { name: 'Upload new version', exact: true })).toHaveCount(0);
 await viewPage.getByRole('button', { name: 'View', exact: true }).last().click();
 await expect(viewPage.getByRole('heading', { name: 'PDF preview — v1' })).toBeVisible();
 await viewPage.goto(`${baseURL}/documents`);
 await expect(viewPage.getByRole('link', { name: `PDF ${suffix}`, exact: true })).toBeVisible();
 const viewOnlyRow = viewPage.locator('tbody tr').filter({ hasText: `PDF ${suffix}` });
 await expect(viewOnlyRow.getByRole('button', { name: 'Download', exact: true })).toHaveCount(0);
 await expect(viewPage.getByRole('button', { name: /Delete/ })).toHaveCount(0);
 await expect(viewPage.getByRole('link', { name: 'Upload Document' })).toHaveCount(0);
 const invHeaders = { Authorization: `Bearer ${investor.access_token}` };
 expect((await request.get(`/api/v1${documentPath}/download`, { headers: invHeaders })).status()).toBe(404);
 expect((await request.get(`/api/v1${documentPath}/preview`, { headers: invHeaders })).status()).toBe(404);
 expect((await request.delete(`/api/v1${documentPath}`, { headers: invHeaders })).status()).toBe(403);
 expect((await request.delete(`/api/v1${documentPath}/versions/2`, { headers: invHeaders })).status()).toBe(403);
 expect((await request.delete(`/api/v1/categories/${category.id}`, { headers: invHeaders })).status()).toBe(403);
 await context.close();
 await page.locator('tbody tr').first().getByRole('button', { name: 'Delete version' }).click();
 await expect(page.locator('.meta')).toContainText('v1');
 await expect(page.locator('tbody tr')).toHaveCount(1);
 await expect(page.getByRole('button', { name: 'Delete version' })).toBeDisabled();
 await page.getByRole('button', { name: 'Delete document' }).click();
 await expect(page).toHaveURL(/\/documents$/);
 await expect(page.getByRole('link', { name: `PDF ${suffix}`, exact: true })).toHaveCount(0);
 await page.goto('/admin/categories');
 const row = page.locator('tbody tr').filter({ hasText: category.name });
 await expect(row.getByRole('button', { name: 'Delete category' })).toBeEnabled();
 await row.getByRole('button', { name: 'Delete category' }).click();
 await expect(page.locator('tbody tr').filter({ hasText: category.name })).toHaveCount(0);
 expect(errors).toEqual([]);
});
