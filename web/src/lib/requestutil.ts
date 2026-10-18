/** Small helpers for showing and re-sending captured requests. */
import type { CapturedRequest } from './types';
import { getWebhookURL } from './utils';

export function queryString(req: CapturedRequest): string {
	const params = new URLSearchParams();
	for (const [k, vs] of Object.entries(req.query ?? {})) for (const v of vs) params.append(k, v);
	const q = params.toString();
	return q ? `?${q}` : '';
}

/** The body as text, or '' for binary bodies. */
export function bodyText(req: CapturedRequest): string {
	return req.body_encoding === 'base64' ? '' : (req.body ?? '');
}

export function parseJSON(text: string): unknown {
	if (!text) return undefined;
	try {
		return JSON.parse(text);
	} catch {
		return undefined;
	}
}

/** A short label for the event, from common payload fields. */
export function eventLabel(req: CapturedRequest): string {
	if (req.body_encoding === 'base64') return 'binary body';
	const text = bodyText(req);
	if (!text) return 'no body';
	const json = parseJSON(text) as Record<string, unknown> | undefined;
	if (json && typeof json === 'object') {
		for (const key of ['type', 'event', 'event_type', 'action', 'topic']) {
			const v = json[key];
			if (typeof v === 'string' && v) return v;
		}
		return 'JSON body';
	}
	return req.content_type?.split(';')[0] || 'text body';
}

export function bodyKind(req: CapturedRequest): string {
	if (req.body_encoding === 'base64') return 'binary body';
	const text = bodyText(req);
	if (!text) return 'no body';
	return parseJSON(text) !== undefined ? 'JSON body' : 'text body';
}

export function ago(iso: string): string {
	const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
	if (s < 60) return `${s} s ago`;
	if (s < 3600) return `${Math.round(s / 60)} min ago`;
	if (s < 86400) return `${Math.round(s / 3600)} h ago`;
	return new Date(iso).toLocaleDateString();
}

/** "Today 10:42:07", or a date and time for older requests. */
export function when(iso: string): string {
	const d = new Date(iso);
	const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
	return d.toDateString() === new Date().toDateString() ? `Today ${time}` : `${d.toLocaleDateString()} ${time}`;
}

// M-11: escape for single-quoted shell arguments.
function sh(s: string): string {
	return s.replace(/'/g, "'\\''");
}

const SKIP = new Set(['host', 'content-length', 'connection']);

export function curlFor(req: CapturedRequest, slug: string): string {
	let cmd = `curl -X ${sh(req.method)}`;
	for (const [k, vs] of Object.entries(req.headers ?? {})) {
		if (SKIP.has(k.toLowerCase())) continue;
		cmd += ` \\\n  -H '${sh(k)}: ${sh(String(vs[0] ?? ''))}'`;
	}
	const body = bodyText(req);
	if (body) cmd += ` \\\n  -d '${sh(body)}'`;
	return cmd + ` \\\n  '${sh(getWebhookURL(slug) + req.path + queryString(req))}'`;
}

/** Send the request to the endpoint again. */
export async function replay(req: CapturedRequest, slug: string): Promise<number> {
	const headers: Record<string, string> = {};
	for (const [k, vs] of Object.entries(req.headers ?? {})) {
		if (!SKIP.has(k.toLowerCase())) headers[k] = vs[0] ?? '';
	}
	const res = await fetch(getWebhookURL(slug) + req.path + queryString(req), {
		method: req.method,
		headers,
		body: ['GET', 'HEAD'].includes(req.method) ? undefined : bodyText(req) || undefined
	});
	return res.status;
}

const STATUS_TEXT: Record<number, string> = {
	200: 'OK', 201: 'Created', 202: 'Accepted', 204: 'No Content', 301: 'Moved', 302: 'Found',
	400: 'Bad Request', 401: 'Unauthorized', 403: 'Forbidden', 404: 'Not Found', 409: 'Conflict',
	418: "I'm a teapot", 422: 'Unprocessable', 429: 'Too Many Requests', 500: 'Server Error',
	502: 'Bad Gateway', 503: 'Unavailable', 504: 'Gateway Timeout'
};

export function statusLine(code: number): string {
	return STATUS_TEXT[code] ? `${code} ${STATUS_TEXT[code]}` : String(code);
}

export function hostOf(url: string): string {
	try {
		return new URL(url).host;
	} catch {
		return url;
	}
}

/** URL without the scheme, as shown in the pipeline. */
export function shortURL(url: string): string {
	return url.replace(/^https?:\/\//, '').replace(/\/$/, '');
}

function esc(s: string): string {
	return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Pretty JSON as escaped HTML with key, string and number colors. */
export function highlightJSON(pretty: string): string {
	return esc(pretty).replace(
		/("(?:\\.|[^"\\])*")(\s*:)?|\b(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b|\b(true|false|null)\b/g,
		(m, str, colon, num, lit) => {
			if (str) return colon ? `<span class="json-key">${str}</span>${colon}` : `<span class="json-str">${str}</span>`;
			if (num) return `<span class="json-num">${num}</span>`;
			if (lit) return `<span class="json-num">${lit}</span>`;
			return m;
		}
	);
}
