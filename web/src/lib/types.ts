/** Types matching Go backend structs (db/queries.go) */

export interface Endpoint {
	id: string;
	slug: string;
	name: string;
	mode: 'server' | 'browser';
	client_id?: string;
	created_at: string;
	config: EndpointConfig;
}

export interface EndpointConfig {
	forward_url?: string;
	forward_mode?: 'sync' | 'async';
	wasm_script?: string;
	// Only JavaScript runs; 'lua' and 'jsonnet' may still be stored by older versions.
	transform_language?: 'javascript' | 'lua' | 'jsonnet';
	/** The script stays saved but doesn't run */
	transform_disabled?: boolean;
	custom_response?: CustomResponse;
	persist_requests?: boolean;
	signature?: SignatureConfig;
	[key: string]: unknown;
}

/** Standard Webhooks verification keys (as sent by Sparrow, Svix, etc). */
export interface SignatureConfig {
	/** Write-only: sent to set or replace the HMAC secret ("" removes it). Never returned. */
	secret?: string;
	/** Returned instead of the secret when one is stored (encrypted). */
	secret_set?: boolean;
	public_key?: string;
	enforce?: boolean;
}

/** Server-side Standard Webhooks verification result for a captured request. */
export interface SignatureResult {
	status: 'verified' | 'invalid' | 'missing';
	scheme?: 'v1' | 'v1a';
	error?: string;
	webhook_id?: string;
}

export interface CustomResponse {
	enabled?: boolean;
	script?: string;
	language?: 'javascript' | 'lua' | 'jsonnet';
}

export interface CapturedRequest {
	id: string;
	endpoint_id: string;
	method: string;
	path: string;
	headers: Record<string, string[]>;
	query?: Record<string, string[]>;
	body?: string | null;
	body_encoding?: 'text' | 'base64';
	content_type: string;
	ip: string;
	size: number;
	created_at: string;
	signature?: SignatureResult;
	/** The forward target's reply, recorded by the server (server mode). */
	forward_result?: ForwardResultData;
	/** The response sent back to the caller, recorded by the server (server mode). */
	response_info?: ResponseInfoData;
}

/** Storage limits (GET /api/limits) and the optional note about a paid plan. */
export interface Limits {
	max_requests_per_endpoint: number;
	max_endpoint_storage_bytes: number;
	max_body_size: number;
	upgrade_note?: string;
}

export interface PaginatedRequests {
	requests: CapturedRequest[];
	total: number;
	limit: number;
	offset: number;
}

/** WebSocket message from hub (hub/hub.go) */
export interface WSMessage {
	type: 'request' | 'response_needed' | 'response_info' | 'forward_result' | 'endpoint_updated' | 'endpoint_deleted';
	data: unknown;
}

/** Data payload for a forward_result message (server/browser reports the forward target's response). */
export interface ForwardResultData {
	request_id: string;
	url: string;
	status_code: number;
	ok: boolean;
	latency_ms: number;
	error?: string;
	response_body?: string;
	response_headers?: Record<string, string>;
	content_type?: string;
	/** Set on a stored result whose response_body was cut to MAX_STORED_RESPONSE_SIZE. */
	response_body_truncated?: boolean;
}

/** Data payload for a response_info message (server tells browser what response was sent). */
export interface ResponseInfoData {
	request_id: string;
	status: number;
	headers?: Record<string, string>;
	body?: string;
	content_type?: string;
	source: 'default' | 'handler' | 'forward_passthrough' | 'signature';
	/** Set on a stored response whose body was cut to MAX_STORED_RESPONSE_SIZE. */
	body_truncated?: boolean;
}

/** Data payload for a response_needed message from the server. */
export interface ResponseNeededData {
	request_id: string;
	request: CapturedRequest;
}

/** Data payload the browser sends back as a response_result message. */
export interface ResponseResultData {
	request_id: string;
	status?: number;
	headers?: Record<string, string>;
	body?: string;
	content_type?: string;
}

export interface ForwardResult {
	url: string;
	status: number | null;
	ok: boolean;
	latency: number;
	error?: string;
}

/** Response received from a sync forward target. */
export interface ForwardResponse {
	status: number;
	headers?: Record<string, string>;
	body?: string;
	content_type?: string;
}

/** HTTP response that was sent back to the webhook caller. */
export interface CapturedResponse {
	status: number;
	headers?: Record<string, string>;
	body?: string;
	content_type?: string;
	/** Where the response came from */
	source: 'default' | 'handler' | 'forward_passthrough' | 'signature';
	/** The stored copy of body was cut to MAX_STORED_RESPONSE_SIZE. */
	body_truncated?: boolean;
}

export interface TransformResult {
	data: Record<string, unknown>;
	ok: boolean;
	error?: string;
	duration: number;
}
