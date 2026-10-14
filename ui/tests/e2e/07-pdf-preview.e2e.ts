import { test, expect, type BrowserContext } from '@playwright/test';
import fs from 'node:fs';
const pdf=fs.readFileSync('../internal/service/pdf/testdata/sample.pdf');
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a6k0AAAAASUVORK5CYII=','base64');
async function mock(context:BrowserContext,mode:'pdf'|'images') {
 await context.addInitScript(()=>{localStorage.setItem('dd_auth_token','test-token');});
 const calls:string[]=[];
 await context.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;calls.push(path);
  const data=(value:unknown)=>route.fulfill({json:{success:true,data:value}});
  if(path.endsWith('/auth/me'))return data({user_id:'u',name:'Investor',email:'u@test',role:'investor'});
  if(path.endsWith('/nda/status'))return data({signed:true});
  if(path.endsWith('/preview-info'))return data({mode,version_number:1,name:'Report.pdf',pages:mode==='images'?Array.from({length:10},()=>({width:595,height:842})):[]});
  if(path.includes('/preview-pages/'))return route.fulfill({contentType:'image/png',body:png});
  if(path.endsWith('/preview'))return route.fulfill({contentType:'application/pdf',body:pdf});
  if(path.endsWith('/categories'))return data([]);
  if(path.endsWith('/documents'))return route.fulfill({json:{success:true,data:[{id:'d',name:'Report.pdf',mime_type:'application/pdf',current_version:1,can_download:mode==='pdf',file_size:100,created_at:'2026-10-14'}],meta:{total:1}}});
  return data({});
 });return calls;
}
test('view-only new tab receives only page images and loads lazily',async({page,context})=>{
 const calls=await mock(context,'images');await page.goto('/documents');await expect(page.getByRole('button',{name:'Download',exact:true})).toHaveCount(0);
 const opened=context.waitForEvent('page');await page.getByRole('link',{name:'Open PDF'}).click();const tab=await opened;await expect(tab.getByAltText('Page 1')).toBeVisible();await expect(tab.locator('iframe')).toHaveCount(0);await expect(tab.getByRole('button',{name:/Download|Print/})).toHaveCount(0);
 expect(calls.filter(p=>p.endsWith('/preview')||p.endsWith('/download'))).toEqual([]);expect(calls.filter(p=>p.includes('/preview-pages/')).length).toBeLessThan(5);await tab.getByRole('button',{name:'Next page'}).click();await expect(tab.getByAltText('Page 2')).toBeVisible();
});
test('download permission enables browser PDF viewer',async({page,context})=>{
 await mock(context,'pdf');await page.goto('/documents/d/preview');await expect(page.locator('iframe[title="PDF preview"]')).toBeVisible();await expect(page.getByRole('button',{name:'Download PDF'})).toBeVisible();
});
test('preparation status then error and retry without PDF fallback',async({page,context})=>{
 const calls=await mock(context,'images');let count=0;let release:()=>void=()=>{};const gate=new Promise<void>(resolve=>{release=resolve});
 await context.route('**/documents/d/preview-info',async route=>{count++;if(count===1){await gate;await route.fulfill({status:504,json:{error:'PDF preparation timed out.'}})}else{await route.fulfill({json:{success:true,data:{mode:'images',version_number:1,name:'Report',pages:[{width:595,height:842}]}}})}});
 await page.goto('/documents/d/preview');await expect(page.getByText('Preparing PDF…')).toBeVisible();release();await expect(page.getByText('PDF preparation timed out.')).toBeVisible();await page.getByRole('button',{name:'Retry',exact:true}).click();await expect(page.getByAltText('Page 1')).toBeVisible();expect(calls.some(p=>p.endsWith('/preview'))).toBe(false);
});

test('download shows preparation until bytes arrive and allows retry',async({page,context})=>{
 await mock(context,'pdf');let attempt=0;let release:()=>void=()=>{};const gate=new Promise<void>(resolve=>{release=resolve});
 await context.route('**/documents/d/download',async route=>{attempt++;if(attempt===1){await gate;await route.fulfill({status:503,json:{error:'PDF preparation is busy.'}})}else{await route.fulfill({contentType:'application/pdf',body:pdf,headers:{'Content-Disposition':'attachment; filename="report.pdf"'}})}});
 await page.goto('/documents');await page.getByRole('button',{name:'Download',exact:true}).click();await expect(page.getByRole('button',{name:'Preparing…'})).toBeDisabled();release();await expect(page.getByRole('alert')).toContainText('PDF preparation is busy.');
 const downloaded=page.waitForEvent('download');await page.getByRole('button',{name:'Download',exact:true}).click();expect((await downloaded).suggestedFilename()).toBe('report.pdf');await expect(page.getByRole('button',{name:'Download',exact:true})).toBeEnabled();
});
test('a rejected view-only page never falls back to PDF and can retry',async({page,context})=>{
 const calls=await mock(context,'images');let count=0;await context.route('**/versions/1/preview-pages/1',async route=>{count++;if(count===1)await route.fulfill({status:422,json:{error:'Page cannot be prepared.'}});else await route.fulfill({contentType:'image/png',body:png})});
 await page.goto('/documents/d/preview');await expect(page.getByRole('alert')).toContainText('Page cannot be prepared.');await page.getByRole('button',{name:'Retry page 1'}).click();await expect(page.getByAltText('Page 1')).toBeVisible();expect(calls.some(p=>p.endsWith('/preview'))).toBe(false);
});
test('image URLs are released after scrolling away and leaving preview',async({page,context})=>{
 await mock(context,'images');await context.addInitScript(()=>{const original=URL.revokeObjectURL.bind(URL);(window as unknown as {revoked:number}).revoked=0;URL.revokeObjectURL=(url:string)=>{(window as unknown as {revoked:number}).revoked++;original(url)};});
 await page.goto('/documents/d/preview');await expect(page.getByAltText('Page 1')).toBeVisible();await page.locator('[data-page="6"]').scrollIntoViewIfNeeded();await expect(page.getByAltText('Page 6')).toBeVisible();expect(await page.evaluate(()=>(window as unknown as {revoked:number}).revoked)).toBeGreaterThan(0);
 await page.getByRole('link',{name:'Document details'}).click();await expect(page.locator('.image-preview')).toHaveCount(0);expect(await page.evaluate(()=>(window as unknown as {revoked:number}).revoked)).toBeGreaterThan(1);
});
