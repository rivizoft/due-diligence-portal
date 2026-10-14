import { describe, it, expect } from 'vitest';
import { ApiError, apiErrorMessage, api } from './client';

describe('apiErrorMessage', () => {
	it('returns the server error message from the envelope', () => {
		const err = new ApiError(400, 'Bad Request', {
			success: false,
			error: 'current password is incorrect'
		});
		expect(apiErrorMessage(err, 'fallback')).toBe('current password is incorrect');
	});

	it('falls back when the body is null', () => {
		expect(apiErrorMessage(new ApiError(500, 'Internal Server Error', null), 'fallback')).toBe(
			'fallback'
		);
	});

	it('falls back when the error field is empty', () => {
		const err = new ApiError(400, 'Bad Request', { success: false, error: '   ' });
		expect(apiErrorMessage(err, 'fallback')).toBe('fallback');
	});

	it('falls back when the error field is not a string', () => {
		const err = new ApiError(400, 'Bad Request', { success: false, error: { code: 1 } });
		expect(apiErrorMessage(err, 'fallback')).toBe('fallback');
	});

	it('falls back for non-API errors', () => {
		expect(apiErrorMessage(new TypeError('Failed to fetch'), 'fallback')).toBe('fallback');
		expect(apiErrorMessage(undefined, 'fallback')).toBe('fallback');
	});
});

describe('authenticated PDF content', () => {
 it('returns PDF bytes and attaches the access token', async () => {
  localStorage.setItem('dd_auth_token', 'preview-token');
  const bytes = new Uint8Array([37, 80, 68, 70]);
  const original = globalThis.fetch;
  let authorization = '';
  globalThis.fetch = async (_input, init) => {
   authorization = (init?.headers as Record<string, string>).Authorization;
   return new Response(bytes, { headers: { 'Content-Type': 'application/pdf' } });
  };
  try {
   const result = await api.bytes('/documents/example/preview');
   expect(new Uint8Array(result)).toEqual(bytes);
   expect(authorization).toBe('Bearer preview-token');
  } finally { globalThis.fetch = original; localStorage.clear(); }
 });
});
