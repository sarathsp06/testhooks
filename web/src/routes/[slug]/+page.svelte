<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { listEndpoints, deleteEndpoint } from '$lib/api';
	import { createWSClient, type WSStatus } from '$lib/ws';
	import { getEndpointStore } from '$lib/stores/endpoint.svelte';
	import { getRequestsStore } from '$lib/stores/requests.svelte';
	import { getWebhookURL, copyToClipboard } from '$lib/utils';
	import { initWasm, runTransform, runResponseHandler, SUPPORTED_LANGUAGES, DEFAULT_RESPONSE_SCRIPTS, RESPONSE_HANDLER_EXAMPLES, formatRequestReference, type TransformLanguage } from '$lib/wasm';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import { forwardRequest, forwardRequestSync, forwardRequestWithResponse } from '$lib/forward';
	import { saveRequest as saveToIDB } from '$lib/idb';
	import RequestList from '$lib/components/RequestList.svelte';
	import RequestDetail from '$lib/components/RequestDetail.svelte';
	import ForwardConfig from '$lib/components/ForwardConfig.svelte';
	import TransformConfig from '$lib/components/TransformConfig.svelte';
	import type { CapturedRequest, CapturedResponse, Endpoint, ResponseNeededData, ResponseInfoData, ForwardResponse, ForwardResultData } from '$lib/types';
	import { Copy, Wifi, WifiOff, Trash2, Server, Monitor, Settings, Code2, Download, Inbox, Check, XCircle, AlertTriangle, MessageSquare, BookOpen, AlertCircle } from 'lucide-svelte';
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
	let deleteError = $state<string | null>(null);

	// Transform script state (synced with endpoint config)
	let transformEnabled = $state(false);
	let transformScript = $state('');
	let transformLanguage = $state<TransformLanguage>('javascript');
	let scriptSaved = $state(false);

	// Custom response state (synced with endpoint config)
	let customResponseEnabled = $state(false);
	let customResponseScript = $state('');
	let customResponseLanguage = $state<TransformLanguage>('javascript');
	let showResponseExample = $state(false);
	let showResponseReqRef = $state(false);

	// Resolve the endpoint by slug -> get ID -> load requests
	async function init(s: string) {
		initError = null;
		try {
			const all = await listEndpoints();
			const ep = all.find((e) => e.slug === s);
			if (!ep) {
				endpointStore.clear();
				return;
			}
			endpointStore.set(ep);
			endpoint = ep;
		transformScript = ep.config?.wasm_script ?? '';
		transformLanguage = (ep.config?.transform_language as TransformLanguage) || 'javascript';
		transformEnabled = !!ep.config?.wasm_script;

			// Init custom response state from endpoint config
			const cr = ep.config?.custom_response as Record<string, unknown> | undefined;
			customResponseEnabled = !!cr?.enabled;
			customResponseScript = (cr?.script as string) ?? '';
			customResponseLanguage = ((cr?.language as string) || 'javascript') as TransformLanguage;

			if (ep.mode === 'server') {
				await requestsStore.load(ep.id);
			}

			if (ep.config?.wasm_script || ep.mode === 'browser') {
				initWasm(transformLanguage).catch((err) => {
					console.warn('WASM init failed for transform language:', err);
				});
			}

			// Pre-init WASM for custom response handler language if configured.
			if (cr?.enabled && cr?.script && cr?.language) {
				initWasm(cr.language as TransformLanguage).catch((err) => {
					console.warn('WASM init failed for response handler:', err);
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
						// Step 1: Transform
						if (transformEnabled && transformScript) {
							try {
								const result = await runTransform(transformScript, req, transformLanguage);
								if (result.ok && result.data) {
									req = { ...req, ...result.data } as CapturedRequest;
								}
							} catch (err) {
								console.warn('Live transform error:', err);
							}
						}

						// Step 2: Forward (if configured)
						const fwdUrl = ep.config?.forward_url;
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
					if (cr?.enabled && cr?.script && wsClient) {
						const crLang = (cr.language as TransformLanguage) || 'javascript';
						try {
							await initWasm(crLang);

							let handlerReq = data.request;
							let fwdResponse: ForwardResponse | null = null;

							// Step 1: Transform (if configured)
							if (transformEnabled && transformScript) {
								try {
									const txResult = await runTransform(transformScript, handlerReq, transformLanguage);
									if (txResult.ok && txResult.data) {
										handlerReq = { ...handlerReq, ...txResult.data } as CapturedRequest;
									}
								} catch (err) {
									console.warn('Transform error in response_needed:', err);
								}
							}

							// Step 2: Sync forward (if configured)
							const fwdUrl = ep.config?.forward_url;
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

							// Step 3: Run handler with (optionally) forward response
							const result = await runResponseHandler(cr.script, handlerReq, crLang, fwdResponse);
							const responseData = {
								request_id: data.request_id,
								status: result.response?.status || 200,
								headers: result.response?.headers || {},
								body: result.response?.body || '',
								content_type: result.response?.content_type || ''
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
								source: 'handler'
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

	function handleTransformLanguageChange(lang: TransformLanguage) {
		transformLanguage = lang;
		scriptSaved = false;
	}

	function toggleCustomResponse() {
		customResponseEnabled = !customResponseEnabled;
		scriptSaved = false;
	}

	function handleResponseScriptChange(value: string) {
		customResponseScript = value;
		scriptSaved = false;
	}

	function handleResponseLanguageChange(lang: TransformLanguage) {
		customResponseLanguage = lang;
		customResponseScript = '';
		scriptSaved = false;
	}

	function saveTransformScript() {
		// Use endpointStore.current (reactive) instead of the local `endpoint`
		// variable, which is only assigned once in init() and becomes stale
		// when other config fields (e.g. forward_url) are saved from Settings.
		const ep = endpointStore.current;
		if (!ep) return;
		const scriptToSave = transformEnabled ? transformScript : '';
		const langToSave = transformEnabled ? transformLanguage : undefined;

		// Build custom response config
		const customResponseConfig = customResponseEnabled
			? { enabled: true, script: customResponseScript, language: customResponseLanguage }
			: customResponseScript
				? { enabled: false, script: customResponseScript, language: customResponseLanguage }
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
		customResponseScript = '';
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

{#if endpointStore.loading}
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
		<div class="border-b border-[var(--border)] px-4 py-2.5 flex items-center gap-3 flex-shrink-0">
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

							{#if transformEnabled}
								<TransformConfig
									script={transformScript}
									language={transformLanguage}
									onScriptChange={handleTransformScriptChange}
									onLanguageChange={handleTransformLanguageChange}
									testRequest={requestsStore.selected}
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
											Runs server-side on every inbound request via Wazero (JavaScript only).
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

							{#if customResponseEnabled}
								<div class="flex flex-col gap-2">
									<!-- Language picker -->
									<div class="flex items-center gap-2 flex-wrap">
										<span class="text-[11px] text-[var(--text-muted)]">Language:</span>
										<div class="flex items-center border border-[var(--border)] rounded overflow-hidden">
											{#each SUPPORTED_LANGUAGES as lang}
												<button
													onclick={() => handleResponseLanguageChange(lang.id)}
													class="text-[11px] px-2.5 py-1 transition-colors {customResponseLanguage === lang.id
														? 'bg-[var(--accent)] text-white'
														: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
												>
													{lang.label}
												</button>
											{/each}
										</div>
										{#if !customResponseScript}
											<button
												onclick={() => { customResponseScript = DEFAULT_RESPONSE_SCRIPTS[customResponseLanguage]; scriptSaved = false; }}
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
									</div>

									{#if showResponseExample}
										<pre class="text-[10px] text-[var(--text-muted)] bg-[var(--bg)] border border-[var(--border)] rounded p-2 overflow-x-auto whitespace-pre">{RESPONSE_HANDLER_EXAMPLES[customResponseLanguage]}</pre>
									{/if}

									{#if showResponseReqRef}
										<div class="bg-[var(--bg)] border border-[var(--border)] rounded">
											<div class="px-3 py-2 text-xs font-medium text-[var(--text-muted)] border-b border-[var(--border)] flex items-center gap-1.5">
												<BookOpen class="w-3 h-3" />
												{#if requestsStore.selected}
													<span>Request Object — <span class="text-[var(--accent)]">{requestsStore.selected.method} {requestsStore.selected.path}</span></span>
												{:else}
													<span>Request Object Shape ({SUPPORTED_LANGUAGES.find(l => l.id === customResponseLanguage)?.label})</span>
												{/if}
											</div>
											<pre class="px-3 py-2 text-[11px] font-mono text-[var(--text-muted)] overflow-x-auto max-h-48 leading-relaxed whitespace-pre">{formatRequestReference(requestsStore.selected, customResponseLanguage)}</pre>
											{#if !requestsStore.selected}
												<div class="px-3 py-1.5 text-[10px] text-[var(--text-muted)] italic border-t border-[var(--border)]">
													Select a request to see its actual data here.
												</div>
											{/if}
										</div>
									{/if}

									{#if ep.mode === 'server' && customResponseLanguage !== 'javascript'}
										<div class="flex items-start gap-2 text-[10px] text-[var(--yellow)] bg-yellow-500/5 border border-yellow-500/20 rounded px-3 py-2">
											<AlertCircle class="w-3 h-3 flex-shrink-0 mt-0.5" />
											<p>Server mode only supports JavaScript. {customResponseLanguage === 'lua' ? 'Lua' : 'Jsonnet'} will only run when the browser tab is open.</p>
										</div>
									{/if}

									<CodeEditor
										value={customResponseScript}
										onchange={handleResponseScriptChange}
										placeholder={DEFAULT_RESPONSE_SCRIPTS[customResponseLanguage].split('\n')[0]}
										minHeight="140px"
										language={customResponseLanguage}
									/>
								</div>
							{:else}
								<div class="text-xs text-[var(--text-muted)] italic bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-4 text-center">
									Custom response is disabled. The server returns <code class="text-[10px]">200 OK</code> with the default JSON response. Toggle the switch above to write a handler script.
								</div>
							{/if}

							<!-- Footer: Clear + Save (both Transform and Custom Response) -->
							<div class="mt-4 flex items-center justify-between">
								<div class="flex items-center gap-2">
									{#if !transformEnabled && !customResponseEnabled && !requestsStore.selected}
										<span class="text-xs text-[var(--text-muted)]"></span>
									{:else if (transformEnabled || customResponseEnabled) && !requestsStore.selected}
										<span class="text-xs text-[var(--text-muted)]">Select a request from the left panel to test your scripts.</span>
									{/if}
								</div>
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
	</div>
{/if}
