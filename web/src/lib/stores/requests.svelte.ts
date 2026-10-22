/**
 * Svelte 5 rune-based store for captured requests.
 */
import type { CapturedRequest, CapturedResponse, ForwardResultData } from '$lib/types';
import { listRequests, getRequest, deleteRequest as apiDeleteRequest, deleteAllRequests as apiDeleteAll } from '$lib/api';

let items = $state<CapturedRequest[]>([]);
let total = $state(0);
let loading = $state(false);
let selectedId = $state<string | null>(null);
let error = $state<string | null>(null);
let responses = $state<Map<string, CapturedResponse>>(new Map());
let forwardResults = $state<Map<string, ForwardResultData>>(new Map());

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

		/** Get the captured HTTP response for a given request ID. */
		getResponse(requestId: string): CapturedResponse | undefined {
			const live = responses.get(requestId);
			if (live) return live;
			// Otherwise the one the server recorded with the request.
			const info = items.find((r) => r.id === requestId)?.response_info;
			return info && { status: info.status, headers: info.headers, body: info.body, content_type: info.content_type, source: info.source, body_truncated: info.body_truncated };
		},

		/** Attach a captured HTTP response to a request by ID. */
		attachResponse(requestId: string, response: CapturedResponse) {
			const next = new Map(responses);
			next.set(requestId, response);
			responses = next;
		},

		/** Get the forward target's response for a given request ID. */
		getForwardResult(requestId: string): ForwardResultData | undefined {
			return forwardResults.get(requestId) ?? items.find((r) => r.id === requestId)?.forward_result;
		},

		/** Reload one request from the server, to pick up a forward result recorded after it was listed. */
		async refresh(requestId: string) {
			try {
				const fresh = await getRequest(requestId);
				items = items.map((r) => (r.id === requestId ? fresh : r));
			} catch {
				// Keep what we have; the detail view says no result was recorded.
			}
		},

		/** Attach a forward target response to a request by ID. */
		attachForwardResult(requestId: string, result: ForwardResultData) {
			const next = new Map(forwardResults);
			next.set(requestId, result);
			forwardResults = next;
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
			// Open the very first request so a new user sees it right away.
			if (items.length === 0 && !selectedId) selectedId = req.id;
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
			const backupResponses = new Map(responses);
			const backupForwardResults = new Map(forwardResults);
			error = null;
			try {
				items = items.filter((r) => r.id !== id);
				total = Math.max(0, total - 1);
				if (selectedId === id) selectedId = null;
				const next = new Map(responses);
				next.delete(id);
				responses = next;
				const nextFwd = new Map(forwardResults);
				nextFwd.delete(id);
				forwardResults = nextFwd;
				await apiDeleteRequest(id);
			} catch (e) {
				items = backup;
				total = backupTotal;
				selectedId = backupSelectedId;
				responses = backupResponses;
				forwardResults = backupForwardResults;
				error = e instanceof Error ? e.message : 'Failed to delete request';
			}
		},

		async clearAll(endpointId: string) {
			const backup = items;
			const backupTotal = total;
			const backupSelectedId = selectedId;
			const backupResponses = new Map(responses);
			const backupForwardResults = new Map(forwardResults);
			error = null;
			try {
				items = [];
				total = 0;
				selectedId = null;
				responses = new Map();
				forwardResults = new Map();
				await apiDeleteAll(endpointId);
			} catch (e) {
				items = backup;
				total = backupTotal;
				selectedId = backupSelectedId;
				responses = backupResponses;
				forwardResults = backupForwardResults;
				error = e instanceof Error ? e.message : 'Failed to clear requests';
			}
		},

		reset() {
			items = [];
			total = 0;
			selectedId = null;
			error = null;
			responses = new Map();
			forwardResults = new Map();
		}
	};
}
