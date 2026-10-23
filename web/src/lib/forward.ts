/**
 * Browser-side forwarding: forward a captured request to a single target URL.
 * Uses fetch() from the browser, so localhost targets work.
 *
 * Supports two modes:
 *   - async (default): fire-and-forget, returns status only
 *   - sync: waits for the target's full response (status, headers, body)
 */
import type { CapturedRequest, ForwardResult, ForwardResponse, ForwardResultData } from './types';

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

/**
 * Headers of a forward target's reply that must not be relayed to the
 * webhook sender: the server re-frames the body itself, and the rest are
 * connection-level. Security-sensitive headers are blocked server-side.
 */
const NOT_RELAYED = new Set([
	'content-length',
	'content-encoding',
	'transfer-encoding',
	'connection',
	'keep-alive',
	'date',
	'server'
]);

/**
 * Pick the headers of a forward target's reply to pass back to the webhook
 * sender when the tab relays the reply as-is (sync forwarding, no script).
 */
export function relayHeaders(headers: Record<string, string> | undefined): Record<string, string> {
	const out: Record<string, string> = {};
	for (const [k, v] of Object.entries(headers ?? {})) {
		if (!NOT_RELAYED.has(k.toLowerCase())) out[k] = v;
	}
	return out;
}


/**
 * Turn a fetch() failure into words the request page can show. A browser
 * reports every blocked cross-origin request as the same opaque TypeError
 * ("Failed to fetch"), so this names the likely causes and where to read on.
 */
export function explainForwardError(err: unknown, url: string): string {
	const raw = err instanceof Error ? err.message : 'Unknown error';
	const looksBlocked = err instanceof TypeError || /failed to fetch|networkerror|load failed/i.test(raw);
	if (!looksBlocked) return raw;
	let host = url;
	try {
		host = new URL(url).origin;
	} catch {
		// keep the raw url
	}
	return (
		`The browser could not deliver to ${host}. Usual causes: your app did not answer the CORS preflight ` +
		`(OPTIONS) or did not allow this origin, nothing is listening on that port, or Chrome's local network ` +
		`permission was denied. Fix: see Webhooks to localhost in the docs. Browser said: ${raw}`
	);
}

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
			error: explainForwardError(err, url)
		};
	}
}

/**
 * Forward a captured request and capture the full response (body, headers).
 * Used by browser-mode async forwarding so we can display the forward target's
 * response in the UI. Returns a ForwardResultData matching the WS message shape.
 */
export async function forwardRequestWithResponse(
	req: CapturedRequest,
	url: string
): Promise<ForwardResultData> {
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

		return {
			request_id: req.id,
			url,
			status_code: res.status,
			ok: res.ok,
			latency_ms: Math.round(performance.now() - start),
			response_body: responseBody,
			response_headers: responseHeaders,
			content_type: res.headers.get('content-type') ?? undefined
		};
	} catch (err) {
		return {
			request_id: req.id,
			url,
			status_code: 0,
			ok: false,
			latency_ms: Math.round(performance.now() - start),
			error: explainForwardError(err, url)
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
				error: explainForwardError(err, url)
			},
			response: null
		};
	}
}
