/**
 * API Client for the Due Diligence Portal.
 * Follows the WitFoo Analytics pattern: JWT auth, typed responses, error handling.
 */

const API_BASE = '/api/v1';
const ACCESS_KEY = 'dd_auth_token';
const REFRESH_KEY = 'dd_refresh_token';

export class ApiError extends Error {
	constructor(
		public status: number,
		public statusText: string,
		public body: unknown
	) {
		super(`API Error: ${status} ${statusText}`);
		this.name = 'ApiError';
	}
}

export interface ApiResponse<T> {
	success: boolean;
	message?: string;
	data?: T;
	meta?: {
		count: number;
		total?: number;
		page?: number;
		page_size?: number;
		has_more?: boolean;
	};
	timestamp: string;
}

export interface ApiErrorResponse {
	success: false;
	error: string;
	details?: Array<{
		code?: string;
		field?: string;
		message: string;
	}>;
	timestamp: string;
}

/**
 * Extract the server's user-facing error message from a thrown error.
 * Returns the envelope's `error` string when present and non-empty, else `fallback`.
 */
export function apiErrorMessage(err: unknown, fallback: string): string {
	if (err instanceof ApiError && err.body !== null && typeof err.body === 'object') {
		const message = (err.body as { error?: unknown }).error;
		if (typeof message === 'string' && message.trim() !== '') return message;
	}
	return fallback;
}

function getAuthToken(): string | null {
	if (typeof window === 'undefined') return null;
	return localStorage.getItem(ACCESS_KEY);
}

function getRefreshToken(): string | null {
	if (typeof window === 'undefined') return null;
	return localStorage.getItem(REFRESH_KEY);
}

function setAuthToken(token: string): void {
	if (typeof window === 'undefined') return;
	localStorage.setItem(ACCESS_KEY, token);
}

function setTokens(access: string, refresh?: string): void {
	if (typeof window === 'undefined') return;
	localStorage.setItem(ACCESS_KEY, access);
	if (refresh) localStorage.setItem(REFRESH_KEY, refresh);
}

function clearTokens(): void {
	if (typeof window === 'undefined') return;
	localStorage.removeItem(ACCESS_KEY);
	localStorage.removeItem(REFRESH_KEY);
}

function redirectToLogin(): void {
	if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
		window.location.href = '/login';
	}
}

/**
 * Attempt to obtain a fresh access token using the stored refresh token.
 * Returns true on success (the new access token is persisted).
 */
let refreshPromise: Promise<boolean> | null = null;
function tryRefresh(): Promise<boolean> {
 if (!refreshPromise) refreshPromise = performRefresh().finally(() => { refreshPromise = null; });
 return refreshPromise;
}
async function performRefresh(): Promise<boolean> {
	const refresh = getRefreshToken();
	if (!refresh) return false;
	try {
		const resp = await fetch(`${API_BASE}/auth/refresh`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ refresh_token: refresh })
		});
		if (!resp.ok) return false;
		const data = await resp.json();
		const newToken = data?.data?.access_token;
		if (typeof newToken === 'string' && newToken) {
			setAuthToken(newToken);
			return true;
		}
	} catch {
		// fall through
	}
	return false;
}

async function request<T>(
	method: string,
	path: string,
	body?: unknown,
	options: RequestInit = {},
	isRetry = false
): Promise<ApiResponse<T>> {
	const url = `${API_BASE}${path}`;
	const headers: Record<string, string> = {
		...(options.headers as Record<string, string>)
	};

	const token = getAuthToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}

	if (body && !(body instanceof FormData)) {
		headers['Content-Type'] = 'application/json';
	}

	const response = await fetch(url, {
		method,
		headers,
		body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
		...options
	});

	if (response.status === 401) {
		// Try a one-shot token refresh before giving up (skip for the auth endpoints
		// themselves to avoid loops).
		const isAuthPath = path.startsWith('/auth/');
		if (!isRetry && !isAuthPath && (await tryRefresh())) {
			return request<T>(method, path, body, options, true);
		}
		clearTokens();
		redirectToLogin();
		throw new ApiError(401, 'Unauthorized', null);
	}

	if (!response.ok) {
		const errorBody = await response.json().catch(() => null);
		throw new ApiError(response.status, response.statusText, errorBody);
	}

	if (response.status === 204) {
		return { success: true, timestamp: new Date().toISOString() } as ApiResponse<T>;
	}

	return response.json();
}

/**
 * Download a file from an authenticated endpoint. Uses fetch (so the JWT is sent in
 * the Authorization header), reads the response as a Blob, and triggers a browser
 * download — a plain <a href> cannot attach the bearer token and would 401.
 */
export interface BinaryResponse { blob: Blob; filename: string | null }
async function binary(path: string, options: { signal?: AbortSignal } = {}, isRetry = false): Promise<BinaryResponse> {
 const token = getAuthToken();
 const resp = await fetch(`${API_BASE}${path}`, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: options.signal });
 if (resp.status === 401) {
  if (!isRetry && await tryRefresh()) return binary(path, options, true);
  clearTokens(); redirectToLogin(); throw new ApiError(401, 'Unauthorized', null);
 }
 if (!resp.ok) throw new ApiError(resp.status, resp.statusText, await resp.json().catch(() => null));
 const cd = resp.headers.get('Content-Disposition');
 const encoded = cd?.match(/filename\*=UTF-8''([^;]+)/i);
 let filename = cd?.match(/filename="([^"\r\n]*)"|filename=([^;\r\n]+)/i)?.slice(1).find(Boolean) ?? null;
 if (encoded) { try { filename = decodeURIComponent(encoded[1]); } catch { /* Use ordinary filename. */ } }
 return { blob: await resp.blob(), filename };
}
async function download(path: string, fallbackName = 'download'): Promise<void> {
 if (typeof window === 'undefined') return;
 const { blob, filename } = await binary(path);
 const objectUrl = URL.createObjectURL(blob);
 const a = document.createElement('a'); a.href = objectUrl; a.download = filename ?? fallbackName;
 document.body.appendChild(a); a.click(); a.remove();
 // Allow the browser to start consuming the URL before releasing it.
 setTimeout(() => URL.revokeObjectURL(objectUrl), 1000);
}

export const api = {
 binary,
	get: <T>(path: string, options: RequestInit = {}) => request<T>('GET', path, undefined, options),
	post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
	put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
	patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
	delete: <T>(path: string) => request<T>('DELETE', path),
	upload: <T>(path: string, formData: FormData) => request<T>('POST', path, formData),
	download,
	setToken: setAuthToken,
	setTokens,
	clearToken: clearTokens,
	clearTokens,
	getToken: getAuthToken
};
