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

import { beforeEach, afterEach, vi } from 'vitest';
describe('authenticated binary', () => {
 beforeEach(() => { localStorage.clear();localStorage.setItem('dd_auth_token','old-token'); });
 afterEach(() => { vi.unstubAllGlobals();localStorage.clear(); });
 it('uses header auth and preserves the download filename', async () => {
  const fetcher=vi.fn().mockResolvedValue(new Response('PDF',{headers:{'Content-Type':'application/pdf','Content-Disposition':'inline; filename="report.pdf"'}}));vi.stubGlobal('fetch',fetcher);
  const result=await api.binary('/documents/d/preview');expect(result.filename).toBe('report.pdf');expect(result.blob.type).toBe('application/pdf');expect(fetcher.mock.calls[0][0]).toBe('/api/v1/documents/d/preview');expect(fetcher.mock.calls[0][1].headers.Authorization).toBe('Bearer old-token');
 });
 it('propagates preparation errors from the envelope', async () => {
  vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({error:'PDF preparation timed out.'}),{status:504})));
  try { await api.binary('/documents/d/preview');throw new Error('must reject'); } catch(err){expect(apiErrorMessage(err,'fallback')).toBe('PDF preparation timed out.');}
 });
 it('refreshes once and retries with the new token', async () => {
  localStorage.setItem('dd_refresh_token','refresh');const fetcher=vi.fn().mockResolvedValueOnce(new Response('',{status:401})).mockResolvedValueOnce(new Response(JSON.stringify({data:{access_token:'new-token'}}))).mockResolvedValueOnce(new Response('PNG',{headers:{'Content-Type':'image/png'}}));vi.stubGlobal('fetch',fetcher);
  await api.binary('/documents/d/preview-pages/1');expect(fetcher).toHaveBeenCalledTimes(3);expect(fetcher.mock.calls[2][1].headers.Authorization).toBe('Bearer new-token');
 });
 it('passes cancellation through to fetch', async () => {
  const controller=new AbortController();controller.abort();const fetcher=vi.fn().mockRejectedValue(new DOMException('aborted','AbortError'));vi.stubGlobal('fetch',fetcher);
  await expect(api.binary('/documents/d/preview',{signal:controller.signal})).rejects.toThrow('aborted');expect(fetcher.mock.calls[0][1].signal).toBe(controller.signal);
 });
});

describe('localized API diagnostics', () => {
 it('uses the action fallback for an unknown upstream diagnostic', () => {
  const err = new ApiError(503, 'Unavailable', {error:'upstream provider diagnostic'});
  expect(apiErrorMessage(err, 'Failed to save storage settings.')).toBe('Failed to save storage settings.');
 });
});
