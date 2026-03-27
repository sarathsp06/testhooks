/**
 * Typed fetch wrapper for the /api/* REST endpoints.
 * Matches handler/api.go routes exactly.
 */
import type { Endpoint, CapturedRequest, PaginatedRequests } from './types';

const BASE = '/api';

/**
 * Auth token for API requests. Stored in localStorage so it persists across
 * page reloads. The token is sent as a Bearer token in the Authorization header.
 * When AUTH_TOKEN is not set on the server, all requests pass through without auth.
 */
const AUTH_TOKEN_KEY = 'testhooks_auth_token';

export function getAuthToken(): string {
	return localStorage.getItem(AUTH_TOKEN_KEY) ?? '';
}

export function setAuthToken(token: string) {
	if (token) {
		localStorage.setItem(AUTH_TOKEN_KEY, token);
	} else {
		localStorage.removeItem(AUTH_TOKEN_KEY);
	}
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
	const headers: Record<string, string> = {
		'Content-Type': 'application/json',
		...((init?.headers as Record<string, string>) ?? {})
	};

	// Add auth token if available.
	const token = getAuthToken();
	if (token) {
		headers['Authorization'] = `Bearer ${token}`;
	}

	// Add client identity for endpoint filtering.
	headers['X-Client-ID'] = getClientId();

	const res = await fetch(`${BASE}${path}`, {
		...init,
		headers
	});
	if (!res.ok) {
		const body = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(body.error || `HTTP ${res.status}`);
	}
	if (res.status === 204) return undefined as T;
	return res.json();
}

// --- Endpoints ---

export function createEndpoint(opts: { name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> } = {}) {
	return request<Endpoint>('/endpoints', {
		method: 'POST',
		body: JSON.stringify({ name: opts.name ?? '', mode: opts.mode ?? 'server', config: opts.config })
	});
}

export async function listEndpoints(): Promise<Endpoint[]> {
	const resp = await request<{ endpoints: Endpoint[]; limit: number; offset: number }>('/endpoints');
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
	patch: { name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> }
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
	return request<void>(`/endpoints/${endpointId}/requests`, { method: 'DELETE' });
}
