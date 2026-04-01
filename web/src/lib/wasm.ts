/**
 * Browser-side WASM transform engine.
 *
 * Supports multiple languages:
 *   - JavaScript via QuickJS (quickjs-emscripten)
 *   - Lua via wasmoon
 *   - Jsonnet via tplfa-jsonnet (Go→WASM)
 *
 * Usage:
 *   await initEngine('javascript');
 *   const result = await runTransform('javascript', script, request);
 */
import type { QuickJSWASMModule } from 'quickjs-emscripten-core';
import type { LuaFactory, LuaEngine } from 'wasmoon';
import type { CapturedRequest, ForwardResponse } from './types';

export type TransformLanguage = 'javascript' | 'lua' | 'jsonnet';

export interface TransformResult {
	/** The transformed request data (JSON object) */
	data: Record<string, unknown>;
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

// ---------------------------------------------------------------------------
// JavaScript engine (QuickJS)
// ---------------------------------------------------------------------------

let quickJSModule: QuickJSWASMModule | null = null;
let jsInitPromise: Promise<void> | null = null;

async function initJS(): Promise<void> {
	if (quickJSModule) return;
	if (jsInitPromise) return jsInitPromise;
	jsInitPromise = (async () => {
		const { getQuickJS } = await import('quickjs-emscripten');
		quickJSModule = await getQuickJS();
	})();
	return jsInitPromise;
}

function isJSReady(): boolean {
	return quickJSModule !== null;
}

async function runJS(script: string, request: CapturedRequest): Promise<TransformResult> {
	const start = performance.now();
	// M-12: Execution timeout for browser-side WASM (5 seconds).
	const TIMEOUT_MS = 5000;

	if (!quickJSModule) {
		return { data: {}, ok: false, error: 'QuickJS WASM not initialized.', duration: 0 };
	}

	const vm = quickJSModule.newContext();
	try {
		// M-12: Set interrupt handler to enforce execution timeout.
		const deadline = Date.now() + TIMEOUT_MS;
		vm.runtime.setInterruptHandler(() => Date.now() > deadline);
		const input: TransformInput = {
			method: request.method,
			path: request.path,
			headers: request.headers,
			query: request.query,
			body: request.body ?? '',
			content_type: request.content_type
		};

		const code = `
${script}

(function() {
  var req = ${JSON.stringify(input)};
  if (typeof transform === 'function') {
    var result = transform(req);
    return JSON.stringify(result);
  }
  return JSON.stringify({ error: 'No transform() function defined' });
})()
`;
		const result = vm.evalCode(code);

		if (result.error) {
			const errorVal = vm.dump(result.error);
			result.error.dispose();
			return {
				data: {},
				ok: false,
				error: typeof errorVal === 'object' ? JSON.stringify(errorVal) : String(errorVal),
				duration: Math.round(performance.now() - start)
			};
		}

		const value = vm.dump(result.value);
		result.value.dispose();

		try {
			const parsed = typeof value === 'string' ? JSON.parse(value) : value;
			return { data: parsed, ok: true, duration: Math.round(performance.now() - start) };
		} catch {
			return {
				data: {},
				ok: false,
				error: `Transform returned invalid JSON: ${String(value)}`,
				duration: Math.round(performance.now() - start)
			};
		}
	} catch (err) {
		return {
			data: {},
			ok: false,
			error: err instanceof Error ? err.message : 'Unknown error',
			duration: Math.round(performance.now() - start)
		};
	} finally {
		vm.dispose();
	}
}

async function validateJS(script: string): Promise<{ valid: boolean; error?: string }> {
	if (!quickJSModule) return { valid: false, error: 'QuickJS WASM not initialized' };

	const vm = quickJSModule.newContext();
	try {
		const code = `
${script}
typeof transform === 'function' ? 'ok' : 'No transform() function defined';
`;
		const result = vm.evalCode(code);
		if (result.error) {
			const errorVal = vm.dump(result.error);
			result.error.dispose();
			return {
				valid: false,
				error: typeof errorVal === 'object' ? JSON.stringify(errorVal) : String(errorVal)
			};
		}
		const value = vm.dump(result.value);
		result.value.dispose();
		return value === 'ok' ? { valid: true } : { valid: false, error: String(value) };
	} catch (err) {
		return { valid: false, error: err instanceof Error ? err.message : 'Unknown error' };
	} finally {
		vm.dispose();
	}
}

// ---------------------------------------------------------------------------
// Lua engine (wasmoon)
// ---------------------------------------------------------------------------

let luaFactory: LuaFactory | null = null;
let luaInitPromise: Promise<void> | null = null;

async function initLua(): Promise<void> {
	if (luaFactory) return;
	if (luaInitPromise) return luaInitPromise;
	luaInitPromise = (async () => {
		const { LuaFactory: Factory } = await import('wasmoon');
		luaFactory = new Factory();
	})();
	return luaInitPromise;
}

function isLuaReady(): boolean {
	return luaFactory !== null;
}

async function runLua(script: string, request: CapturedRequest): Promise<TransformResult> {
	const start = performance.now();

	if (!luaFactory) {
		return { data: {}, ok: false, error: 'Lua WASM not initialized.', duration: 0 };
	}

	let engine: LuaEngine | null = null;
	try {
		engine = await luaFactory.createEngine();

		const input: TransformInput = {
			method: request.method,
			path: request.path,
			headers: request.headers,
			query: request.query,
			body: request.body ?? '',
			content_type: request.content_type
		};

		// Provide a json module for encode/decode inside Lua
		const jsonLib = `
local json = {}
function json.decode(str) return _json_decode(str) end
function json.encode(val) return _json_encode(val) end
`;

		// Inject helpers via globals
		engine.global.set('_json_decode_input', JSON.stringify(input));
		engine.global.set('_json_decode', (s: string) => {
			try {
				return JSON.parse(s);
			} catch {
				return null;
			}
		});
		engine.global.set('_json_encode', (val: unknown) => {
			try {
				return JSON.stringify(val);
			} catch {
				return 'null';
			}
		});

		// Execute: load helpers + user script + call transform
		// M-12: Wrap execution with a timeout to prevent runaway Lua scripts.
		const TIMEOUT_MS = 5000;
		const code = `
${jsonLib}

local req = _json_decode(_json_decode_input)

${script}

if type(transform) == "function" then
  local result = transform(req)
  return _json_encode(result)
else
  return '{"error":"No transform() function defined"}'
end
`;

		// M-12: Timeout for Lua execution.
		const resultStr = await Promise.race([
			engine.doString(code),
			new Promise<never>((_, reject) =>
				setTimeout(() => reject(new Error('Lua execution timed out (5s limit)')), TIMEOUT_MS)
			)
		]);

		try {
			const parsed = typeof resultStr === 'string' ? JSON.parse(resultStr) : resultStr;
			if (parsed && parsed.error && Object.keys(parsed).length === 1) {
				return {
					data: {},
					ok: false,
					error: parsed.error,
					duration: Math.round(performance.now() - start)
				};
			}
			return { data: parsed, ok: true, duration: Math.round(performance.now() - start) };
		} catch {
			return {
				data: {},
				ok: false,
				error: `Transform returned invalid JSON: ${String(resultStr)}`,
				duration: Math.round(performance.now() - start)
			};
		}
	} catch (err) {
		return {
			data: {},
			ok: false,
			error: err instanceof Error ? err.message : 'Unknown Lua error',
			duration: Math.round(performance.now() - start)
		};
	} finally {
		engine?.global.close();
	}
}

async function validateLua(script: string): Promise<{ valid: boolean; error?: string }> {
	if (!luaFactory) return { valid: false, error: 'Lua WASM not initialized' };

	let engine: LuaEngine | null = null;
	try {
		engine = await luaFactory.createEngine();

		// Load the script and check that transform is a function
		const code = `
${script}

if type(transform) == "function" then
  return "ok"
else
  return "No transform() function defined"
end
`;
		const result = await engine.doString(code);
		return result === 'ok' ? { valid: true } : { valid: false, error: String(result) };
	} catch (err) {
		return { valid: false, error: err instanceof Error ? err.message : 'Unknown Lua error' };
	} finally {
		engine?.global.close();
	}
}

// ---------------------------------------------------------------------------
// Jsonnet engine (tplfa-jsonnet — Go compiled to WASM)
// ---------------------------------------------------------------------------

interface JsonnetInstance {
	evaluate: (
		code: string,
		extrStrs?: Record<string, string>,
		files?: Record<string, string>
	) => Promise<string>;
}

let jsonnetInstance: JsonnetInstance | null = null;
let jsonnetInitPromise: Promise<void> | null = null;

async function initJsonnet(): Promise<void> {
	if (jsonnetInstance) return;
	if (jsonnetInitPromise) return jsonnetInitPromise;
	jsonnetInitPromise = (async () => {
		// tplfa-jsonnet requires the Go WASM support script (wasm_exec.js) to set up
		// globalThis.Go. We load it dynamically as a side-effect import.
		// Then we load the WASM binary and instantiate the Jsonnet evaluator.
		await import('tplfa-jsonnet/wasm_exec.js');

		// Now globalThis.Go should be available
		const { getJsonnet } = await import('tplfa-jsonnet/jsonnet.js');

		// Fetch the WASM binary — the path resolves to the package's libjsonnet.wasm
		// We use a dynamic fetch with the resolved URL
		const wasmUrl = new URL('tplfa-jsonnet/libjsonnet.wasm', import.meta.url);
		let wasmSource: BufferSource | Promise<Response>;
		try {
			// Try streaming instantiation first (works when served with correct MIME type)
			wasmSource = fetch(wasmUrl.href);
		} catch {
			// Fallback: fetch as buffer
			const response = await fetch(wasmUrl.href);
			wasmSource = await response.arrayBuffer();
		}
		jsonnetInstance = await getJsonnet(wasmSource);
	})();
	return jsonnetInitPromise;
}

function isJsonnetReady(): boolean {
	return jsonnetInstance !== null;
}

async function runJsonnet(script: string, request: CapturedRequest): Promise<TransformResult> {
	const start = performance.now();

	if (!jsonnetInstance) {
		return { data: {}, ok: false, error: 'Jsonnet WASM not initialized.', duration: 0 };
	}

	try {
		const input: TransformInput = {
			method: request.method,
			path: request.path,
			headers: request.headers,
			query: request.query,
			body: request.body ?? '',
			content_type: request.content_type
		};

		// Pass the request as an external variable (stringified JSON).
		// The user's Jsonnet code accesses it via: std.parseJson(std.extVar("req"))
		// We wrap the user script — it must evaluate to a JSON value.
		const extrStrs: Record<string, string> = {
			req: JSON.stringify(input)
		};

		// M-12: Timeout for Jsonnet execution.
		const TIMEOUT_MS = 5000;
		const resultStr = await Promise.race([
			jsonnetInstance.evaluate(script, extrStrs),
			new Promise<never>((_, reject) =>
				setTimeout(() => reject(new Error('Jsonnet execution timed out (5s limit)')), TIMEOUT_MS)
			)
		]);

		try {
			const parsed = JSON.parse(resultStr);
			return { data: parsed, ok: true, duration: Math.round(performance.now() - start) };
		} catch {
			return {
				data: {},
				ok: false,
				error: `Jsonnet returned invalid JSON: ${resultStr}`,
				duration: Math.round(performance.now() - start)
			};
		}
	} catch (err) {
		return {
			data: {},
			ok: false,
			error: err instanceof Error ? err.message : 'Unknown Jsonnet error',
			duration: Math.round(performance.now() - start)
		};
	}
}

async function validateJsonnet(script: string): Promise<{ valid: boolean; error?: string }> {
	if (!jsonnetInstance) return { valid: false, error: 'Jsonnet WASM not initialized' };

	try {
		// Try evaluating with a dummy request to check syntax
		const dummyVars: Record<string, string> = {
			req: JSON.stringify({
				method: 'POST',
				path: '/',
				headers: {},
				query: {},
				body: '{}',
				content_type: 'application/json'
			})
		};
		const result = await jsonnetInstance.evaluate(script, dummyVars);
		// If it evaluates without error, it's valid Jsonnet
		try {
			JSON.parse(result);
			return { valid: true };
		} catch {
			return { valid: false, error: 'Script must evaluate to valid JSON' };
		}
	} catch (err) {
		return { valid: false, error: err instanceof Error ? err.message : 'Unknown Jsonnet error' };
	}
}

// ---------------------------------------------------------------------------
// Public API — language-agnostic
// ---------------------------------------------------------------------------

/**
 * Initialize the WASM engine for the given language (lazy, singleton).
 */
export async function initWasm(language: TransformLanguage = 'javascript'): Promise<void> {
	switch (language) {
		case 'javascript':
			return initJS();
		case 'lua':
			return initLua();
		case 'jsonnet':
			return initJsonnet();
	}
}

/** Check if the engine for the given language is ready. */
export function isWasmReady(language: TransformLanguage = 'javascript'): boolean {
	switch (language) {
		case 'javascript':
			return isJSReady();
		case 'lua':
			return isLuaReady();
		case 'jsonnet':
			return isJsonnetReady();
	}
}

/**
 * Run a user-provided transform script against a captured request.
 *
 * JavaScript scripts must define: function transform(req) { ... }
 * Lua scripts must define:       function transform(req) ... end
 * Jsonnet scripts must evaluate to a JSON object (req available via std.extVar("req"))
 */
export async function runTransform(
	script: string,
	request: CapturedRequest,
	language: TransformLanguage = 'javascript'
): Promise<TransformResult> {
	switch (language) {
		case 'javascript':
			return runJS(script, request);
		case 'lua':
			return runLua(script, request);
		case 'jsonnet':
			return runJsonnet(script, request);
	}
}

/**
 * Validate a transform script (compiles and defines transform()).
 */
export async function validateScript(
	script: string,
	language: TransformLanguage = 'javascript'
): Promise<{ valid: boolean; error?: string }> {
	switch (language) {
		case 'javascript':
			return validateJS(script);
		case 'lua':
			return validateLua(script);
		case 'jsonnet':
			return validateJsonnet(script);
	}
}

/**
 * Validate a custom response handler script (compiles and defines handler()).
 */
export async function validateResponseHandler(
	script: string,
	language: TransformLanguage = 'javascript'
): Promise<{ valid: boolean; error?: string }> {
	switch (language) {
		case 'javascript':
			return validateResponseHandlerJS(script);
		case 'lua':
			return validateResponseHandlerLua(script);
		case 'jsonnet':
			return validateResponseHandlerJsonnet(script);
	}
}

async function validateResponseHandlerJS(script: string): Promise<{ valid: boolean; error?: string }> {
	if (!quickJSModule) return { valid: false, error: 'QuickJS WASM not initialized' };

	const vm = quickJSModule.newContext();
	try {
		const code = `
${script}
typeof handler === 'function' ? 'ok' : 'No handler() function defined';
`;
		const result = vm.evalCode(code);
		if (result.error) {
			const errorVal = vm.dump(result.error);
			result.error.dispose();
			return {
				valid: false,
				error: typeof errorVal === 'object' ? JSON.stringify(errorVal) : String(errorVal)
			};
		}
		const value = vm.dump(result.value);
		result.value.dispose();
		return value === 'ok' ? { valid: true } : { valid: false, error: String(value) };
	} catch (err) {
		return { valid: false, error: err instanceof Error ? err.message : 'Unknown error' };
	} finally {
		vm.dispose();
	}
}

async function validateResponseHandlerLua(script: string): Promise<{ valid: boolean; error?: string }> {
	if (!luaFactory) return { valid: false, error: 'Lua WASM not initialized' };

	let engine: LuaEngine | null = null;
	try {
		engine = await luaFactory.createEngine();

		const code = `
${script}

if type(handler) == "function" then
  return "ok"
else
  return "No handler() function defined"
end
`;
		const result = await engine.doString(code);
		return result === 'ok' ? { valid: true } : { valid: false, error: String(result) };
	} catch (err) {
		return { valid: false, error: err instanceof Error ? err.message : 'Unknown Lua error' };
	} finally {
		engine?.global.close();
	}
}

async function validateResponseHandlerJsonnet(script: string): Promise<{ valid: boolean; error?: string }> {
	if (!jsonnetInstance) return { valid: false, error: 'Jsonnet WASM not initialized' };

	try {
		const dummyVars: Record<string, string> = {
			req: JSON.stringify({
				method: 'POST',
				path: '/',
				headers: {},
				query: {},
				body: '{}',
				content_type: 'application/json'
			})
		};
		const result = await jsonnetInstance.evaluate(script, dummyVars);
		try {
			const parsed = JSON.parse(result);
			if (typeof parsed !== 'object' || parsed === null) {
				return { valid: false, error: 'Script must evaluate to a response object' };
			}
			return { valid: true };
		} catch {
			return { valid: false, error: 'Script must evaluate to valid JSON' };
		}
	} catch (err) {
		return { valid: false, error: err instanceof Error ? err.message : 'Unknown Jsonnet error' };
	}
}

/** List of supported transform languages with display info. */
export const SUPPORTED_LANGUAGES: { id: TransformLanguage; label: string; ext: string }[] = [
	{ id: 'javascript', label: 'JavaScript', ext: 'js' },
	{ id: 'lua', label: 'Lua', ext: 'lua' },
	{ id: 'jsonnet', label: 'Jsonnet', ext: 'jsonnet' }
];

/** Default template script per language. */
export const DEFAULT_SCRIPTS: Record<TransformLanguage, string> = {
	javascript: `// Transform function receives the request and returns a modified version.
// Available fields: method, path, headers, query, body, content_type
function transform(req) {
  const body = JSON.parse(req.body || '{}');
  // Modify body here
  return { ...req, body: JSON.stringify(body) };
}`,
	lua: `-- Transform function receives the request and returns a modified version.
-- Available fields: method, path, headers, query, body, content_type
-- Use json.decode(str) and json.encode(val) for JSON handling.
function transform(req)
  local body = json.decode(req.body or "{}")
  -- Modify body here
  return req
end`,
	jsonnet: `// Jsonnet transform — must evaluate to a JSON object.
// Access the request via: local req = std.parseJson(std.extVar("req"));
// Available fields: method, path, headers, query, body, content_type

local req = std.parseJson(std.extVar("req"));
local body = std.parseJson(req.body);

// Return transformed request
req { body: std.manifestJsonEx(body, "  ") }`
};

/** Request object shape reference for each language. */
export const REQUEST_EXAMPLES: Record<TransformLanguage, string> = {
	javascript: `// The \`req\` object passed to transform():
// {
//   method: "POST",              // HTTP method
//   path: "/webhook/payload",    // Request path
//   headers: {                   // Headers (arrays)
//     "content-type": ["application/json"],
//     "x-custom": ["value1", "value2"]
//   },
//   query: {                     // Query params (arrays)
//     "key": ["value"]
//   },
//   body: "{\\"key\\":\\"value\\"}",  // Raw body string
//   content_type: "application/json"  // Content-Type shortcut
// }`,
	lua: `-- The \`req\` table passed to transform():
-- {
--   method = "POST",              -- HTTP method
--   path = "/webhook/payload",    -- Request path
--   headers = {                   -- Headers (arrays)
--     ["content-type"] = {"application/json"},
--     ["x-custom"] = {"value1", "value2"}
--   },
--   query = {                     -- Query params (arrays)
--     ["key"] = {"value"}
--   },
--   body = '{"key":"value"}',     -- Raw body string
--   content_type = "application/json"  -- Content-Type shortcut
-- }`,
	jsonnet: `// Access request via: local req = std.parseJson(std.extVar("req"));
// {
//   method: "POST",              // HTTP method
//   path: "/webhook/payload",    // Request path
//   headers: {                   // Headers (arrays)
//     "content-type": ["application/json"],
//     "x-custom": ["value1", "value2"]
//   },
//   query: {                     // Query params (arrays)
//     "key": ["value"]
//   },
//   body: '{"key":"value"}',     // Raw body string
//   content_type: "application/json"  // Content-Type shortcut
// }`
};

/**
 * Format a captured request into the `req` object shape that scripts receive,
 * displayed in the language's comment style. Falls back to the static
 * REQUEST_EXAMPLES when no request is provided.
 */
export function formatRequestReference(
	request: CapturedRequest | null | undefined,
	language: TransformLanguage
): string {
	if (!request) return REQUEST_EXAMPLES[language];

	const input: TransformInput = {
		method: request.method,
		path: request.path,
		headers: request.headers,
		query: request.query,
		body: request.body ?? '',
		content_type: request.content_type
	};

	// Truncate body display to keep reference readable
	const bodyDisplay =
		input.body.length > 200 ? input.body.slice(0, 200) + '...' : input.body;
	const displayObj = { ...input, body: bodyDisplay };
	const json = JSON.stringify(displayObj, null, 2);

	switch (language) {
		case 'javascript':
			return `// Actual \`req\` object for the selected request:\n` + json.split('\n').map((l) => `// ${l}`).join('\n');
		case 'lua':
			return `-- Actual \`req\` table for the selected request:\n` + json.split('\n').map((l) => `-- ${l}`).join('\n');
		case 'jsonnet':
			return `// Actual \`req\` object for the selected request:\n// Access via: local req = std.parseJson(std.extVar("req"));\n` + json.split('\n').map((l) => `// ${l}`).join('\n');
	}
}

// ---------------------------------------------------------------------------
// Response Handler — script-based custom response generation
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

/** Default template scripts for response handlers per language. */
export const DEFAULT_RESPONSE_SCRIPTS: Record<TransformLanguage, string> = {
	javascript: `// Response handler receives the (post-transform) request and returns a response object.
// If sync forwarding is configured, req.forward_response is available with
// { status, headers, body, content_type } from the forward target.
// Return: { status, headers, body }
function handler(req) {
  return {
    status: 200,
    headers: { "X-Powered-By": "Testhooks" },
    body: JSON.stringify({ ok: true, received: req.method })
  };
}`,
	lua: `-- Response handler receives the (post-transform) request and returns a response table.
-- If sync forwarding is configured, req.forward_response is available with
-- { status, headers, body, content_type } from the forward target.
-- Return: { status, headers, body }
-- Use json.decode(str) and json.encode(val) for JSON handling.
function handler(req)
  return {
    status = 200,
    headers = { ["X-Powered-By"] = "Testhooks" },
    body = json.encode({ ok = true, received = req.method })
  }
end`,
	jsonnet: `// Jsonnet response handler — must evaluate to a response object.
// Access the request via: local req = std.parseJson(std.extVar("req"));
// If sync forwarding is configured, req.forward_response is available with
// { status, headers, body, content_type } from the forward target.
// Return: { status, headers, body }

local req = std.parseJson(std.extVar("req"));

{
  status: 200,
  headers: { "X-Powered-By": "Testhooks" },
  body: std.manifestJsonEx({ ok: true, received: req.method }, "  ")
}`
};

/** Response handler examples showing the response object shape. */
export const RESPONSE_HANDLER_EXAMPLES: Record<TransformLanguage, string> = {
	javascript: `// The handler(req) function receives the (post-transform) request.
// It must return a response object:
// {
//   status: 200,                   // HTTP status code (default: 200)
//   headers: {                     // Response headers (flat key-value)
//     "Content-Type": "application/json",
//     "X-Custom": "value"
//   },
//   body: '{"ok": true}'           // Response body string
// }
//
// If sync forwarding is configured, req.forward_response is available:
// {
//   status: 200,                   // Forward target's HTTP status
//   headers: { "content-type": "application/json", ... },
//   body: '{"result": ...}',       // Forward target's response body
//   content_type: "application/json"
// }
//
// Example: proxy response from forward target
// function handler(req) {
//   if (req.forward_response) {
//     return {
//       status: req.forward_response.status,
//       body: req.forward_response.body
//     };
//   }
//   return { status: 200, body: '{"ok": true}' };
// }`,
	lua: `-- The handler(req) function receives the (post-transform) request.
-- It must return a response table:
-- {
--   status = 200,                   -- HTTP status code (default: 200)
--   headers = {                     -- Response headers (flat key-value)
--     ["Content-Type"] = "application/json",
--     ["X-Custom"] = "value"
--   },
--   body = '{"ok": true}'           -- Response body string
-- }
--
-- If sync forwarding is configured, req.forward_response is available:
-- {
--   status = 200,                   -- Forward target's HTTP status
--   headers = { ["content-type"] = "application/json", ... },
--   body = '{"result": ...}',       -- Forward target's response body
--   content_type = "application/json"
-- }
--
-- Example: proxy response from forward target
-- function handler(req)
--   if req.forward_response then
--     return { status = req.forward_response.status, body = req.forward_response.body }
--   end
--   return { status = 200, body = json.encode({ ok = true }) }
-- end`,
	jsonnet: `// Jsonnet response handler — must evaluate to a response object:
// {
//   status: 200,                   // HTTP status code (default: 200)
//   headers: {                     // Response headers (flat key-value)
//     "Content-Type": "application/json",
//     "X-Custom": "value"
//   },
//   body: '{"ok": true}'           // Response body string
// }
//
// The request is available via: local req = std.parseJson(std.extVar("req"));
//
// If sync forwarding is configured, req.forward_response is available:
// {
//   status: 200,                   // Forward target's HTTP status
//   headers: { "content-type": "application/json", ... },
//   body: '{"result": ...}',       // Forward target's response body
//   content_type: "application/json"
// }
//
// Example: proxy response from forward target
// local req = std.parseJson(std.extVar("req"));
// if std.objectHas(req, "forward_response") then
//   { status: req.forward_response.status, body: req.forward_response.body }
// else
//   { status: 200, body: std.manifestJsonEx({ ok: true }, "  ") }`
};

/**
 * Run a user-provided response handler script against a captured request.
 *
 * JavaScript scripts must define: function handler(req) { ... }
 * Lua scripts must define:       function handler(req) ... end
 * Jsonnet scripts must evaluate to a response object (req available via std.extVar("req"))
 *
 * If sync forwarding is configured, pass the forward target's response as
 * `forwardResponse` — it will be available on `req.forward_response` inside the script.
 */
export async function runResponseHandler(
	script: string,
	request: CapturedRequest,
	language: TransformLanguage = 'javascript',
	forwardResponse?: ForwardResponse | null
): Promise<ResponseHandlerResult> {
	// Reuse the same engines — just call with handler() instead of transform()
	const start = performance.now();

	const input: TransformInput & { forward_response?: ForwardResponse } = {
		method: request.method,
		path: request.path,
		headers: request.headers,
		query: request.query,
		body: request.body ?? '',
		content_type: request.content_type
	};

	// Attach forward response if available (sync forwarding)
	if (forwardResponse) {
		input.forward_response = forwardResponse;
	}

	try {
		switch (language) {
			case 'javascript':
				return await runResponseHandlerJS(script, input, start);
			case 'lua':
				return await runResponseHandlerLua(script, input, start);
			case 'jsonnet':
				return await runResponseHandlerJsonnet(script, input, start);
		}
	} catch (err) {
		return {
			response: {},
			ok: false,
			error: err instanceof Error ? err.message : 'Unknown error',
			duration: Math.round(performance.now() - start)
		};
	}
}

async function runResponseHandlerJS(
	script: string,
	input: TransformInput & { forward_response?: ForwardResponse },
	start: number
): Promise<ResponseHandlerResult> {
	if (!quickJSModule) {
		return { response: {}, ok: false, error: 'QuickJS WASM not initialized.', duration: 0 };
	}

	const vm = quickJSModule.newContext();
	try {
		// M-12: Set interrupt handler to enforce execution timeout (5s).
		const TIMEOUT_MS = 5000;
		const deadline = Date.now() + TIMEOUT_MS;
		vm.runtime.setInterruptHandler(() => Date.now() > deadline);
		const code = `
${script}

(function() {
  var req = ${JSON.stringify(input)};
  if (typeof handler === 'function') {
    var result = handler(req);
    return JSON.stringify(result);
  }
  return JSON.stringify({ error: 'No handler() function defined' });
})()
`;
		const result = vm.evalCode(code);

		if (result.error) {
			const errorVal = vm.dump(result.error);
			result.error.dispose();
			return {
				response: {},
				ok: false,
				error: typeof errorVal === 'object' ? JSON.stringify(errorVal) : String(errorVal),
				duration: Math.round(performance.now() - start)
			};
		}

		const value = vm.dump(result.value);
		result.value.dispose();

		try {
			const parsed = typeof value === 'string' ? JSON.parse(value) : value;
			if (parsed?.error && Object.keys(parsed).length === 1) {
				return { response: {}, ok: false, error: parsed.error, duration: Math.round(performance.now() - start) };
			}
			return { response: parsed, ok: true, duration: Math.round(performance.now() - start) };
		} catch {
			return {
				response: {},
				ok: false,
				error: `Handler returned invalid JSON: ${String(value)}`,
				duration: Math.round(performance.now() - start)
			};
		}
	} finally {
		vm.dispose();
	}
}

async function runResponseHandlerLua(
	script: string,
	input: TransformInput & { forward_response?: ForwardResponse },
	start: number
): Promise<ResponseHandlerResult> {
	if (!luaFactory) {
		return { response: {}, ok: false, error: 'Lua WASM not initialized.', duration: 0 };
	}

	let engine: LuaEngine | null = null;
	try {
		engine = await luaFactory.createEngine();

		const jsonLib = `
local json = {}
function json.decode(str) return _json_decode(str) end
function json.encode(val) return _json_encode(val) end
`;

		engine.global.set('_json_decode_input', JSON.stringify(input));
		engine.global.set('_json_decode', (s: string) => {
			try { return JSON.parse(s); } catch { return null; }
		});
		engine.global.set('_json_encode', (val: unknown) => {
			try { return JSON.stringify(val); } catch { return 'null'; }
		});

		const code = `
${jsonLib}

local req = _json_decode(_json_decode_input)

${script}

if type(handler) == "function" then
  local result = handler(req)
  return _json_encode(result)
else
  return '{"error":"No handler() function defined"}'
end
`;

		// M-12: Timeout for Lua handler execution.
		const TIMEOUT_MS = 5000;
		const resultStr = await Promise.race([
			engine.doString(code),
			new Promise<never>((_, reject) =>
				setTimeout(() => reject(new Error('Lua handler execution timed out (5s limit)')), TIMEOUT_MS)
			)
		]);

		try {
			const parsed = typeof resultStr === 'string' ? JSON.parse(resultStr) : resultStr;
			if (parsed?.error && Object.keys(parsed).length === 1) {
				return { response: {}, ok: false, error: parsed.error, duration: Math.round(performance.now() - start) };
			}
			return { response: parsed, ok: true, duration: Math.round(performance.now() - start) };
		} catch {
			return {
				response: {},
				ok: false,
				error: `Handler returned invalid JSON: ${String(resultStr)}`,
				duration: Math.round(performance.now() - start)
			};
		}
	} finally {
		engine?.global.close();
	}
}

async function runResponseHandlerJsonnet(
	script: string,
	input: TransformInput & { forward_response?: ForwardResponse },
	start: number
): Promise<ResponseHandlerResult> {
	if (!jsonnetInstance) {
		return { response: {}, ok: false, error: 'Jsonnet WASM not initialized.', duration: 0 };
	}

	try {
		const extrStrs: Record<string, string> = {
			req: JSON.stringify(input)
		};

		// M-12: Timeout for Jsonnet handler execution.
		const TIMEOUT_MS = 5000;
		const resultStr = await Promise.race([
			jsonnetInstance.evaluate(script, extrStrs),
			new Promise<never>((_, reject) =>
				setTimeout(() => reject(new Error('Jsonnet handler execution timed out (5s limit)')), TIMEOUT_MS)
			)
		]);

		try {
			const parsed = JSON.parse(resultStr);
			return { response: parsed, ok: true, duration: Math.round(performance.now() - start) };
		} catch {
			return {
				response: {},
				ok: false,
				error: `Jsonnet returned invalid JSON: ${resultStr}`,
				duration: Math.round(performance.now() - start)
			};
		}
	} catch (err) {
		return {
			response: {},
			ok: false,
			error: err instanceof Error ? err.message : 'Unknown Jsonnet error',
			duration: Math.round(performance.now() - start)
		};
	}
}
