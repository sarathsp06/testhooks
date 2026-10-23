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

/**
 * Provider-shaped samples for the "Send sample" picker on a new endpoint.
 * Shapes follow each provider's public docs closely enough to try a
 * transform or a reply against; they are not signed and carry no secrets.
 */
export interface ProviderSample {
	id: string;
	provider: string;
	label: string;
	path: string;
	headers: Record<string, string>;
	body: unknown;
}

export const PROVIDER_SAMPLES: ProviderSample[] = [
	{
		id: 'generic',
		provider: 'Generic',
		label: 'JSON event',
		path: '',
		headers: { 'Content-Type': 'application/json', 'X-Event-Type': 'invoice.paid' },
		body: { id: 'evt_1Q2w3E', type: 'invoice.paid', created: 1760781727, data: { invoice_id: 'inv_42', amount: 4999, currency: 'usd' } }
	},
	{
		id: 'stripe-payment-intent',
		provider: 'Stripe',
		label: 'payment_intent.succeeded',
		path: '/stripe',
		headers: { 'Content-Type': 'application/json', 'Stripe-Signature': 't=1760781727,v1=sample', 'User-Agent': 'Stripe/1.0 (+https://stripe.com/docs/webhooks)' },
		body: {
			id: 'evt_3Q2w3E2eZvKYlo2C0X1Y2Z3A', object: 'event', api_version: '2024-06-20', created: 1760781727, type: 'payment_intent.succeeded', livemode: false,
			data: { object: { id: 'pi_3Q2w3E2eZvKYlo2C0A1B2C3D', object: 'payment_intent', amount: 2000, currency: 'usd', status: 'succeeded', customer: 'cus_Q1w2E3r4T5y6U7', metadata: { order_id: '6735' } } },
			request: { id: 'req_8f2k1', idempotency_key: null }
		}
	},
	{
		id: 'stripe-invoice-failed',
		provider: 'Stripe',
		label: 'invoice.payment_failed',
		path: '/stripe',
		headers: { 'Content-Type': 'application/json', 'Stripe-Signature': 't=1760781727,v1=sample' },
		body: {
			id: 'evt_1Q2w3E2eZvKYlo2C9Z8Y7X6W', object: 'event', type: 'invoice.payment_failed', created: 1760781727, livemode: false,
			data: { object: { id: 'in_1Q2w3E2eZvKYlo2C', object: 'invoice', amount_due: 4999, currency: 'usd', attempt_count: 2, customer_email: 'ada@example.com', status: 'open' } }
		}
	},
	{
		id: 'github-push',
		provider: 'GitHub',
		label: 'push',
		path: '/github',
		headers: { 'Content-Type': 'application/json', 'X-GitHub-Event': 'push', 'X-GitHub-Delivery': '72d3162e-cc78-11e3-81ab-4c9367dc0958', 'X-Hub-Signature-256': 'sha256=sample', 'User-Agent': 'GitHub-Hookshot/044aadd' },
		body: {
			ref: 'refs/heads/main', before: 'a1b2c3d4e5f60718293a4b5c6d7e8f9012345678', after: 'b2c3d4e5f60718293a4b5c6d7e8f901234567890',
			repository: { id: 1296269, name: 'shop', full_name: 'acme/shop', private: false },
			pusher: { name: 'ada', email: 'ada@example.com' },
			commits: [{ id: 'b2c3d4e5f60718293a4b5c6d7e8f901234567890', message: 'Fix checkout total', author: { name: 'Ada', email: 'ada@example.com' }, added: [], modified: ['src/checkout.ts'], removed: [] }]
		}
	},
	{
		id: 'github-pr',
		provider: 'GitHub',
		label: 'pull_request opened',
		path: '/github',
		headers: { 'Content-Type': 'application/json', 'X-GitHub-Event': 'pull_request', 'X-GitHub-Delivery': '8d1a4c1e-0b1e-4b8e-9c6a-6f1d2e3f4a5b', 'X-Hub-Signature-256': 'sha256=sample' },
		body: {
			action: 'opened', number: 42,
			pull_request: { id: 1, number: 42, title: 'Add retry on 5xx', state: 'open', user: { login: 'grace' }, head: { ref: 'retry-5xx' }, base: { ref: 'main' } },
			repository: { full_name: 'acme/shop' }, sender: { login: 'grace' }
		}
	},
	{
		id: 'shopify-order',
		provider: 'Shopify',
		label: 'orders/create',
		path: '/shopify',
		headers: { 'Content-Type': 'application/json', 'X-Shopify-Topic': 'orders/create', 'X-Shopify-Shop-Domain': 'acme.myshopify.com', 'X-Shopify-Hmac-Sha256': 'sample', 'X-Shopify-Webhook-Id': 'b54557e4-bdd9-4b37-8a5f-bf7d70bcd043', 'X-Shopify-API-Version': '2025-01' },
		body: {
			id: 820982911946154508, email: 'ada@example.com', created_at: '2026-10-18T09:42:07+00:00', total_price: '49.99', currency: 'USD', financial_status: 'paid',
			line_items: [{ id: 866550311766439020, title: 'Webhook Tester Mug', quantity: 1, price: '49.99' }],
			customer: { id: 115310627314723954, first_name: 'Ada', last_name: 'Lovelace' }
		}
	},
	{
		id: 'slack-event',
		provider: 'Slack',
		label: 'message event',
		path: '/slack/events',
		headers: { 'Content-Type': 'application/json', 'X-Slack-Request-Timestamp': '1760781727', 'X-Slack-Signature': 'v0=sample' },
		body: {
			token: 'sample', team_id: 'T0001', api_app_id: 'A0001', type: 'event_callback', event_id: 'Ev0001', event_time: 1760781727,
			event: { type: 'message', channel: 'C2147483705', user: 'U2147483697', text: 'Hello, world', ts: '1760781727.000200' }
		}
	},
	{
		id: 'slack-url-verification',
		provider: 'Slack',
		label: 'url_verification challenge',
		path: '/slack/events',
		headers: { 'Content-Type': 'application/json' },
		body: { token: 'sample', challenge: '3eZbrw1aBm2rZgRNFdxV2595E9CY3gmdALWMmHkvFXO7tYXAYM8P', type: 'url_verification' }
	},
	{
		id: 'twilio-sms',
		provider: 'Twilio',
		label: 'incoming SMS (form post)',
		path: '/twilio/sms',
		headers: { 'Content-Type': 'application/x-www-form-urlencoded', 'X-Twilio-Signature': 'sample' },
		body: 'MessageSid=SM_sample_message_sid&AccountSid=AC_sample_account_sid&From=%2B15017122661&To=%2B15558675310&Body=Hello&NumMedia=0'
	},
	{
		id: 'svix-standard',
		provider: 'Svix / Standard Webhooks',
		label: 'signed-shape message',
		path: '/svix',
		headers: { 'Content-Type': 'application/json', 'webhook-id': 'msg_2KWPBgLlAfxdpx2AI54pPJ85f4W', 'webhook-timestamp': '1760781727', 'webhook-signature': 'v1,c2FtcGxlLW5vdC1hLXJlYWwtc2lnbmF0dXJl' },
		body: { type: 'user.created', timestamp: '2026-10-18T09:42:07Z', data: { id: 'usr_01', email: 'ada@example.com', plan: 'pro' } }
	}
];

/** Group the provider samples for a picker, keeping the array order. */
export function samplesByProvider(): { provider: string; items: ProviderSample[] }[] {
	const groups: { provider: string; items: ProviderSample[] }[] = [];
	for (const s of PROVIDER_SAMPLES) {
		let g = groups.find((x) => x.provider === s.provider);
		if (!g) groups.push((g = { provider: s.provider, items: [] }));
		g.items.push(s);
	}
	return groups;
}
