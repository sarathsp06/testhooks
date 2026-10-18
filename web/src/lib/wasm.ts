/**
 * Browser-side script engine: JavaScript via QuickJS (quickjs-emscripten).
 *
 * Scripts run on top of the same prelude the server uses
 * (internal/wasm/prelude.js), so req.json, req.header() and "return null to
 * skip" behave the same in browser mode, the editor preview and on the server.
 *
 * Usage:
 *   await initWasm();
 *   const result = await runTransform(script, request);
 */
import type { QuickJSWASMModule } from 'quickjs-emscripten-core';
import type { CapturedRequest, ForwardResponse } from './types';
import PRELUDE from '../../../internal/wasm/prelude.js?raw';

/** Only JavaScript runs. Lua and Jsonnet were removed; see isLegacyLanguage. */
export type TransformLanguage = 'javascript';

/** True for a language saved by an older version that no longer runs. */
export function isLegacyLanguage(language: string | null | undefined): boolean {
	return !!language && language !== 'javascript';
}

export interface TransformResult {
	/** The request to forward (method, path, headers, query, body, content_type) */
	data: Record<string, unknown>;
	/** True when the script returned null: capture it but don't forward it */
	dropped?: boolean;
	/** Whether the transform succeeded */
	ok: boolean;
	/** Error message if transform failed */
	error?: string;
	/** Execution time in milliseconds */
	duration: number;
}

/** Input passed to the user's transform function */
export interface TransformInput {
	method: string;
	path: string;
	headers: Record<string, string[]>;
	query?: Record<string, string[]>;
	body: string;
	content_type: string;
}

// M-12: Execution timeout for browser-side scripts.
const TIMEOUT_MS = 5000;

let quickJSModule: QuickJSWASMModule | null = null;
let initPromise: Promise<void> | null = null;

/** Load QuickJS (lazy, singleton). The argument is ignored; only JavaScript runs. */
export async function initWasm(_language?: string): Promise<void> {
	if (quickJSModule) return;
	if (initPromise) return initPromise;
	initPromise = (async () => {
		const { getQuickJS } = await import('quickjs-emscripten');
		quickJSModule = await getQuickJS();
	})();
	return initPromise;
}

/** Whether QuickJS is loaded. The argument is ignored; only JavaScript runs. */
export function isWasmReady(_language?: string): boolean {
	return quickJSModule !== null;
}

function toInput(request: CapturedRequest): TransformInput {
	return {
		method: request.method,
		path: request.path,
		headers: request.headers,
		query: request.query,
		body: request.body ?? '',
		content_type: request.content_type
	};
}

function formatError(err: unknown): string {
	if (err && typeof err === 'object') {
		const e = err as { name?: string; message?: string; stack?: string };
		if (e.message) {
			// QuickJS stacks look like "    at transform (script.js:3)"; keep the
			// first frame in the user's script so the line number is visible.
			const frame = e.stack?.split('\n').find((l) => l.includes('script.js'));
			const where = frame?.match(/script\.js:(\d+)/);
			return `${e.name ?? 'Error'}: ${e.message}${where ? ` (line ${where[1]})` : ''}`;
		}
		return JSON.stringify(err);
	}
	return String(err);
}

/**
 * Evaluate the prelude, then the user's script, then `call`, in one fresh
 * context. Each is a separate eval so error line numbers match the script.
 * Returns the string value of `call`.
 */
function evaluate(script: string, call: string): { ok: true; value: string } | { ok: false; error: string } {
	if (!quickJSModule) return { ok: false, error: 'QuickJS WASM not initialized.' };

	const vm = quickJSModule.newContext();
	try {
		const deadline = Date.now() + TIMEOUT_MS;
		vm.runtime.setInterruptHandler(() => Date.now() > deadline);

		for (const [code, file] of [
			[PRELUDE, 'prelude.js'],
			[script, 'script.js'],
			[call, 'run.js']
		] as const) {
			const result = vm.evalCode(code, file);
			if (result.error) {
				const err = vm.dump(result.error);
				result.error.dispose();
				const msg = formatError(err);
				return {
					ok: false,
					error: Date.now() > deadline ? `Script took longer than ${TIMEOUT_MS / 1000}s and was stopped.` : msg
				};
			}
			if (file !== 'run.js') {
				result.value.dispose();
				continue;
			}
			const value = vm.dump(result.value);
			result.value.dispose();
			return { ok: true, value: String(value) };
		}
		return { ok: false, error: 'unreachable' };
	} catch (err) {
		return { ok: false, error: err instanceof Error ? err.message : 'Unknown error' };
	} finally {
		vm.dispose();
	}
}

/**
 * Run a transform script against a captured request. The script defines
 * `function transform(req) { ... }` and returns req (changes to req.json are
 * written back to the body) or null to skip forwarding.
 */
export async function runTransform(
	script: string,
	request: CapturedRequest,
	_language?: string
): Promise<TransformResult> {
	const start = performance.now();
	const res = evaluate(script, `__th_runTransform(${JSON.stringify(toInput(request))})`);
	const duration = Math.round(performance.now() - start);
	if (!res.ok) return { data: {}, ok: false, error: res.error, duration };

	const data = JSON.parse(res.value) as Record<string, unknown>;
	if (data.drop === true) return { data: {}, dropped: true, ok: true, duration };
	return { data, ok: true, duration };
}

function validateDefines(script: string, fn: 'transform' | 'handler'): { valid: boolean; error?: string } {
	const res = evaluate(script, `typeof ${fn} === 'function' ? 'ok' : 'Define a function called ${fn}(req).'`);
	if (!res.ok) return { valid: false, error: res.error };
	return res.value === 'ok' ? { valid: true } : { valid: false, error: res.value };
}

/** Check a transform script compiles and defines transform(). */
export async function validateScript(
	script: string,
	_language?: string
): Promise<{ valid: boolean; error?: string }> {
	return validateDefines(script, 'transform');
}

/** Check a response handler script compiles and defines handler(). */
export async function validateResponseHandler(
	script: string,
	_language?: string
): Promise<{ valid: boolean; error?: string }> {
	return validateDefines(script, 'handler');
}

/** Script languages offered in the UI. */
export const SUPPORTED_LANGUAGES: { id: TransformLanguage; label: string; ext: string }[] = [
	{ id: 'javascript', label: 'JavaScript', ext: 'js' }
];

/** Starting transform script. */
export const DEFAULT_SCRIPTS: Record<TransformLanguage, string> = {
	javascript: `// Runs for each request before it's forwarded.
// req.json is the parsed body when it's JSON (null otherwise).
function transform(req) {
  if (req.json) {
    req.json.received_at = new Date().toISOString();
  }
  return req;
}

// Return null to skip a request (it's still captured).`
};

/** What scripts can use on req. */
export const REQUEST_EXAMPLES: Record<TransformLanguage, string> = {
	javascript: `// What transform(req) gets:
//   req.method              "POST"
//   req.path                "/events"
//   req.query               { "key": ["value"] }
//   req.headers             { "Content-Type": ["application/json"] }
//   req.body                the raw body as a string
//   req.json                the body parsed as JSON, or null
//   req.content_type        "application/json"
//
//   req.header("name")           first value of a header, any case
//   req.setHeader("name", value) replace a header
//   req.removeHeader("name")     delete a header
//
// Return req to forward it (changes to req.json are written to the body),
// or null to skip it.`
};

/**
 * Format a captured request as the `req` a script would get, as JS comments.
 * Falls back to REQUEST_EXAMPLES when no request is given.
 */
export function formatRequestReference(
	request: CapturedRequest | null | undefined,
	_language?: string
): string {
	if (!request) return REQUEST_EXAMPLES.javascript;

	const input = toInput(request);
	let json: unknown = null;
	try {
		json = input.body ? JSON.parse(input.body) : null;
	} catch {
		json = null;
	}
	// Truncate the body to keep the reference readable.
	const body = input.body.length > 200 ? input.body.slice(0, 200) + '...' : input.body;
	const display = json === null ? { ...input, body } : { ...input, body, json };
	const lines = JSON.stringify(display, null, 2).split('\n');
	return `// The \`req\` this request would give your script:\n` + lines.map((l) => `// ${l}`).join('\n');
}

// ---------------------------------------------------------------------------
// Response handler: script-based custom responses
// ---------------------------------------------------------------------------

export interface ResponseHandlerResult {
	/** Response to return to the webhook sender */
	response: {
		status?: number;
		headers?: Record<string, string>;
		body?: string;
		content_type?: string;
	};
	/** Whether the handler succeeded */
	ok: boolean;
	/** Error message if handler failed */
	error?: string;
	/** Execution time in milliseconds */
	duration: number;
}

/** Starting response handler script. */
export const DEFAULT_RESPONSE_SCRIPTS: Record<TransformLanguage, string> = {
	javascript: `// Decides what the sender gets back. Return { status, headers, body }.
// With sync forwarding on, req.forward_response has the target's
// { status, headers, body, content_type }.
function handler(req) {
  return {
    status: 200,
    headers: { "X-Powered-By": "Testhooks" },
    body: JSON.stringify({ ok: true, received: req.method })
  };
}`
};

/** The response object a handler returns. */
export const RESPONSE_HANDLER_EXAMPLES: Record<TransformLanguage, string> = {
	javascript: `// handler(req) gets the same req as transform() (after the transform),
// and returns the response to send back:
// {
//   status: 200,                        // default 200
//   headers: { "X-Custom": "value" },   // flat key-value
//   body: '{"ok": true}'                // a string
// }
//
// With sync forwarding on, req.forward_response is the target's reply:
// { status: 200, headers: { ... }, body: "...", content_type: "..." }
//
// Example: pass the forward target's reply through
// function handler(req) {
//   if (req.forward_response) {
//     return { status: req.forward_response.status, body: req.forward_response.body };
//   }
//   return { status: 200, body: '{"ok": true}' };
// }`
};

/**
 * Run a response handler script (`function handler(req) { ... }`). Pass the
 * forward target's reply as `forwardResponse` to expose req.forward_response.
 */
export async function runResponseHandler(
	script: string,
	request: CapturedRequest,
	_language?: string,
	forwardResponse?: ForwardResponse | null
): Promise<ResponseHandlerResult> {
	const start = performance.now();
	const input: TransformInput & { forward_response?: ForwardResponse } = toInput(request);
	if (forwardResponse) input.forward_response = forwardResponse;

	const res = evaluate(script, `__th_runHandler(${JSON.stringify(input)})`);
	const duration = Math.round(performance.now() - start);
	if (!res.ok) return { response: {}, ok: false, error: res.error, duration };

	const parsed = JSON.parse(res.value);
	if (parsed === null) {
		return { response: {}, ok: false, error: 'handler() returned nothing. Return { status, body }.', duration };
	}
	return { response: parsed, ok: true, duration };
}
