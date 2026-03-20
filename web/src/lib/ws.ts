/**
 * WebSocket client with auto-reconnect for live request streaming.
 * Connects to /ws/{slug} on the Go server.
 *
 * Supports bidirectional communication:
 * - Server → Browser: request, response_needed, endpoint_updated, endpoint_deleted
 * - Browser → Server: response_result (for browser-mode custom responses)
 */
import type { WSMessage } from './types';

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

	function setStatus(status: WSStatus) {
		statusHandlers.forEach((h) => h(status));
	}

	function connect() {
		intentionalClose = false;
		if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
			return;
		}

		const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${protocol}//${window.location.host}/ws/${slug}`;

		setStatus('connecting');
		ws = new WebSocket(url);

		ws.onopen = () => {
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
			if (!intentionalClose) {
				scheduleReconnect();
			}
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
