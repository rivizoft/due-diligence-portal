import { test, expect, type Page } from '@playwright/test';

const timestamp = '2026-04-03T10:30:00Z';
const category = {id:'cat-financials',name:'Financials',slug:'financials',description:'Financial statements, projections, cap table, tax returns',document_count:1,sort_order:2};
const doc = {id:'example',name:'Investor report.pdf',category_id:category.id,category_name:category.name,file_size:1536,mime_type:'application/pdf',current_version:1,created_at:timestamp,can_download:true};
const user = {id:'admin',user_id:'admin',name:'Admin Example',email:'admin@example.com',role:'admin',is_active:true,created_at:timestamp,last_login_at:timestamp};

async function authenticate(page: Page) {
 await page.addInitScript(() => localStorage.setItem('dd_auth_token','test-session'));
 await page.route('**/api/v1/**', async route => {
  const path = new URL(route.request().url()).pathname.replace('/api/v1','');
  let data: unknown = [];
  if(path === '/auth/me') data=user;
  else if(path === '/categories') data=[category];
  else if(path === '/documents') data=[doc];
  else if(path === '/documents/example') data={document:doc,versions:[{...doc,version_number:1}],permissions:{can_download:true,can_manage:true}};
  else if(path === '/users') data=[user];
  else if(path === '/nda/status') data={signed:false};
  else if(path === '/nda/active') data={id:'nda',name:'Standard NDA',content:'Original legal text',version:1};
  else if(path === '/storage') data={enabled:false,location_locked:false,credentials_configured:false};
  else if(path === '/watermark') data={enabled:true,text_template:'{{user_name}} - {{document_name}}',opacity:0.3,font_size:24,color:'#808080',position:'diagonal'};
  else if(path === '/branding') data={};
  else if(path === '/qa') data=[{id:'thread',subject:'My original question',status:'open',message_count:2,created_at:timestamp,asked_by_name:'Alice'}];
  else if(path === '/qa/thread') data={thread:{id:'thread',subject:'My original question',status:'open'},messages:[]};
  else if(path === '/analytics/dashboard') data={total_documents:1000,total_views:1200,unique_viewers:30,active_investors:5,total_downloads:60,open_questions:2,recent_view_count:30,pending_ndas:0};
  else if(path === '/audit') data=[{id:'entry',action:'user.password_set',details:'changed own password',user_email:user.email,created_at:timestamp}];
  await route.fulfill({json:{success:true,data,meta:{total:1,count:1},timestamp}});
 });
}

test('browser default, live switch, input preservation and persistent preference', async ({page}) => {
 await page.goto('/login');
 await expect(page.getByRole('heading',{name:'Войти',exact:true})).toBeVisible();
 await expect(page.locator('html')).toHaveAttribute('lang','ru');
 await page.getByLabel('Электронная почта',{exact:true}).fill('alice@example.com');
 await page.getByRole('button',{name:'English',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Sign In',exact:true})).toBeVisible();
 await expect(page.getByLabel('Email',{exact:true})).toHaveValue('alice@example.com');
 await page.reload();
 await expect(page.getByRole('heading',{name:'Sign In',exact:true})).toBeVisible();
 await expect(page.locator('html')).toHaveAttribute('lang','en');
 await page.getByRole('button',{name:'Русский',exact:true}).click();
 await page.goto('/register');
 await expect(page.getByRole('heading',{name:'Создание учётной записи'})).toBeVisible();
});

test('already displayed login errors switch language too', async ({page}) => {
 await page.route('**/api/v1/auth/login', route => route.fulfill({status:401,json:{error:'invalid email or password'}}));
 await page.goto('/login');
 await page.getByLabel('Электронная почта',{exact:true}).fill('alice@example.com');
 await page.getByLabel('Пароль',{exact:true}).fill('invalid-password');
 await page.getByRole('button',{name:'Войти',exact:true}).click();
 await expect(page.getByText('Неверный адрес электронной почты или пароль.')).toBeVisible();
 await page.getByRole('button',{name:'English',exact:true}).click();
 await expect(page.getByText('Invalid email or password.')).toBeVisible();
});

test('document labels, categories, dates and size switch without changing document content', async ({page}) => {
 await authenticate(page);
 await page.goto('/documents');
 await expect(page.getByRole('heading',{name:'Документы',exact:true})).toBeVisible();
 await expect(page.getByRole('cell',{name:'Финансы',exact:true})).toBeVisible();
 await expect(page.getByRole('cell',{name:'1,5 КБ',exact:true})).toBeVisible();
 await expect(page.getByRole('cell',{name:/апр\. 2026/})).toBeVisible();
 await expect(page.getByRole('link',{name:doc.name,exact:true})).toBeVisible();
 await page.getByRole('button',{name:'English',exact:true}).click();
 await expect(page.getByRole('cell',{name:'Financials',exact:true})).toBeVisible();
 await expect(page.getByRole('cell',{name:'1.5 KB',exact:true})).toBeVisible();
 await expect(page.getByRole('cell',{name:/Apr/})).toBeVisible();
 await expect(page.getByRole('link',{name:doc.name,exact:true})).toBeVisible();
});

const screens = [
 ['/admin/users','Пользователи','Users'],
 ['/admin/categories','Категории','Categories'],
 ['/admin/permissions','Права доступа','Access Grants'],
 ['/admin/branding','Оформление','Branding'],
 ['/admin/watermark','Настройки водяных знаков','Watermark Settings'],
 ['/admin/storage','Настройки хранилища','Storage Settings'],
 ['/admin/nda','Шаблоны NDA','NDA Templates'],
 ['/analytics','Аналитика','Analytics Dashboard'],
 ['/activity','Журнал активности','Activity Log'],
 ['/documents/upload','Загрузить документ','Upload Document'],
 ['/qa','Вопросы и ответы','Questions & Answers'],
 ['/nda-sign/required','Соглашение о неразглашении','Non-Disclosure Agreement']
];
for(const [path,ru,en] of screens) test(`switches both languages on ${path}`,async ({page}) => {
 await authenticate(page);
 await page.goto(path);
 await expect(page.getByRole('heading',{name:ru,exact:true,level:1})).toBeVisible();
 await page.getByRole('button',{name:'English',exact:true}).click();
 await expect(page.getByRole('heading',{name:en,exact:true,level:1})).toBeVisible();
});

test('unsupported browser locale falls back to English', async ({browser}) => {
 const context = await browser.newContext({locale:'hy-AM'});
 try {
  const page = await context.newPage();
  await page.goto('http://127.0.0.1:4174/login');
  await expect(page.getByRole('heading',{name:'Sign In',exact:true})).toBeVisible();
 } finally {await context.close();}
});

test('language control fits a narrow screen and works with the keyboard',async ({page}) => {
 await page.setViewportSize({width:375,height:812});
 await page.goto('/login');
 await expect(page.getByRole('heading',{name:'Войти',exact:true})).toBeVisible();
 const control=page.getByRole('button',{name:'English',exact:true});
 await expect(control).toBeInViewport();
 await control.focus();
 await page.keyboard.press('Enter');
 await expect(page.getByRole('heading',{name:'Sign In',exact:true})).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('keeps document version URLs independent of displayed number formatting', async ({page}) => {
 await authenticate(page);
 await page.route('**/api/v1/documents/example',route => route.fulfill({json:{success:true,data:{document:{...doc,current_version:10000},versions:[{...doc,version_number:10000}],permissions:{can_download:true,can_manage:true}}}}));
 await page.goto('/documents/example');
 await expect(page.getByRole('link',{name:'Открыть PDF',exact:true})).toHaveAttribute('href','/documents/example/preview?version=10000');
});

test('formats displayed opacity without changing CSS values', async ({page}) => {
 await authenticate(page);
 await page.goto('/admin/watermark');
 await expect(page.locator('.sample-watermark')).toHaveCSS('opacity','0.3');
 await expect(page.locator('.value')).toHaveText('0,3');
});

test('keeps status styling stable while translating the discussion label', async ({page}) => {
 await authenticate(page);
 await page.goto('/qa/thread');
 const status = page.locator('.thread-header .status');
 await expect(status).toHaveText('Открыт');
 await expect(status).toHaveClass(/\bstatus-open\b/);
 await page.getByRole('button',{name:'English',exact:true}).click();
 await expect(status).toHaveText('open');
 await expect(status).toHaveClass(/\bstatus-open\b/);
});

test('translates the NDA load error and updates an existing error on language switch', async ({page}) => {
 await authenticate(page);
 await page.route('**/api/v1/nda/active',route => route.fulfill({status:500,json:{error:'Internal server error'}}));
 await page.goto('/nda-sign/required');
 await expect(page.getByText('Не удалось загрузить NDA.',{exact:true})).toBeVisible();
 await page.getByRole('button',{name:'English',exact:true}).click();
 await expect(page.getByText('Failed to load the NDA.',{exact:true})).toBeVisible();
});
