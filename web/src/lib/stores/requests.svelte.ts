/**
 * Svelte 5 rune-based store for captured requests.
 */
import type { CapturedRequest } from '$lib/types';
import { listRequests, deleteRequest as apiDeleteRequest, deleteAllRequests as apiDeleteAll } from '$lib/api';

let items = $state<CapturedRequest[]>([]);
let total = $state(0);
let loading = $state(false);
let selectedId = $state<string | null>(null);
let error = $state<string | null>(null);

export function getRequestsStore() {
	return {
		get items() {
			return items;
		},
		get total() {
			return total;
		},
		get loading() {
			return loading;
		},
		get selectedId() {
			return selectedId;
		},
		get error() {
			return error;
		},
		get selected(): CapturedRequest | undefined {
			return items.find((r) => r.id === selectedId);
		},

		clearError() {
			error = null;
		},

		async load(endpointId: string, limit = 50, offset = 0) {
			loading = true;
			error = null;
			try {
				const res = await listRequests(endpointId, limit, offset);
				items = res.requests;
				total = res.total;
			} catch (e) {
				items = [];
				total = 0;
				error = e instanceof Error ? e.message : 'Failed to load requests';
			} finally {
				loading = false;
			}
		},

		/** Prepend a new request from the WebSocket stream */
		prepend(req: CapturedRequest) {
			items = [req, ...items];
			total += 1;
		},

		select(id: string | null) {
			selectedId = id;
		},

		async remove(id: string) {
			const backup = items;
			const backupTotal = total;
			const backupSelectedId = selectedId;
			error = null;
			try {
				items = items.filter((r) => r.id !== id);
				total = Math.max(0, total - 1);
				if (selectedId === id) selectedId = null;
				await apiDeleteRequest(id);
			} catch (e) {
				items = backup;
				total = backupTotal;
				selectedId = backupSelectedId;
				error = e instanceof Error ? e.message : 'Failed to delete request';
			}
		},

		async clearAll(endpointId: string) {
			const backup = items;
			const backupTotal = total;
			const backupSelectedId = selectedId;
			error = null;
			try {
				items = [];
				total = 0;
				selectedId = null;
				await apiDeleteAll(endpointId);
			} catch (e) {
				items = backup;
				total = backupTotal;
				selectedId = backupSelectedId;
				error = e instanceof Error ? e.message : 'Failed to clear requests';
			}
		},

		reset() {
			items = [];
			total = 0;
			selectedId = null;
			error = null;
		}
	};
}
