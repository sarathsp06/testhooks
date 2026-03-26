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
	transform_language?: 'javascript' | 'lua' | 'jsonnet';
	custom_response?: CustomResponse;
	persist_requests?: boolean;
	[key: string]: unknown;
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
}

/** Data payload for a response_info message (server tells browser what response was sent). */
export interface ResponseInfoData {
	request_id: string;
	status: number;
	headers?: Record<string, string>;
	body?: string;
	content_type?: string;
	source: 'default' | 'handler' | 'forward_passthrough';
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
	source: 'default' | 'handler' | 'forward_passthrough';
}

export interface TransformResult {
	data: Record<string, unknown>;
	ok: boolean;
	error?: string;
	duration: number;
}
