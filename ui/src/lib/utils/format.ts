import { getLocale, intlLocale, type Locale } from '$lib/i18n';

/**
 * Formatting utilities for the portal UI.
 */

/**
 * Format a byte count as a human-readable file size.
 */
export function formatFileSize(bytes: number, language: Locale = getLocale()): string {
	const units = language === 'ru' ? ['Б', 'КБ', 'МБ', 'ГБ'] : ['B', 'KB', 'MB', 'GB'];
	const index = bytes < 1024 ? 0 : Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 3);
	const value = new Intl.NumberFormat(intlLocale(language), {
		minimumFractionDigits: index ? 1 : 0,
		maximumFractionDigits: index ? 1 : 0
	}).format(bytes / 1024 ** index);
	return `${value} ${units[index]}`;
}

/**
 * Format an ISO date string as a short locale date.
 */
export function formatDate(iso: string, language: Locale = getLocale()): string {
	return new Date(iso).toLocaleDateString(intlLocale(language), {
		year: 'numeric',
		month: 'short',
		day: 'numeric'
	});
}

/**
 * Format an ISO date string as a date+time string.
 */
export function formatDateTime(iso: string, language: Locale = getLocale()): string {
	return new Date(iso).toLocaleString(intlLocale(language), {
		month: 'short',
		day: 'numeric',
		hour: '2-digit',
		minute: '2-digit'
	});
}

/**
 * Truncate a string to the given length with ellipsis.
 */
export function truncate(str: string, maxLen: number): string {
	if (str.length <= maxLen) return str;
	return str.slice(0, maxLen - 1) + '\u2026';
}
