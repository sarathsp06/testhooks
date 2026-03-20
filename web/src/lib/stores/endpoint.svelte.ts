/**
 * Svelte 5 rune-based store for the current endpoint.
 */
import type { Endpoint } from '$lib/types';
import { getEndpoint, updateEndpoint } from '$lib/api';

let current = $state<Endpoint | null>(null);
let loading = $state(false);
let error = $state<string | null>(null);

export function getEndpointStore() {
	return {
		get current() {
			return current;
		},
		get loading() {
			return loading;
		},
		get error() {
			return error;
		},

		clearError() {
			error = null;
		},

		async load(id: string) {
			loading = true;
			error = null;
			try {
				current = await getEndpoint(id);
			} catch (e) {
				error = e instanceof Error ? e.message : 'Failed to load endpoint';
				current = null;
			} finally {
				loading = false;
			}
		},

		async update(patch: { name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> }) {
			if (!current) return;
			try {
				current = await updateEndpoint(current.id, patch);
			} catch (e) {
				error = e instanceof Error ? e.message : 'Failed to update endpoint';
			}
		},

		set(ep: Endpoint) {
			current = ep;
			error = null;
		},

		clear() {
			current = null;
			error = null;
		}
	};
}
