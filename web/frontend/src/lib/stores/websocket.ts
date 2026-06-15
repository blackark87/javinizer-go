import { writable } from 'svelte/store';
import { browser } from '$app/environment';
import type { ProgressMessage } from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';

// Build WebSocket URL dynamically from browser location
// This works for both local dev (localhost:8080) and Docker (any host)
// Converts http -> ws and https -> wss automatically
function getWebSocketURL(): string {
	if (!browser) {
		// During SSR, return a placeholder (won't be used)
		return 'ws://localhost:8080/ws/progress';
	}
	// Replace http/https with ws/wss and append the WebSocket path
	return location.origin.replace(/^http/, 'ws') + '/ws/progress';
}

interface WebSocketState {
	connected: boolean;
	lastCloseCode?: number;
	lastCloseReason?: string;
	lastCloseWasClean?: boolean;
	reconnectAttempts: number;
	nextReconnectAt?: number;
	lastConnectedAt?: number;
	lastDisconnectedAt?: number;
	messages: ProgressMessage[];
	messagesByFile: Record<string, Record<string, ProgressMessage>>; // Latest message per file per job (job_id -> file_path -> message)
	error?: string;
}

const MAX_MESSAGES = 200;
const INITIAL_RECONNECT_DELAY_MS = 1000;
const MAX_RECONNECT_DELAY_MS = 30000;
const ERROR_TOAST_RATE_LIMIT_MS = 10000;

function getReconnectDelay(attempt: number): number {
	const exponentialDelay = Math.min(
		INITIAL_RECONNECT_DELAY_MS * 2 ** Math.max(attempt - 1, 0),
		MAX_RECONNECT_DELAY_MS
	);
	const jitter = exponentialDelay * 0.2 * Math.random();
	return Math.round(exponentialDelay + jitter);
}

function getCloseMessage(event: CloseEvent, reconnectDelay?: number): string {
	const reason = event.reason ? `: ${event.reason}` : '';
	const reconnectMessage = reconnectDelay
		? ` Reconnecting in ${Math.ceil(reconnectDelay / 1000)}s.`
		: '';
	return `WebSocket disconnected (code ${event.code}${reason}).${reconnectMessage}`;
}

function createWebSocketStore() {
	const { subscribe, set, update } = writable<WebSocketState>({
		connected: false,
		messages: [],
		messagesByFile: {},
		reconnectAttempts: 0
	});

	let ws: WebSocket | null = null;
	let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
	let shouldReconnect = false;
	let lastErrorToastTime = 0;

	function connect() {
		if (!browser) {
			console.warn('WebSocket connection attempted during SSR, skipping');
			return;
		}

		shouldReconnect = true;

		if (reconnectTimeout) {
			clearTimeout(reconnectTimeout);
			reconnectTimeout = null;
		}

		if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
			return;
		}

		const wsUrl = getWebSocketURL();

		try {
			ws = new WebSocket(wsUrl);

			ws.onopen = () => {
				update((state) => ({
					...state,
					connected: true,
					error: undefined,
					reconnectAttempts: 0,
					nextReconnectAt: undefined,
					lastConnectedAt: Date.now()
				}));
			};

			ws.onclose = (event) => {
				ws = null;
				const disconnectedAt = Date.now();

				if (!shouldReconnect) {
					update((state) => ({
						...state,
						connected: false,
						nextReconnectAt: undefined,
						lastCloseCode: event.code,
						lastCloseReason: event.reason,
						lastCloseWasClean: event.wasClean,
						lastDisconnectedAt: disconnectedAt
					}));
					return;
				}

				let reconnectDelay = 0;
				let reconnectAttempt = 0;
				let nextReconnectAt: number | undefined;

				update((state) => {
					reconnectAttempt = state.reconnectAttempts + 1;
					reconnectDelay = getReconnectDelay(reconnectAttempt);
					nextReconnectAt = disconnectedAt + reconnectDelay;
					return {
						...state,
						connected: false,
						reconnectAttempts: reconnectAttempt,
						nextReconnectAt,
						lastCloseCode: event.code,
						lastCloseReason: event.reason,
						lastCloseWasClean: event.wasClean,
						lastDisconnectedAt: disconnectedAt,
						error: getCloseMessage(event, reconnectDelay)
					};
				});

				const now = Date.now();
				if (now - lastErrorToastTime > ERROR_TOAST_RATE_LIMIT_MS) {
					toastStore.error(getCloseMessage(event, reconnectDelay));
					lastErrorToastTime = now;
				}

				reconnectTimeout = setTimeout(() => {
					reconnectTimeout = null;
					connect();
				}, reconnectDelay);
			};

			ws.onerror = (error) => {
				console.error('WebSocket error:', error);
				const now = Date.now();
				if (now - lastErrorToastTime > ERROR_TOAST_RATE_LIMIT_MS) {
					toastStore.error('WebSocket connection error. Reconnecting if the server is available.');
					lastErrorToastTime = now;
				}
				update((state) => ({ ...state, error: 'WebSocket connection error' }));
			};

			ws.onmessage = (event) => {
				try {
					const message: ProgressMessage = JSON.parse(event.data);
					update((state) => {
						const newMessagesByFile = { ...state.messagesByFile };
						if (message.file_path && message.job_id) {
							newMessagesByFile[message.job_id] = {
								...(newMessagesByFile[message.job_id] || {}),
								[message.file_path]: message,
							};
						}
						return {
							...state,
							messages: [...state.messages.slice(-(MAX_MESSAGES - 1)), message],
							messagesByFile: newMessagesByFile
						};
					});
				} catch (error) {
					console.error('Failed to parse WebSocket message:', error);
					toastStore.error('Failed to process server message');
				}
			};
		} catch (error) {
			console.error('Failed to create WebSocket:', error);
			toastStore.error('Failed to connect to server');
			update((state) => ({ ...state, error: 'Failed to create WebSocket connection' }));
		}
	}

	function disconnect() {
		shouldReconnect = false;

		if (reconnectTimeout) {
			clearTimeout(reconnectTimeout);
			reconnectTimeout = null;
		}

		if (ws) {
			ws.onclose = null;
			ws.onerror = null;
			ws.onopen = null;
			ws.onmessage = null;
			ws.close();
			ws = null;
		}

		set({
			connected: false,
			messages: [],
			messagesByFile: {},
			reconnectAttempts: 0,
			nextReconnectAt: undefined,
			lastDisconnectedAt: Date.now()
		});
	}

	function clearMessages() {
		update((state) => ({ ...state, messages: [], messagesByFile: {} }));
	}

	function clearJobMessages(jobId: string) {
		update((state) => {
			const newMessagesByFile = { ...state.messagesByFile };
			delete newMessagesByFile[jobId];
			return { ...state, messagesByFile: newMessagesByFile };
		});
	}

	return {
		subscribe,
		connect,
		disconnect,
		clearMessages,
		clearJobMessages
	};
}

export const websocketStore = createWebSocketStore();
