/**
 * Sample requests for trying transforms and response handlers without
 * sending a real webhook. Shapes are generic, not copies of any service's
 * payloads.
 */
import type { CapturedRequest } from './types';

export interface SampleDraft {
	method: string;
	path: string;
	/** One "Name: value" per line */
	headers: string;
	body: string;
}

export const SAMPLE_PRESETS: { id: string; label: string; draft: SampleDraft }[] = [
	{
		id: 'json-event',
		label: 'JSON event',
		draft: {
			method: 'POST',
			path: '/events',
			headers: 'Content-Type: application/json\nX-Event-Type: invoice.paid\nX-Request-Id: req_8f2k1',
			body: JSON.stringify(
				{
					id: 'evt_1Q2w3E',
					type: 'invoice.paid',
					created: 1760781727,
					data: { invoice_id: 'inv_42', amount: 4999, currency: 'usd', customer: { id: 'cus_7', email: 'ada@example.com' } },
					internal_id: 'tmp-2231'
				},
				null,
				2
			)
		}
	},
	{
		id: 'push-event',
		label: 'Push event',
		draft: {
			method: 'POST',
			path: '/hooks/repo',
			headers: 'Content-Type: application/json\nX-Event: push\nUser-Agent: example-hooks/1.0',
			body: JSON.stringify(
				{
					ref: 'refs/heads/main',
					repository: { name: 'shop', owner: 'acme' },
					commits: [
						{ id: 'a1b2c3', message: 'Fix checkout total', author: 'ada' },
						{ id: 'd4e5f6', message: 'Bump deps', author: 'grace' }
					]
				},
				null,
				2
			)
		}
	},
	{
		id: 'ping',
		label: 'Ping',
		draft: {
			method: 'POST',
			path: '/events',
			headers: 'Content-Type: application/json\nX-Event-Type: ping',
			body: '{\n  "type": "ping"\n}'
		}
	},
	{
		id: 'form',
		label: 'Form post',
		draft: {
			method: 'POST',
			path: '/forms/contact',
			headers: 'Content-Type: application/x-www-form-urlencoded',
			body: 'name=Ada&email=ada%40example.com&message=Hello'
		}
	},
	{
		id: 'get',
		label: 'GET, no body',
		draft: { method: 'GET', path: '/health?verbose=1', headers: 'Accept: */*', body: '' }
	}
];

/** Parse "Name: value" lines into the headers shape scripts get. */
export function parseHeaderLines(text: string): Record<string, string[]> {
	const headers: Record<string, string[]> = {};
	for (const line of text.split('\n')) {
		const i = line.indexOf(':');
		if (i <= 0) continue;
		const name = line.slice(0, i).trim();
		const value = line.slice(i + 1).trim();
		if (!name) continue;
		(headers[name] ??= []).push(value);
	}
	return headers;
}

/** Turn a captured request's headers back into editable lines. */
export function headerLines(headers: Record<string, string[]>): string {
	return Object.entries(headers)
		.flatMap(([name, values]) => values.map((v) => `${name}: ${v}`))
		.join('\n');
}

/** Build the request a script would get from a sample draft. */
export function draftToRequest(draft: SampleDraft): CapturedRequest {
	const [path, qs = ''] = draft.path.split('?');
	const query: Record<string, string[]> = {};
	for (const [k, v] of new URLSearchParams(qs)) (query[k] ??= []).push(v);
	const headers = parseHeaderLines(draft.headers);
	const contentType =
		Object.entries(headers).find(([k]) => k.toLowerCase() === 'content-type')?.[1][0] ?? '';
	return {
		id: 'sample',
		endpoint_id: '',
		method: draft.method || 'POST',
		path: path || '/',
		headers,
		query,
		body: draft.body,
		content_type: contentType,
		ip: '127.0.0.1',
		size: new TextEncoder().encode(draft.body).length,
		created_at: new Date().toISOString()
	};
}

/** Start a draft from a captured request, so it can be edited and rerun. */
export function requestToDraft(req: CapturedRequest): SampleDraft {
	const qs = new URLSearchParams();
	for (const [k, vs] of Object.entries(req.query ?? {})) for (const v of vs) qs.append(k, v);
	const q = qs.toString();
	return {
		method: req.method,
		path: req.path + (q ? `?${q}` : ''),
		headers: headerLines(req.headers),
		body: req.body_encoding === 'base64' ? '' : (req.body ?? '')
	};
}
