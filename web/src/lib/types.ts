/** Types matching Go backend structs (db/queries.go) */

export interface Endpoint {
	id: string;
	slug: string;
	name: string;
	mode: 'server' | 'browser';
	created_at: string;
	config: EndpointConfig;
}

export interface EndpointConfig {
	forward_url?: string;
	forward_mode?: 'sync' | 'async';
	wasm_script?: string;
	transform_language?: 'javascript' | 'lua' | 'jsonnet';
	custom_response?: CustomResponse;
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
	type: 'request' | 'response_needed' | 'endpoint_updated' | 'endpoint_deleted';
	data: unknown;
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

export interface TransformResult {
	data: Record<string, unknown>;
	ok: boolean;
	error?: string;
	duration: number;
}
