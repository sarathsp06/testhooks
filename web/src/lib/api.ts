/**
 * Typed fetch wrapper for the /api/* REST endpoints.
 * Matches handler/api.go routes exactly.
 */
import type { Endpoint, CapturedRequest, PaginatedRequests } from './types';

const BASE = '/api';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(`${BASE}${path}`, {
		headers: { 'Content-Type': 'application/json', ...init?.headers },
		...init
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

export function listEndpoints() {
	return request<Endpoint[]>('/endpoints');
}

export function getEndpoint(id: string) {
	return request<Endpoint>(`/endpoints/${id}`);
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
