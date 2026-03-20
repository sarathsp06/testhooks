/**
 * Browser-side forwarding: forward a captured request to a single target URL.
 * Uses fetch() from the browser, so localhost targets work.
 *
 * Supports two modes:
 *   - async (default): fire-and-forget, returns status only
 *   - sync: waits for the target's full response (status, headers, body)
 */
import type { CapturedRequest, ForwardResult, ForwardResponse } from './types';

/** Headers to strip when forwarding (hop-by-hop / transport-level). */
const HOP_BY_HOP = new Set([
	'host',
	'connection',
	'keep-alive',
	'transfer-encoding',
	'te',
	'trailer',
	'upgrade',
	'proxy-authorization',
	'proxy-authenticate'
]);

/** Build a clean headers object from the captured request. */
function buildHeaders(req: CapturedRequest): Record<string, string> {
	const headers: Record<string, string> = {};
	if (req.headers) {
		for (const [key, values] of Object.entries(req.headers)) {
			if (!HOP_BY_HOP.has(key.toLowerCase())) {
				headers[key] = Array.isArray(values) ? values[0] : values;
			}
		}
	}
	return headers;
}

/**
 * Forward a captured request to a single target URL (async mode).
 * Returns a ForwardResult with status/latency info.
 */
export async function forwardRequest(
	req: CapturedRequest,
	url: string
): Promise<ForwardResult> {
	const start = performance.now();
	try {
		const headers = buildHeaders(req);
		const hasBody = req.method !== 'GET' && req.method !== 'HEAD' && req.body;

		const res = await fetch(url, {
			method: req.method,
			headers,
			body: hasBody ? req.body : undefined
		});

		return {
			url,
			status: res.status,
			ok: res.ok,
			latency: Math.round(performance.now() - start)
		};
	} catch (err) {
		return {
			url,
			status: null,
			ok: false,
			latency: Math.round(performance.now() - start),
			error: err instanceof Error ? err.message : 'Unknown error'
		};
	}
}

/**
 * Forward a captured request to a single target URL in sync mode.
 * Returns the full response (status, headers, body) for use by the
 * custom response handler.
 */
export async function forwardRequestSync(
	req: CapturedRequest,
	url: string
): Promise<{ result: ForwardResult; response: ForwardResponse | null }> {
	const start = performance.now();
	try {
		const headers = buildHeaders(req);
		const hasBody = req.method !== 'GET' && req.method !== 'HEAD' && req.body;

		const res = await fetch(url, {
			method: req.method,
			headers,
			body: hasBody ? req.body : undefined
		});

		const responseBody = await res.text();
		const responseHeaders: Record<string, string> = {};
		res.headers.forEach((value, key) => {
			responseHeaders[key] = value;
		});

		const latency = Math.round(performance.now() - start);

		return {
			result: {
				url,
				status: res.status,
				ok: res.ok,
				latency
			},
			response: {
				status: res.status,
				headers: responseHeaders,
				body: responseBody,
				content_type: res.headers.get('content-type') ?? undefined
			}
		};
	} catch (err) {
		const latency = Math.round(performance.now() - start);
		return {
			result: {
				url,
				status: null,
				ok: false,
				latency,
				error: err instanceof Error ? err.message : 'Unknown error'
			},
			response: null
		};
	}
}
