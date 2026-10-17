/**
 * Typed fetch wrapper for the /api/* REST endpoints.
 * Matches handler/api.go routes exactly.
 */
import type { Endpoint, CapturedRequest, PaginatedRequests } from './types';
import { auth } from './auth.svelte';

const BASE = '/api';

/**
 * Auth token for API requests, sent as a Bearer token. When AUTH_TOKEN is not
 * set on the server, all requests pass through without auth. Token state lives
 * in auth.svelte.ts; these wrappers are kept for existing callers.
 */
export function getAuthToken(): string {
	return auth.token;
}

export function setAuthToken(token: string) {
	if (token) auth.save(token);
	else auth.signOut();
}

/**
 * Client identity for UX convenience (not access control).
 * Generated once per browser and stored in localStorage. Sent as X-Client-ID
 * header so the server can associate endpoints with this browser and filter
 * the endpoint list accordingly.
 */
const CLIENT_ID_KEY = 'testhooks_client_id';

export function getClientId(): string {
	let id = localStorage.getItem(CLIENT_ID_KEY);
	if (!id) {
		id = crypto.randomUUID();
		localStorage.setItem(CLIENT_ID_KEY, id);
	}
	return id;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	for (;;) {
		const headers: Record<string, string> = {
			'Content-Type': 'application/json',
			...((init?.headers as Record<string, string>) ?? {})
		};

		// Add auth token if available.
		const token = auth.token;
		if (token) {
			headers['Authorization'] = `Bearer ${token}`;
		}

		// Add client identity for endpoint filtering.
		headers['X-Client-ID'] = getClientId();

		const res = await fetch(`${BASE}${path}`, {
			...init,
			headers
		});
		if (res.status === 401) {
			// Ask for a token, then retry the same request with it.
			await auth.requireToken(token);
			continue;
		}
		if (!res.ok) {
			const body = await res.json().catch(() => ({ error: res.statusText }));
			throw new Error(body.error || `HTTP ${res.status}`);
		}
		if (res.status === 204) return undefined as T;
		return res.json();
	}
}

/**
 * Check a candidate token against the server without storing it.
 * Returns true if the server accepts it (or doesn't require auth).
 */
export async function verifyAuthToken(token: string): Promise<boolean> {
	const res = await fetch(`${BASE}/endpoints?limit=1`, {
		headers: { Authorization: `Bearer ${token}`, 'X-Client-ID': getClientId() }
	});
	if (res.status === 401) return false;
	if (!res.ok) throw new Error(`Server error (HTTP ${res.status})`);
	return true;
}

/**
 * Make a cheap authenticated call so a missing or stale token surfaces the
 * sign-in prompt. Used when a WebSocket can't connect, since browsers don't
 * expose the upgrade's 401 status.
 */
export async function checkAuth(): Promise<void> {
	try {
		await request<unknown>('/endpoints?limit=1');
	} catch {
		// Network or server errors are handled by the caller's reconnect logic.
	}
}

// --- Endpoints ---

export function createEndpoint(
	opts: {
		name?: string;
		mode?: 'server' | 'browser';
		config?: Record<string, unknown>;
	} = {}
) {
	return request<Endpoint>('/endpoints', {
		method: 'POST',
		body: JSON.stringify({
			name: opts.name ?? '',
			mode: opts.mode ?? 'server',
			config: opts.config
		})
	});
}

export async function listEndpoints(): Promise<Endpoint[]> {
	const resp = await request<{
		endpoints: Endpoint[];
		limit: number;
		offset: number;
	}>('/endpoints');
	return resp.endpoints;
}

export function getEndpoint(id: string) {
	return request<Endpoint>(`/endpoints/${id}`);
}

export function getEndpointBySlug(slug: string) {
	return request<Endpoint>(`/slug/${slug}`);
}

export function updateEndpoint(
	id: string,
	patch: {
		name?: string;
		mode?: 'server' | 'browser';
		config?: Record<string, unknown>;
	}
) {
	return request<Endpoint>(`/endpoints/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(patch)
	});
}

export function deleteEndpoint(id: string) {
	return request<void>(`/endpoints/${id}`, { method: 'DELETE' });
}

// --- Requests ---

export function listRequests(endpointId: string, limit = 50, offset = 0) {
	return request<PaginatedRequests>(
		`/endpoints/${endpointId}/requests?limit=${limit}&offset=${offset}`
	);
}

export function getRequest(reqId: string) {
	return request<CapturedRequest>(`/requests/${reqId}`);
}

export function deleteRequest(reqId: string) {
	return request<void>(`/requests/${reqId}`, { method: 'DELETE' });
}

export function deleteAllRequests(endpointId: string) {
	return request<void>(`/endpoints/${endpointId}/requests`, {
		method: 'DELETE'
	});
}
