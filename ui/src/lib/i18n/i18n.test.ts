import { beforeEach, describe, expect, it, vi } from 'vitest';
import { get } from 'svelte/store';
import { detectLocale, initializeLocale, locale, setLocale, t, translate, categoryName } from './index';

describe('language preferences', () => {
 beforeEach(() => { localStorage.clear(); setLocale('en'); localStorage.clear(); });
 it('recognizes regional Russian and English browser languages in priority order', () => {
  expect(detectLocale(['ru-RU', 'en-US'])).toBe('ru');
  expect(detectLocale(['en-GB', 'ru'])).toBe('en');
  expect(detectLocale(['hy-AM', 'ru'])).toBe('ru');
  expect(detectLocale(['de-DE'])).toBe('en');
  expect(detectLocale([])).toBe('en');
 });
 it('uses browser preferences until a manual language is saved', () => {
  initializeLocale(['ru-RU']);
  expect(get(locale)).toBe('ru');
  expect(localStorage.getItem('dd_locale')).toBeNull();
  setLocale('en');
  initializeLocale(['ru-RU']);
  expect(get(locale)).toBe('en');
  expect(document.documentElement.lang).toBe('en');
 });
 it('ignores corrupt saved preferences', () => {
  localStorage.setItem('dd_locale', 'unsupported');
  initializeLocale(['ru']);
  expect(get(locale)).toBe('ru');
 });
 it('keeps language switching usable when storage is blocked', () => {
  const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
  try { expect(() => setLocale('ru')).not.toThrow(); expect(get(locale)).toBe('ru'); }
  finally { spy.mockRestore(); }
 });
 it('updates an existing translation subscription when the language changes', () => {
  const output: string[] = [];
  const unsubscribe = t.subscribe(fn => output.push(fn('Sign In')));
  setLocale('ru');
  unsubscribe();
  expect(output).toEqual(['Sign In', 'Войти']);
  expect(document.documentElement.lang).toBe('ru');
 });
});

describe('translations', () => {
 it('interpolates values without translating user content or interpreting it as HTML', () => {
  expect(translate('ru', 'Asked by {name}', {name:'Alice <b>Admin</b>'})).toBe('Автор вопроса: Alice <b>Admin</b>');
  expect(translate('en', 'Asked by {name}', {name:'Alice'})).toBe('Asked by Alice');
 });
 it('uses Russian plural forms, including teens', () => {
  for (const [count, text] of [[1,'1 сообщение'],[2,'2 сообщения'],[5,'5 сообщений'],[11,'11 сообщений'],[21,'21 сообщение']] as const)
   expect(translate('ru', '{count} messages', {count})).toBe(text);
 });
 it('localizes only unchanged built-in categories', () => {
  expect(categoryName({id:'cat-financials',name:'Financials'},'ru')).toBe('Финансы');
  expect(categoryName({id:'custom',name:'Financials'},'ru')).toBe('Financials');
  expect(categoryName({id:'cat-financials',name:'Our financials'},'ru')).toBe('Our financials');
 });
});

it('translates parameterized notices again after switching languages', async () => {
 const { msg } = await import('./index');
 const notice = msg('Failed to change role for {email}.', {email:'user@example.com'});
 setLocale('en');
 expect(get(t)(notice)).toBe('Failed to change role for user@example.com.');
 setLocale('ru');
 expect(get(t)(notice)).toBe('Не удалось изменить роль пользователя user@example.com.');
});

it('keeps default descriptions and custom category content separate', async () => {
 const { categoryDescription } = await import('./index');
 expect(categoryDescription({id:'cat-financials',description:'Financial statements, projections, cap table, tax returns'},'ru')).toBe('Финансовая отчётность, прогнозы, таблица долей, налоговые декларации');
 expect(categoryDescription({id:'cat-financials',description:'Custom financial description'},'ru')).toBe('Custom financial description');
 expect(categoryName({id:'cat-financials',name:'— Financials'},'ru')).toBe('— Финансы');
});
