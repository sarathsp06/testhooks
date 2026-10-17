/**
 * WebSocket client with auto-reconnect for live request streaming.
 * Connects to /ws/{slug} on the Go server.
 *
 * Supports bidirectional communication:
 * - Server → Browser: request, response_needed, endpoint_updated, endpoint_deleted
 * - Browser → Server: response_result (for browser-mode custom responses)
 */
import type { WSMessage } from './types';
import { checkAuth, getAuthToken } from './api';
import { AUTH_CHANGED_EVENT } from './auth.svelte';

export type WSStatus = 'connecting' | 'connected' | 'disconnected';
export type MessageHandler = (msg: WSMessage) => void;
export type StatusHandler = (status: WSStatus) => void;

export interface WSClient {
	connect: () => void;
	disconnect: () => void;
	send: (msg: { type: string; data: unknown }) => void;
	onMessage: (handler: MessageHandler) => void;
	onStatus: (handler: StatusHandler) => void;
}

export function createWSClient(slug: string): WSClient {
	let ws: WebSocket | null = null;
	let messageHandlers: MessageHandler[] = [];
	let statusHandlers: StatusHandler[] = [];
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectDelay = 1000;
	let intentionalClose = false;
	let opened = false;
	let listening = false;
	let checkingAuth = false;

	// Reconnect straight away with the new token after sign-in, instead of
	// waiting out the backoff.
	function onAuthChanged() {
		if (intentionalClose) return;
		if (reconnectTimer) clearTimeout(reconnectTimer);
		reconnectTimer = null;
		reconnectDelay = 1000;
		if (ws) {
			ws.onclose = null;
			ws.close();
			ws = null;
		}
		connect();
	}

	function setStatus(status: WSStatus) {
		statusHandlers.forEach((h) => h(status));
	}

	function connect() {
		intentionalClose = false;
		if (!listening) {
			window.addEventListener(AUTH_CHANGED_EVENT, onAuthChanged);
			listening = true;
		}
		if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
			return;
		}

		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${protocol}//${window.location.host}/ws/${slug}`;

		setStatus('connecting');

		// Auth: send token via Sec-WebSocket-Protocol subprotocol.
		// The server expects "auth.<token>" as a subprotocol.
		const token = getAuthToken();
		const protocols = token ? [`auth.${token}`] : undefined;
		ws = new WebSocket(url, protocols);
		opened = false;

		ws.onopen = () => {
			opened = true;
			setStatus('connected');
			reconnectDelay = 1000; // reset backoff
		};

		ws.onmessage = (event) => {
			try {
				const msg: WSMessage = JSON.parse(event.data);
				messageHandlers.forEach((h) => h(msg));
			} catch {
				// ignore malformed messages
			}
		};

		ws.onclose = () => {
			setStatus('disconnected');
			ws = null;
			if (intentionalClose) return;
			// A connection that never opened may have been refused for auth
			// (the server answers the upgrade with 401). Probe the API so the
			// sign-in prompt appears; onAuthChanged reconnects after sign-in.
			if (!opened && !checkingAuth) {
				checkingAuth = true;
				void checkAuth().finally(() => (checkingAuth = false));
			}
			scheduleReconnect();
		};

		ws.onerror = () => {
			// onclose will fire after onerror
		};
	}

	function scheduleReconnect() {
		if (reconnectTimer) clearTimeout(reconnectTimer);
		reconnectTimer = setTimeout(() => {
			reconnectDelay = Math.min(reconnectDelay * 2, 30000);
			connect();
		}, reconnectDelay);
	}

	function disconnect() {
		intentionalClose = true;
		if (listening) {
			window.removeEventListener(AUTH_CHANGED_EVENT, onAuthChanged);
			listening = false;
		}
		if (reconnectTimer) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		if (ws) {
			ws.close();
			ws = null;
		}
		setStatus('disconnected');
	}

	function send(msg: { type: string; data: unknown }) {
		if (ws && ws.readyState === WebSocket.OPEN) {
			ws.send(JSON.stringify(msg));
		}
	}

	return {
		connect,
		disconnect,
		send,
		onMessage: (handler) => {
			messageHandlers.push(handler);
		},
		onStatus: (handler) => {
			statusHandlers.push(handler);
		}
	};
}
