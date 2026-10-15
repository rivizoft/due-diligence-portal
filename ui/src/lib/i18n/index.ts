import { derived, get, writable } from 'svelte/store';
import ru from './ru.json';
import en from './en.json';
import categories from './categories.json';

export type Locale = 'en' | 'ru';
export type Params = Record<string, string | number>;
export interface Message { key: string; params: Params }
export type LocalizedText = string | Message;
type Translation = string | Partial<Record<Intl.LDMLPluralRule, string>>;

const dictionaries: Record<Locale, Record<string, Translation>> = { en, ru };
const preferenceKey = 'dd_locale';
const currentLocale = writable<Locale>('en');
export const locale = { subscribe: currentLocale.subscribe };
export const intlLocale = (language: Locale): string => language === 'ru' ? 'ru-RU' : 'en-US';
export const getLocale = (): Locale => get(locale);

export function detectLocale(languages: readonly string[]): Locale {
	for (const language of languages) {
		const base = language.toLowerCase().split(/[-_]/)[0];
		if (base === 'ru' || base === 'en') return base;
	}
	return 'en';
}

function applyLocale(language: Locale): void {
	currentLocale.set(language);
	if (typeof document !== 'undefined') document.documentElement.lang = language;
}

export function initializeLocale(languages?: readonly string[]): void {
	let saved: string | null = null;
	try { saved = localStorage.getItem(preferenceKey); } catch { /* Storage can be disabled. */ }
	const preferred = languages ?? (typeof navigator === 'undefined' ? []
		: navigator.languages?.length ? navigator.languages : [navigator.language]);
	applyLocale(saved === 'ru' || saved === 'en' ? saved : detectLocale(preferred));
}

export function setLocale(language: Locale): void {
	applyLocale(language);
	try { localStorage.setItem(preferenceKey, language); } catch { /* Switching still works for this visit. */ }
}

/** Preserve parameters until rendering so an existing notice also changes language. */
export const msg = (key: string, params: Params = {}): Message => ({ key, params });

/** Text interpolation is rendered by Svelte as text, never as HTML. */
export function translate(language: Locale, key: string, params: Params = {}): string {
	const value = dictionaries[language][key];
	const text = typeof value === 'string' ? value
		: value?.[new Intl.PluralRules(intlLocale(language)).select(Number(params.count))] ?? value?.other ?? key;
	return text.replace(/\{(\w+)\}/g, (match, name: string) => {
		const parameter = params[name];
		if (parameter === undefined) return match;
		return typeof parameter === 'number'
			? new Intl.NumberFormat(intlLocale(language)).format(parameter) : parameter;
	});
}

export function hasTranslation(key: string): boolean { return Object.hasOwn(ru, key); }
export const t = derived(locale, language => (text: LocalizedText, params: Params = {}) =>
	typeof text === 'string' ? translate(language, text, params) : translate(language, text.key, text.params));
export const number = derived(locale, language => {
	const formatter = new Intl.NumberFormat(intlLocale(language));
	return (value: number) => formatter.format(value);
});

/** Translate default category fields only while their stored values are unmodified. */
const defaults: Record<string, { name: string; description: string }> = categories;
export function categoryName(category: { id: string; name: string }, language: Locale = getLocale()): string {
	const prefix = category.name.match(/^(?:— )*/)?.[0] ?? '';
	const name = category.name.slice(prefix.length);
	return defaults[category.id]?.name === name ? prefix + translate(language, name) : category.name;
}

export function categoryDescription(category: { id: string; description?: string }, language: Locale = getLocale()): string {
	const description = category.description ?? '';
	return defaults[category.id]?.description === description ? translate(language, description) : description;
}
