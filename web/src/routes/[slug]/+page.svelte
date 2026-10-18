<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { getEndpointBySlug, deleteEndpoint } from '$lib/api';
	import { createWSClient, type WSStatus } from '$lib/ws';
	import { getEndpointStore } from '$lib/stores/endpoint.svelte';
	import { getRequestsStore } from '$lib/stores/requests.svelte';
	import { getWebhookURL, copyToClipboard } from '$lib/utils';
	import { initWasm, isWasmReady, runTransform, runResponseHandler, validateResponseHandler, isLegacyLanguage, DEFAULT_SCRIPTS, DEFAULT_RESPONSE_SCRIPTS, RESPONSE_HANDLER_EXAMPLES, formatRequestReference } from '$lib/wasm';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import { forwardRequest, forwardRequestSync, forwardRequestWithResponse } from '$lib/forward';
	import { saveRequest as saveToIDB } from '$lib/idb';
	import RequestList from '$lib/components/RequestList.svelte';
	import EndpointWaiting from '$lib/components/EndpointWaiting.svelte';
	import RequestDetail from '$lib/components/RequestDetail.svelte';
	import ForwardConfig from '$lib/components/ForwardConfig.svelte';
	import TransformConfig from '$lib/components/TransformConfig.svelte';
	import TryIt from '$lib/components/TryIt.svelte';
	import type { CapturedRequest, CapturedResponse, Endpoint, ResponseNeededData, ResponseInfoData, ForwardResponse, ForwardResultData } from '$lib/types';
	import { Copy, Wifi, WifiOff, Trash2, Server, Monitor, Settings, Code2, Download, Inbox, Check, XCircle, AlertTriangle, MessageSquare, BookOpen, AlertCircle, Loader2, Play } from 'lucide-svelte';
	import Tooltip from '$lib/components/Tooltip.svelte';

	type RightPanelTab = 'requests' | 'transform' | 'settings';

	const slug = $derived(page.params.slug);
	const endpointStore = getEndpointStore();
	const requestsStore = getRequestsStore();

	let wsStatus = $state<WSStatus>('disconnected');
	let wsClient: ReturnType<typeof createWSClient> | null = null;
	let copied = $state(false);
	let endpoint: Endpoint | null = null;
	let rightTab = $state<RightPanelTab>('requests');
	let initError = $state<string | null>(null);
	// True until the first endpoint lookup settles (it may wait on the sign-in prompt).
	let initializing = $state(true);
	let deleteError = $state<string | null>(null);

	// Transform script state (synced with endpoint config)
	let transformEnabled = $state(false);
	let transformScript = $state('');
	// Set when the saved script is Lua or Jsonnet, which no longer run.
	let transformLegacy = $state<string | null>(null);
	let scriptSaved = $state(false);
	// The request both scripts are tested against: a captured one or a sample.
	let testInput = $state<CapturedRequest | null>(null);

	// Custom response state (synced with endpoint config)
	let customResponseEnabled = $state(false);
	let customResponseScript = $state('');
	let customResponseLegacy = $state<string | null>(null);
	let showResponseExample = $state(false);
	let showResponseReqRef = $state(false);
	let responseValidating = $state(false);
	let responseValidationResult = $state<{ valid: boolean; error?: string } | null>(null);

	// Resolve the endpoint by slug -> get ID -> load requests
	async function init(s: string) {
		initError = null;
		initializing = true;
		try {
			const ep = await getEndpointBySlug(s);
			if (!ep) {
				endpointStore.clear();
				return;
			}
			endpointStore.set(ep);
			endpoint = ep;
		transformScript = ep.config?.wasm_script ?? '';
		transformLegacy = isLegacyLanguage(ep.config?.transform_language) ? (ep.config?.transform_language as string) : null;
		transformEnabled = !!ep.config?.wasm_script;

			// Init custom response state from endpoint config
			const cr = ep.config?.custom_response as Record<string, unknown> | undefined;
			customResponseEnabled = !!cr?.enabled;
			customResponseScript = (cr?.script as string) ?? '';
			customResponseLegacy = isLegacyLanguage(cr?.language as string) ? (cr?.language as string) : null;

			if (ep.mode === 'server' || ep.config?.persist_requests) {
				await requestsStore.load(ep.id);
			}

			if (ep.config?.wasm_script || ep.mode === 'browser' || (cr?.enabled && cr?.script)) {
				initWasm().catch((err) => {
					console.warn('Script engine failed to load:', err);
				});
			}

			wsClient = createWSClient(s);
			wsClient.onStatus((status) => {
				wsStatus = status;
			});
			wsClient.onMessage(async (msg) => {
				if (msg.type === 'request') {
					let req = msg.data as CapturedRequest;

					// Browser-mode pipeline: Transform → Forward → Store
					if (ep.mode === 'browser') {
						// Step 1: Transform (returning null skips forwarding)
						let dropped = false;
						if (transformEnabled && transformScript && !transformLegacy) {
							try {
								await initWasm();
								const result = await runTransform(transformScript, req);
								if (result.ok && result.dropped) {
									dropped = true;
								} else if (result.ok && result.data) {
									req = { ...req, ...result.data } as CapturedRequest;
								} else if (!result.ok) {
									console.warn('Live transform error:', result.error);
								}
							} catch (err) {
								console.warn('Live transform error:', err);
							}
						}

						// Step 2: Forward (if configured). With a custom reply on, the server
						// also sends response_needed for this request, and that flow does the
						// forward; doing it here too would deliver it twice.
						const crActive = !!(ep.config?.custom_response?.enabled && ep.config?.custom_response?.script && !customResponseLegacy);
						const fwdUrl = dropped || crActive ? undefined : ep.config?.forward_url;
						if (fwdUrl) {
							const fwdMode = ep.config?.forward_mode || 'async';
							if (fwdMode === 'async') {
								// Fire-and-forget but capture response for display.
								// Store a pending placeholder so the UI shows "Forwarding..." immediately.
								requestsStore.attachForwardResult(req.id, {
									request_id: req.id,
									url: fwdUrl,
									status_code: -1,
									ok: false,
									latency_ms: 0,
									error: '__pending__'
								});
								forwardRequestWithResponse(req, fwdUrl).then((fwdResult) => {
									requestsStore.attachForwardResult(req.id, fwdResult);
								}).catch((err) => {
									// Always store a result so the UI shows the error instead of nothing.
									const message = err instanceof Error ? err.message : 'Unknown forward error';
									console.warn('Browser forward error:', message);
									requestsStore.attachForwardResult(req.id, {
										request_id: req.id,
										url: fwdUrl,
										status_code: 0,
										ok: false,
										latency_ms: 0,
										error: message
									});
								});
							} else {
								// Sync — wait for response and capture it for display
								try {
									const syncResult = await forwardRequestSync(req, fwdUrl);
									// Store the forward response as a captured response for display
									if (syncResult.response) {
										requestsStore.attachResponse(req.id, {
											status: syncResult.response.status,
											headers: syncResult.response.headers,
											body: syncResult.response.body,
											content_type: syncResult.response.content_type,
											source: 'forward_passthrough'
										});
									}
									// Also store as forward result for the Forward Target Response section
									requestsStore.attachForwardResult(req.id, {
										request_id: req.id,
										url: fwdUrl,
										status_code: syncResult.result.status ?? 0,
										ok: syncResult.result.ok,
										latency_ms: syncResult.result.latency,
										error: syncResult.result.error,
										response_body: syncResult.response?.body,
										response_headers: syncResult.response?.headers,
										content_type: syncResult.response?.content_type
									});
								} catch (err) {
									console.warn('Browser sync forward error:', err);
								}
							}
						}
					}

					requestsStore.prepend(req);

					if (ep.mode === 'browser') {
						saveToIDB(req).catch(() => {});
					}
				} else if (msg.type === 'response_needed') {
					// Server is holding the HTTP response open and waiting for us
					// to run the custom response handler and send back the result.
					// Pipeline: Transform → Forward (sync) → Handler → respond
					const data = msg.data as ResponseNeededData;
					const cr = ep.config?.custom_response;
					if (cr?.enabled && cr?.script && !customResponseLegacy && wsClient) {
						try {
							await initWasm();

							let handlerReq = data.request;
							let fwdResponse: ForwardResponse | null = null;

							// Step 1: Transform (if configured; null skips the forward)
							let dropped = false;
							if (transformEnabled && transformScript && !transformLegacy) {
								try {
									const txResult = await runTransform(transformScript, handlerReq);
									if (txResult.ok && txResult.dropped) {
										dropped = true;
									} else if (txResult.ok && txResult.data) {
										handlerReq = { ...handlerReq, ...txResult.data } as CapturedRequest;
									}
								} catch (err) {
									console.warn('Transform error in response_needed:', err);
								}
							}

							// Step 2: Sync forward (if configured)
							const fwdUrl = dropped ? undefined : ep.config?.forward_url;
							const fwdMode = ep.config?.forward_mode || 'async';
							if (fwdUrl && fwdMode === 'sync') {
								try {
									const syncResult = await forwardRequestSync(handlerReq, fwdUrl);
									fwdResponse = syncResult.response;
									// Capture forward result for display
									requestsStore.attachForwardResult(data.request_id, {
										request_id: data.request_id,
										url: fwdUrl,
										status_code: syncResult.result.status ?? 0,
										ok: syncResult.result.ok,
										latency_ms: syncResult.result.latency,
										error: syncResult.result.error,
										response_body: syncResult.response?.body,
										response_headers: syncResult.response?.headers,
										content_type: syncResult.response?.content_type
									});
								} catch (err) {
									console.warn('Sync forward error in response_needed:', err);
								}
							} else if (fwdUrl && fwdMode === 'async') {
								// Fire-and-forget even in response_needed flow, but capture response
								forwardRequestWithResponse(handlerReq, fwdUrl).then((fwdResult) => {
									// Use the original request_id from the response_needed data
									requestsStore.attachForwardResult(data.request_id, {
										...fwdResult,
										request_id: data.request_id
									});
								}).catch((err) => {
									console.warn('Async forward error in response_needed:', err);
								});
							}

							// Step 3: Run handler with (optionally) forward response. Like the
							// server, a skipped request gets the default reply, not the handler's.
							const result = dropped ? null : await runResponseHandler(cr.script, handlerReq, 'javascript', fwdResponse);
							const responseData = result
								? {
										request_id: data.request_id,
										status: result.response?.status || 200,
										headers: result.response?.headers || {},
										body: result.response?.body || '',
										content_type: result.response?.content_type || ''
									}
								: {
										request_id: data.request_id,
										status: 202,
										headers: {},
										body: JSON.stringify({ status: 'ok', id: data.request_id }),
										content_type: 'application/json'
									};
							wsClient.send({
								type: 'response_result',
								data: responseData
							});

							// Capture the response locally — this is what the server will send.
							requestsStore.attachResponse(data.request_id, {
								status: responseData.status,
								headers: responseData.headers,
								body: responseData.body,
								content_type: responseData.content_type,
								source: result ? 'handler' : 'default'
							});
						} catch (err) {
							console.warn('Custom response handler failed:', err);
							// Script failed — send a fallback so the server doesn't time out.
							const fallbackData = {
								request_id: data.request_id,
								status: 500,
								headers: {},
								body: JSON.stringify({ error: 'Browser response handler failed' }),
								content_type: 'application/json'
							};
							wsClient.send({
								type: 'response_result',
								data: fallbackData
							});
							requestsStore.attachResponse(data.request_id, {
								status: 500,
								headers: {},
								body: fallbackData.body,
								content_type: 'application/json',
								source: 'handler'
							});
						}
					}
				} else if (msg.type === 'response_info') {
					// Server tells us what HTTP response was sent to the webhook caller.
					const info = msg.data as ResponseInfoData;
					requestsStore.attachResponse(info.request_id, {
						status: info.status,
						headers: info.headers,
						body: info.body,
						content_type: info.content_type,
						source: info.source
					});
				} else if (msg.type === 'forward_result') {
					// Server tells us what the forward target responded with.
					const result = msg.data as ForwardResultData;
					requestsStore.attachForwardResult(result.request_id, result);
				}
			});
			wsClient.connect();
		} catch (err) {
			initError = err instanceof Error ? err.message : 'Failed to load endpoint';
			endpointStore.clear();
		} finally {
			initializing = false;
		}
	}

	async function handleCopy() {
		if (!slug) return;
		await copyToClipboard(getWebhookURL(slug));
		copied = true;
		setTimeout(() => (copied = false), 2000);
	}

	async function handleClearAll() {
		if (!endpoint) return;
		await requestsStore.clearAll(endpoint.id);
	}

	function handleTransformScriptChange(script: string) {
		transformScript = script;
		scriptSaved = false;
	}

	// Replace a Lua/Jsonnet script with a JavaScript starting point.
	function rewriteTransformInJS() {
		transformLegacy = null;
		transformScript = DEFAULT_SCRIPTS.javascript;
		scriptSaved = false;
	}

	function rewriteResponseInJS() {
		customResponseLegacy = null;
		customResponseScript = DEFAULT_RESPONSE_SCRIPTS.javascript;
		scriptSaved = false;
		responseValidationResult = null;
	}

	function toggleCustomResponse() {
		customResponseEnabled = !customResponseEnabled;
		scriptSaved = false;
	}

	function handleResponseScriptChange(value: string) {
		customResponseScript = value;
		scriptSaved = false;
	}


	async function handleResponseValidate() {
		if (!isWasmReady()) {
			await initWasm();
		}
		if (!isWasmReady()) return;
		responseValidating = true;
		responseValidationResult = null;
		try {
			responseValidationResult = await validateResponseHandler(
				customResponseScript || DEFAULT_RESPONSE_SCRIPTS.javascript
			);
		} finally {
			responseValidating = false;
		}
	}


	function saveTransformScript() {
		// Use endpointStore.current (reactive) instead of the local `endpoint`
		// variable, which is only assigned once in init() and becomes stale
		// when other config fields (e.g. forward_url) are saved from Settings.
		const ep = endpointStore.current;
		if (!ep) return;
		const scriptToSave = transformEnabled ? transformScript : '';
		// A legacy script keeps its language so it stays skipped (and shown as legacy).
		const langToSave = transformEnabled ? (transformLegacy ?? 'javascript') : undefined;
		const responseLang = customResponseLegacy ?? 'javascript';

		// Build custom response config
		const customResponseConfig = customResponseEnabled
			? { enabled: true, script: customResponseScript, language: responseLang }
			: customResponseScript
				? { enabled: false, script: customResponseScript, language: responseLang }
				: undefined;

		endpointStore.update({
			config: {
				...ep.config,
				wasm_script: scriptToSave || undefined,
				transform_language: langToSave,
				custom_response: customResponseConfig
			}
		});
		scriptSaved = true;
		setTimeout(() => (scriptSaved = false), 2000);
	}

	function clearTransformScript() {
		transformScript = '';
		transformEnabled = false;
		transformLegacy = null;
		customResponseScript = '';
		customResponseLegacy = null;
		customResponseEnabled = false;
		scriptSaved = false;
	}

	async function handleSettingsUpdate(patch: { name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> }) {
		if (!endpoint) return;
		// Intercept delete action from ForwardConfig
		if (patch.config?._delete) {
			deleteError = null;
			try {
				await deleteEndpoint(endpoint.id);
				goto('/');
			} catch (err) {
				deleteError = err instanceof Error ? err.message : 'Failed to delete endpoint';
			}
			return;
		}
		endpointStore.update(patch);
	}

	function exportAllJSON() {
		const data = JSON.stringify(requestsStore.items, null, 2);
		const blob = new Blob([data], { type: 'application/json' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `requests-${slug}.json`;
		a.click();
		URL.revokeObjectURL(url);
	}

	function exportAllCSV() {
		const items = requestsStore.items;
		if (items.length === 0) return;
		const headers = ['id', 'method', 'path', 'content_type', 'ip', 'size', 'created_at'];
		const rows = items.map((r) =>
			headers.map((h) => {
				const val = r[h as keyof CapturedRequest];
				return typeof val === 'string' ? `"${val.replace(/"/g, '""')}"` : String(val ?? '');
			}).join(',')
		);
		const csv = [headers.join(','), ...rows].join('\n');
		const blob = new Blob([csv], { type: 'text/csv' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `requests-${slug}.csv`;
		a.click();
		URL.revokeObjectURL(url);
	}

	onMount(() => {
		if (slug) init(slug);
	});

	onDestroy(() => {
		wsClient?.disconnect();
		requestsStore.reset();
		endpointStore.clear();
	});
</script>

{#if endpointStore.loading || initializing}
	<div class="flex items-center justify-center h-full py-20">
		<div class="text-[var(--text-muted)]">Loading endpoint...</div>
	</div>
{:else if initError}
	<div class="flex items-center justify-center h-full py-20">
		<div class="text-center">
			<AlertTriangle class="w-8 h-8 mx-auto mb-3 text-[var(--red)] opacity-60" />
			<p class="text-[var(--red)] mb-2">{initError}</p>
			<button
				onclick={() => slug && init(slug)}
				class="text-sm text-[var(--accent)] hover:underline mr-3"
			>
				Retry
			</button>
			<a href="/" class="text-[var(--accent)] hover:underline text-sm">Go back</a>
		</div>
	</div>
{:else if !endpointStore.current}
	<div class="flex items-center justify-center h-full py-20">
		<div class="text-center">
			<p class="text-[var(--text-muted)] mb-2">Endpoint not found</p>
			<a href="/" class="text-[var(--accent)] hover:underline text-sm">Go back</a>
		</div>
	</div>
{:else}
	{@const ep = endpointStore.current}
	<div class="flex flex-col h-[calc(100vh-57px)]">
		<!-- Top bar -->
		<div class="border-b border-[var(--border)] px-4 py-2.5 flex flex-wrap items-center gap-3 flex-shrink-0">
			<!-- Left: endpoint info -->
			<div class="flex items-center gap-2 min-w-0">
				<a href="/" class="text-xs text-[var(--text-muted)] hover:text-[var(--text)] transition-colors">&larr;</a>
				<span class="text-sm font-mono font-semibold text-[var(--accent)]">{ep.slug}</span>
				{#if ep.name}
					<span class="text-sm text-[var(--text-muted)] truncate max-w-[200px]">{ep.name}</span>
				{/if}
				<span class="text-[10px] px-1.5 py-0.5 rounded flex items-center gap-1 {ep.mode === 'server' ? 'bg-blue-500/10 text-blue-400' : 'bg-purple-500/10 text-purple-400'}">
					{#if ep.mode === 'server'}<Server class="w-2.5 h-2.5" />{:else}<Monitor class="w-2.5 h-2.5" />{/if}
					{ep.mode}
				</span>
			</div>

			<!-- Center: webhook URL -->
			<div class="flex-1 flex items-center justify-center gap-2">
				<code class="text-xs bg-[var(--bg)] border border-[var(--border)] rounded px-2.5 py-1 text-[var(--text-muted)] max-w-sm truncate">
					{getWebhookURL(ep.slug)}
				</code>
				<button
					onclick={handleCopy}
					class="p-1 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
					title="Copy webhook URL"
				>
					{#if copied}
						<Check class="w-3.5 h-3.5 text-[var(--green)]" />
					{:else}
						<Copy class="w-3.5 h-3.5" />
					{/if}
				</button>
			</div>

			<!-- Right: WS status + actions -->
			<div class="flex items-center gap-2">
				<!-- WS status -->
				<div class="flex items-center gap-1 text-[10px] mr-1">
					{#if wsStatus === 'connected'}
						<span class="w-1.5 h-1.5 rounded-full bg-[var(--green)]"></span>
						<span class="text-[var(--green)]">Live</span>
					{:else if wsStatus === 'connecting'}
						<span class="w-1.5 h-1.5 rounded-full bg-[var(--yellow)] animate-pulse"></span>
						<span class="text-[var(--yellow)]">Connecting</span>
					{:else}
						<span class="w-1.5 h-1.5 rounded-full bg-[var(--red)]"></span>
						<span class="text-[var(--red)]">Disconnected</span>
					{/if}
				</div>

				<!-- Export dropdown -->
				<div class="relative group">
					<button
						class="p-1.5 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
						title="Export requests"
					>
						<Download class="w-3.5 h-3.5" />
					</button>
					<div class="absolute right-0 top-full mt-1 bg-[var(--bg-card)] border border-[var(--border)] rounded shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-10 min-w-[120px]">
						<button
							onclick={exportAllJSON}
							class="w-full text-left px-3 py-1.5 text-xs text-[var(--text-muted)] hover:text-[var(--text)] hover:bg-[var(--bg-hover)] transition-colors"
						>
							Export JSON
						</button>
						<button
							onclick={exportAllCSV}
							class="w-full text-left px-3 py-1.5 text-xs text-[var(--text-muted)] hover:text-[var(--text)] hover:bg-[var(--bg-hover)] transition-colors"
						>
							Export CSV
						</button>
					</div>
				</div>

				<button
					onclick={handleClearAll}
					class="p-1.5 rounded hover:bg-red-500/10 text-[var(--text-muted)] hover:text-[var(--red)] transition-colors"
					title="Clear all requests"
				>
					<Trash2 class="w-3.5 h-3.5" />
				</button>
			</div>
		</div>

		<!-- Error banners -->
		{#if endpointStore.error || requestsStore.error || deleteError}
			<div class="px-4 py-2 border-b border-red-500/20 bg-red-500/5 flex items-center gap-3 flex-shrink-0">
				<AlertTriangle class="w-3.5 h-3.5 text-[var(--red)] flex-shrink-0" />
				<span class="text-xs text-[var(--red)] flex-1">
					{endpointStore.error || requestsStore.error || deleteError}
				</span>
				<button
					onclick={() => { endpointStore.clearError?.(); requestsStore.clearError(); deleteError = null; }}
					class="p-0.5 rounded hover:bg-red-500/20 text-[var(--red)] transition-colors"
				>
					<XCircle class="w-3.5 h-3.5" />
				</button>
			</div>
		{/if}

		<!-- Main content area -->
		{#if requestsStore.items.length === 0 && rightTab === 'requests'}
			<div class="flex-1 overflow-y-auto"><EndpointWaiting endpoint={ep} onOpenTab={(tab) => (rightTab = tab)} /></div>
		{:else}
		<div class="flex flex-1 overflow-hidden">
			<!-- Left sidebar: request list -->
			<div class="w-80 border-r border-[var(--border)] overflow-y-auto flex-shrink-0 flex flex-col">
				<RequestList
					requests={requestsStore.items}
					selectedId={requestsStore.selectedId}
					total={requestsStore.total}
					onSelect={(id) => { requestsStore.select(id); rightTab = 'requests'; }}
				/>
			</div>

			<!-- Right panel -->
			<div class="flex-1 flex flex-col overflow-hidden">
				<!-- Tab bar -->
				<div class="border-b border-[var(--border)] px-4 flex items-center gap-0 flex-shrink-0">
					<button
						onclick={() => (rightTab = 'requests')}
						class="px-3 py-2 text-xs font-medium transition-colors border-b-2 {rightTab === 'requests'
							? 'border-[var(--accent)] text-[var(--accent)]'
							: 'border-transparent text-[var(--text-muted)] hover:text-[var(--text)]'}"
					>
						<span class="flex items-center gap-1.5">
							<Inbox class="w-3.5 h-3.5" />
							Request
						</span>
					</button>
					<button
						onclick={() => (rightTab = 'transform')}
						class="px-3 py-2 text-xs font-medium transition-colors border-b-2 {rightTab === 'transform'
							? 'border-[var(--accent)] text-[var(--accent)]'
							: 'border-transparent text-[var(--text-muted)] hover:text-[var(--text)]'}"
					>
						<span class="flex items-center gap-1.5">
							<Code2 class="w-3.5 h-3.5" />
							Transform
							{#if (transformEnabled && transformScript) || customResponseEnabled}
								<span class="w-1.5 h-1.5 rounded-full bg-[var(--green)]"></span>
							{/if}
							<Tooltip text="Modify the request payload before forwarding, and control the HTTP response sent back to the webhook sender via custom response scripts." />
						</span>
					</button>
					<button
						onclick={() => (rightTab = 'settings')}
						class="px-3 py-2 text-xs font-medium transition-colors border-b-2 {rightTab === 'settings'
							? 'border-[var(--accent)] text-[var(--accent)]'
							: 'border-transparent text-[var(--text-muted)] hover:text-[var(--text)]'}"
					>
						<span class="flex items-center gap-1.5">
							<Settings class="w-3.5 h-3.5" />
							Settings
							<Tooltip text="Configure endpoint mode, forwarding URLs, and other properties." />
						</span>
					</button>
				</div>

				<!-- Tab content -->
				<div class="flex-1 overflow-y-auto">
					{#if rightTab === 'requests'}
						{#if requestsStore.selected}
							<RequestDetail
								request={requestsStore.selected}
								response={requestsStore.getResponse(requestsStore.selected.id)}
								forwardResult={requestsStore.getForwardResult(requestsStore.selected.id)}
								forwardUrl={ep.config?.forward_url}
								endpointSlug={ep.slug}
								onDelete={(id) => requestsStore.remove(id)}
							/>
						{:else}
							<div class="flex items-center justify-center h-full text-[var(--text-muted)]">
								<div class="text-center">
									<Inbox class="w-8 h-8 mx-auto mb-3 opacity-30" />
									<p class="mb-1 text-sm">Select a request to inspect</p>
									<p class="text-xs">Send a webhook to</p>
									<code class="text-xs text-[var(--accent)]">{getWebhookURL(ep.slug)}</code>
								</div>
							</div>
						{/if}
					{:else if rightTab === 'transform'}
						<div class="p-4 max-w-2xl">
							<div class="mb-4 flex items-start justify-between">
								<div>
									<h3 class="text-sm font-semibold mb-1 flex items-center">
										Transform Script
										<Tooltip text="Transforms modify the request before forwarding. Your function receives the request and returns a modified version. Use this to reshape payloads, add/remove headers, or drop requests. This does NOT change the HTTP response to the sender -- use Custom Response below for that." />
									</h3>
									<p class="text-xs text-[var(--text-muted)]">
										{#if ep.mode === 'server'}
											Server-side: runs on every inbound request via Wazero (headless, always-on).
										{:else}
											Browser-side: runs in-browser via WASM when this tab is open.
										{/if}
									</p>
								</div>
								<label class="relative inline-flex items-center cursor-pointer flex-shrink-0 ml-4">
									<input
										type="checkbox"
										bind:checked={transformEnabled}
										onchange={() => { scriptSaved = false; }}
										class="sr-only peer"
									/>
									<div class="w-8 h-4.5 bg-[var(--border)] peer-focus:outline-none rounded-full peer peer-checked:bg-[var(--accent)] transition-colors after:content-[''] after:absolute after:top-[2px] after:start-[2px] after:bg-white after:rounded-full after:h-3.5 after:w-3.5 after:transition-all peer-checked:after:translate-x-full"></div>
									<span class="text-[10px] text-[var(--text-muted)] ml-2">{transformEnabled ? 'Enabled' : 'Disabled'}</span>
								</label>
							</div>

							{#snippet legacyNotice(lang: string, script: string, what: string, onRewrite: () => void)}
								<div class="flex flex-col gap-2">
									<div class="flex items-start gap-2 text-xs text-[var(--yellow)] bg-yellow-500/5 border border-yellow-500/20 rounded px-3 py-2">
										<AlertCircle class="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
										<div class="flex-1">
											<p>This {what} is written in {lang === 'lua' ? 'Lua' : lang === 'jsonnet' ? 'Jsonnet' : lang}. testhooks now runs JavaScript only, so it's skipped until you rewrite it.</p>
											<button onclick={onRewrite} class="mt-2 text-[11px] px-2.5 py-1 rounded border border-[var(--border)] bg-[var(--bg-card)] text-[var(--text)] hover:bg-[var(--bg-hover)] transition-colors">
												Start a JavaScript version
											</button>
										</div>
									</div>
									<pre class="text-[11px] font-mono text-[var(--text-muted)] bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-2 overflow-x-auto max-h-64 whitespace-pre" aria-label="Old {what} (read-only)">{script}</pre>
								</div>
							{/snippet}

							{#if transformEnabled && transformLegacy}
								{@render legacyNotice(transformLegacy, transformScript, 'transform', rewriteTransformInJS)}
							{:else if transformEnabled}
								<TransformConfig
									script={transformScript}
									onScriptChange={handleTransformScriptChange}
									testRequest={testInput}
								/>
							{:else}
								<div class="text-xs text-[var(--text-muted)] italic bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-4 text-center">
									Transform is disabled. Toggle the switch above to enable it and write a script.
								</div>
							{/if}

							<!-- Divider between Transform and Custom Response -->
							<hr class="border-[var(--border)] my-6" />

							<!-- Custom Response Section -->
							<div class="mb-4 flex items-start justify-between">
								<div>
									<h3 class="text-sm font-semibold mb-1 flex items-center gap-1.5">
										<MessageSquare class="w-3.5 h-3.5 text-[var(--text-muted)]" />
										Custom Response
										<Tooltip text="Control the HTTP response sent back to the webhook sender. Your handler function receives the request and returns a response object (status, headers, body). This is independent of Transform -- Transform modifies data for forwarding, Custom Response controls what the sender sees." />
									</h3>
									<p class="text-xs text-[var(--text-muted)]">
										Write a script that receives the request and returns a custom response ({@html '<code class="text-[10px]">{ status, headers, body }</code>'}).
										{#if ep.mode === 'server'}
											Runs server-side on every inbound request.
										{:else}
											Runs in-browser via WASM when this tab is open.
										{/if}
									</p>
								</div>
								<label class="relative inline-flex items-center cursor-pointer flex-shrink-0 ml-4">
									<input
										type="checkbox"
										checked={customResponseEnabled}
										onchange={toggleCustomResponse}
										class="sr-only peer"
									/>
									<div class="w-8 h-4.5 bg-[var(--border)] peer-focus:outline-none rounded-full peer peer-checked:bg-[var(--accent)] transition-colors after:content-[''] after:absolute after:top-[2px] after:start-[2px] after:bg-white after:rounded-full after:h-3.5 after:w-3.5 after:transition-all peer-checked:after:translate-x-full"></div>
									<span class="text-[10px] text-[var(--text-muted)] ml-2">{customResponseEnabled ? 'Enabled' : 'Disabled'}</span>
								</label>
							</div>

							{#if customResponseEnabled && customResponseLegacy}
								{@render legacyNotice(customResponseLegacy, customResponseScript, 'response handler', rewriteResponseInJS)}
							{:else if customResponseEnabled}
								<div class="flex flex-col gap-2">
									<div class="flex items-center gap-2 flex-wrap">
										<span class="text-[11px] text-[var(--text-muted)]">JavaScript</span>
										{#if !customResponseScript}
											<button
												onclick={() => { customResponseScript = DEFAULT_RESPONSE_SCRIPTS.javascript; scriptSaved = false; }}
												class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
											>
												Insert template
											</button>
										{/if}
										<button
											onclick={() => (showResponseExample = !showResponseExample)}
											class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
										>
											{showResponseExample ? 'Hide' : 'Show'} example
										</button>
										<button
											onclick={() => (showResponseReqRef = !showResponseReqRef)}
											class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] transition-colors flex items-center gap-1 {showResponseReqRef ? 'text-[var(--accent)] border-[var(--accent)]/40' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
											title="Show request object reference"
										>
											<BookOpen class="w-3 h-3" />
											Reference
										</button>
										<button
											onclick={handleResponseValidate}
											disabled={responseValidating || !customResponseScript}
											class="text-[11px] px-2.5 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1 disabled:opacity-50"
										>
											{#if responseValidating}
												<Loader2 class="w-3 h-3 animate-spin" />
											{:else}
												<Check class="w-3 h-3" />
											{/if}
											Validate
										</button>
									</div>

									{#if showResponseExample}
										<pre class="text-[10px] text-[var(--text-muted)] bg-[var(--bg)] border border-[var(--border)] rounded p-2 overflow-x-auto whitespace-pre">{RESPONSE_HANDLER_EXAMPLES.javascript}</pre>
									{/if}

									{#if showResponseReqRef}
										<div class="bg-[var(--bg)] border border-[var(--border)] rounded">
											<div class="px-3 py-2 text-xs font-medium text-[var(--text-muted)] border-b border-[var(--border)] flex items-center gap-1.5">
												<BookOpen class="w-3 h-3" />
												{#if testInput}
													<span>Request Object — <span class="text-[var(--accent)]">{testInput.method} {testInput.path}</span></span>
												{:else}
													<span>What your script gets</span>
												{/if}
											</div>
											<pre class="px-3 py-2 text-[11px] font-mono text-[var(--text-muted)] overflow-x-auto max-h-48 leading-relaxed whitespace-pre">{formatRequestReference(testInput)}</pre>
										</div>
									{/if}

									<CodeEditor
										value={customResponseScript}
										onchange={handleResponseScriptChange}
										placeholder={DEFAULT_RESPONSE_SCRIPTS.javascript.split('\n')[0]}
										minHeight="140px"
										language="javascript"
									/>

									{#if responseValidationResult}
										<div class="text-xs px-3 py-2 rounded border {responseValidationResult.valid ? 'border-green-500/30 bg-green-500/5 text-[var(--green)]' : 'border-red-500/30 bg-red-500/5 text-[var(--red)]'}">
											{#if responseValidationResult.valid}
												<span class="flex items-center gap-1"><Check class="w-3 h-3" /> Handler script is valid</span>
											{:else}
												<span class="flex items-center gap-1"><AlertTriangle class="w-3 h-3" /> {responseValidationResult.error}</span>
											{/if}
										</div>
									{/if}
								</div>
							{:else}
								<div class="text-xs text-[var(--text-muted)] italic bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-4 text-center">
									Custom response is disabled. The server returns <code class="text-[10px]">200 OK</code> with the default JSON response. Toggle the switch above to write a handler script.
								</div>
							{/if}

							{#if transformEnabled || customResponseEnabled}
								<div class="mt-6">
									<TryIt
										captured={requestsStore.items}
										selected={requestsStore.selected ?? null}
										slug={ep.slug}
										bind:request={testInput}
										transformScript={transformEnabled && !transformLegacy ? transformScript || DEFAULT_SCRIPTS.javascript : ''}
										handlerScript={customResponseEnabled && !customResponseLegacy ? customResponseScript || DEFAULT_RESPONSE_SCRIPTS.javascript : ''}
										forwardUrl={ep.config?.forward_url}
										forwardMode={ep.config?.forward_mode}
									/>
								</div>
							{/if}

							<!-- Footer: Clear + Save (both Transform and Custom Response) -->
							<div class="mt-4 flex items-center justify-end">
								<div class="flex items-center gap-2">
									{#if (transformEnabled && transformScript) || (customResponseEnabled && customResponseScript)}
										<button
											onclick={clearTransformScript}
											class="text-xs px-3 py-1.5 rounded border border-[var(--border)] hover:border-[var(--red)]/40 text-[var(--text-muted)] hover:text-[var(--red)] transition-colors flex items-center gap-1"
										>
											<XCircle class="w-3 h-3" />
											Clear
										</button>
									{/if}
									{#if scriptSaved}
										<span class="text-xs text-[var(--green)] flex items-center gap-1">
											<Check class="w-3 h-3" /> Saved
										</span>
									{/if}
									<button
										onclick={saveTransformScript}
										class="text-xs px-4 py-1.5 rounded bg-[var(--accent)] hover:bg-[var(--accent-hover)] text-white transition-colors"
									>
										Save Scripts
									</button>
								</div>
							</div>
						</div>
					{:else if rightTab === 'settings'}
						<div class="p-4">
							<div class="mb-4">
								<h3 class="text-sm font-semibold mb-1">Endpoint Settings</h3>
								<p class="text-xs text-[var(--text-muted)]">
									Configure forwarding and endpoint properties.
								</p>
							</div>
							<ForwardConfig endpoint={ep} onUpdate={handleSettingsUpdate} />
						</div>
					{/if}
				</div>
			</div>
		</div>
		{/if}
	</div>
{/if}
