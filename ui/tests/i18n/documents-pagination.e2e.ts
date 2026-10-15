import { test, expect, type Page } from '@playwright/test';

const timestamp = '2026-10-15T10:00:00Z';
const docs = Array.from({length:105}, (_, i) => ({id:`doc-${i}`,name:`${i < 75 ? 'Смета' : 'Отчёт'} ${String(i + 1).padStart(3,'0')}.pdf`,category_id:i < 80 ? 'cat-financials' : 'cat-legal',category_name:i < 80 ? 'Financials' : 'Legal',file_size:1024,mime_type:'application/pdf',current_version:1,created_at:timestamp,can_download:true}));

async function setup(page:Page) {
 let available=docs.slice();
 const requests: {path:string;method:string;limit:number;offset:number;query:string}[] = [];
 await page.addInitScript(() => {localStorage.setItem('dd_auth_token','test-session');localStorage.setItem('dd_locale','ru');});
 await page.route('**/api/v1/**',async route => {
  const req=route.request(); const url=new URL(req.url()); const path=url.pathname.replace('/api/v1','');
  let data:unknown=[]; let total=0;
  if(path==='/auth/me') data={id:'admin',email:'admin@example.com',name:'Admin',role:'admin',is_active:true};
  else if(path==='/categories') data=[{id:'cat-financials',name:'Financials',slug:'financials',document_count:80},{id:'cat-legal',name:'Legal',slug:'legal',document_count:25}];
  else if(req.method()==='DELETE'&&path.startsWith('/documents/')) available=available.filter(d=>path!==`/documents/${d.id}`);
  else if(path.endsWith('/download')) {
   await route.fulfill({body:'%PDF-1.4\nfixture',headers:{'Content-Type':'application/pdf','Content-Disposition':`attachment; filename*=UTF-8''${encodeURIComponent('Пояснение по категориям.pdf')}`}});return;
  } else if(path==='/documents'||path==='/documents/search') {
   const limit=Number(url.searchParams.get('limit')??50);const offset=Number(url.searchParams.get('offset')??0);
   const query=path.endsWith('/search') ? (req.postDataJSON() as {query:string}).query : '';
   requests.push({path,method:req.method(),limit,offset,query});
   const filtered=available.filter(d=>(!url.searchParams.get('category_id')||d.category_id===url.searchParams.get('category_id'))&&d.name.includes(query));
   total=filtered.length; data=filtered.slice(offset,offset+limit);
  }
  await route.fulfill({json:{success:true,data,meta:{total},timestamp}});
 });
 await page.goto('/documents');
 await expect(page.getByRole('table')).toBeVisible();
 return requests;
}

test('documents page sizes and previous/next boundaries',async({page})=>{
 await setup(page);
 const size=page.getByLabel('Документов на странице');
 await expect(size).toHaveValue('50');
 await expect(size.locator('option')).toHaveText(['10','20','30','50','100']);
 await expect(page.getByText('Страница 1 из 3',{exact:false})).toBeVisible();
 await expect(page.getByRole('button',{name:'Назад',exact:true})).toBeDisabled();
 await page.getByRole('button',{name:'Вперёд',exact:true}).click();
 await expect(page.getByText('Страница 2 из 3',{exact:false})).toBeVisible();
 await expect(page.getByRole('link',{name:docs[50].name,exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Вперёд',exact:true}).click();
 await expect(page.getByRole('row')).toHaveCount(6);
 await expect(page.getByRole('button',{name:'Вперёд',exact:true})).toBeDisabled();
 await page.getByRole('button',{name:'Назад',exact:true}).click();
 await expect(page.getByRole('row')).toHaveCount(51);
 for(const count of [10,20,30,100,50]) {
  await size.selectOption(String(count));
  await expect(page.getByRole('row')).toHaveCount(count+1);
  await expect(page.getByText(`Страница 1 из ${Math.ceil(105/count)}`,{exact:false})).toBeVisible();
 }
});

test('search and category filters keep pagination coherent',async({page})=>{
 const requests=await setup(page);
 await page.getByRole('button',{name:'Вперёд',exact:true}).click();
 await expect(page.getByText('Страница 2 из 3',{exact:false})).toBeVisible();
 await page.getByPlaceholder('Поиск документов…').fill('Смета');
 await page.getByRole('button',{name:'Поиск',exact:true}).click();
 await expect(page.getByText('Страница 1 из 2',{exact:false})).toBeVisible();
 await page.getByRole('button',{name:'Вперёд',exact:true}).click();
 await expect(page.getByRole('row')).toHaveCount(26);
 expect(requests.at(-1)).toMatchObject({path:'/documents/search',method:'POST',limit:50,offset:50,query:'Смета'});
 await page.getByLabel('Документов на странице').selectOption('20');
 await expect(page.getByText('Страница 1 из 4',{exact:false})).toBeVisible();
 expect(requests.at(-1)).toMatchObject({path:'/documents/search',limit:20,offset:0,query:'Смета'});
 await page.getByLabel('Категория',{exact:true}).selectOption('cat-legal');
 await expect(page.getByText('Страница 1 из 2',{exact:false})).toBeVisible();
 await expect(page.getByRole('row')).toHaveCount(21);
 expect(requests.at(-1)).toMatchObject({path:'/documents',limit:20,offset:0});
 await page.getByPlaceholder('Поиск документов…').fill('Ничего не найдено');
 await page.getByRole('button',{name:'Поиск',exact:true}).click();
 await expect(page.getByText('Страница 1 из 1',{exact:false})).toBeVisible();
 await expect(page.getByRole('button',{name:'Вперёд',exact:true})).toBeDisabled();
});

test('browser download keeps a Cyrillic filename',async({page})=>{
 await setup(page);
 const downloaded=page.waitForEvent('download');
 await page.getByRole('button',{name:'Скачать',exact:true}).first().click();
 expect((await downloaded).suggestedFilename()).toBe('Пояснение по категориям.pdf');
});

test('failed page requests keep the page number and retry the same offset',async({page})=>{
 const requests=await setup(page);
 let fail=true;
 await page.route('**/api/v1/documents?*',async route=>{
  if(new URL(route.request().url()).searchParams.get('offset')==='50'&&fail) {
   fail=false;await route.fulfill({status:503,json:{success:false,error:'temporarily unavailable'}});
  } else await route.fallback();
 });
 await page.getByRole('button',{name:'Вперёд',exact:true}).click();
 await expect(page.getByText('Не удалось загрузить документы.',{exact:false})).toBeVisible();
 await expect(page.getByText('Страница 2 из 3',{exact:false})).toBeVisible();
 await page.getByRole('button',{name:'Повторить',exact:true}).click();
 await expect(page.getByRole('row')).toHaveCount(51);
 expect(requests.at(-1)).toMatchObject({limit:50,offset:50});
});

test('older filter responses cannot overwrite the selected category',async({page})=>{
 await setup(page);
 let release:()=>void=()=>undefined;
 let started:()=>void=()=>undefined;
 const gate=new Promise<void>(resolve=>{release=resolve;});
 const entered=new Promise<void>(resolve=>{started=resolve;});
 await page.route('**/api/v1/documents?*',async route=>{
  if(new URL(route.request().url()).searchParams.get('category_id')==='cat-financials') {started();await gate;}
  await route.fallback();
 });
 await page.getByLabel('Категория',{exact:true}).selectOption('cat-financials');
 await entered;
 await page.getByLabel('Категория',{exact:true}).selectOption('cat-legal');
 await expect(page.getByRole('row')).toHaveCount(26);
 const response=page.waitForResponse(r=>r.url().includes('category_id=cat-financials'));
 release();await response;
 await expect(page.getByRole('row')).toHaveCount(26);
 await expect(page.getByRole('link',{name:docs[80].name,exact:true})).toBeVisible();
});

test('deleting the final row returns to the last remaining page',async({page})=>{
 await setup(page);
 page.on('dialog',dialog=>dialog.accept());
 for(let i=0;i<2;i++) {
  await page.getByRole('button',{name:'Вперёд',exact:true}).click();
  await expect(page.getByText(`Страница ${i+2} из 3`,{exact:false})).toBeVisible();
 }
 for(let i=0;i<5;i++) {
  await page.getByRole('button',{name:'Удалить документ',exact:true}).first().click();
  await expect(page.getByRole('row')).toHaveCount(i===4 ? 51 : 5-i);
 }
 await expect(page.getByText('Страница 2 из 2',{exact:false})).toBeVisible();
 await expect(page.getByRole('button',{name:'Вперёд',exact:true})).toBeDisabled();
});
