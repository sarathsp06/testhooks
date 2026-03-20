<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { listEndpoints, deleteEndpoint } from '$lib/api';
	import { createWSClient, type WSStatus } from '$lib/ws';
	import { getEndpointStore } from '$lib/stores/endpoint.svelte';
	import { getRequestsStore } from '$lib/stores/requests.svelte';
	import { getWebhookURL, copyToClipboard } from '$lib/utils';
	import { initWasm, runTransform, runResponseHandler, type TransformLanguage } from '$lib/wasm';
	import { forwardRequest, forwardRequestSync } from '$lib/forward';
	import { saveRequest as saveToIDB } from '$lib/idb';
	import RequestList from '$lib/components/RequestList.svelte';
	import RequestDetail from '$lib/components/RequestDetail.svelte';
	import ForwardConfig from '$lib/components/ForwardConfig.svelte';
	import TransformConfig from '$lib/components/TransformConfig.svelte';
	import type { CapturedRequest, Endpoint, ResponseNeededData, ForwardResponse } from '$lib/types';
	import { Copy, Wifi, WifiOff, Trash2, Server, Monitor, Settings, Code2, Download, Inbox, Check, XCircle, AlertTriangle } from 'lucide-svelte';
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

			if (ep.mode === 'server') {
				await requestsStore.load(ep.id);
			}

			if (ep.config?.wasm_script || ep.mode === 'browser') {
				initWasm(transformLanguage).catch((err) => {
					console.warn('WASM init failed for transform language:', err);
				});
			}

			// Pre-init WASM for custom response handler language if configured.
			const cr = ep.config?.custom_response;
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
								// Fire-and-forget
								forwardRequest(req, fwdUrl).catch((err) => {
									console.warn('Browser forward error:', err);
								});
							} else {
								// Sync — wait for response (but no handler to pass it to here;
								// handler runs in response_needed flow for browser mode)
								try {
									await forwardRequestSync(req, fwdUrl);
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
								} catch (err) {
									console.warn('Sync forward error in response_needed:', err);
								}
							} else if (fwdUrl && fwdMode === 'async') {
								// Fire-and-forget even in response_needed flow
								forwardRequest(handlerReq, fwdUrl).catch((err) => {
									console.warn('Async forward error in response_needed:', err);
								});
							}

							// Step 3: Run handler with (optionally) forward response
							const result = await runResponseHandler(cr.script, handlerReq, crLang, fwdResponse);
							wsClient.send({
								type: 'response_result',
								data: {
									request_id: data.request_id,
									status: result.response?.status || 200,
									headers: result.response?.headers || {},
									body: result.response?.body || '',
									content_type: result.response?.content_type || ''
								}
							});
						} catch (err) {
							console.warn('Custom response handler failed:', err);
							// Script failed — send a fallback so the server doesn't time out.
							wsClient.send({
								type: 'response_result',
								data: {
									request_id: data.request_id,
									status: 500,
									headers: {},
									body: JSON.stringify({ error: 'Browser response handler failed' }),
									content_type: 'application/json'
								}
							});
						}
					}
				}
			});
			wsClient.connect();
		} catch (err) {
			initError = err instanceof Error ? err.message : 'Failed to load endpoint';
			endpointStore.clear();
		}
	}

	async function handleCopy() {
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

	function saveTransformScript() {
		if (!endpoint) return;
		const scriptToSave = transformEnabled ? transformScript : '';
		const langToSave = transformEnabled ? transformLanguage : undefined;
		endpointStore.update({ config: { ...endpoint.config, wasm_script: scriptToSave || undefined, transform_language: langToSave } });
		scriptSaved = true;
		setTimeout(() => (scriptSaved = false), 2000);
	}

	function clearTransformScript() {
		transformScript = '';
		transformEnabled = false;
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
		init(slug);
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
				onclick={() => init(slug)}
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
							{#if transformEnabled && transformScript}
								<span class="w-1.5 h-1.5 rounded-full bg-[var(--green)]"></span>
							{/if}
							<Tooltip text="Modify the request payload before it is forwarded to downstream URLs. Does not affect the HTTP response sent back to the webhook sender." />
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
							<Tooltip text="Configure endpoint mode, forwarding URLs, custom HTTP responses, and other properties." />
						</span>
					</button>
				</div>

				<!-- Tab content -->
				<div class="flex-1 overflow-y-auto">
					{#if rightTab === 'requests'}
						{#if requestsStore.selected}
							<RequestDetail
								request={requestsStore.selected}
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
										<Tooltip text="Transforms modify the request before forwarding. Your function receives the request and returns a modified version. Use this to reshape payloads, add/remove headers, or drop requests. This does NOT change the HTTP response to the sender -- use Custom Response in Settings for that." />
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

							<div class="mt-4 flex items-center justify-between">
								<div class="flex items-center gap-2">
									{#if !transformEnabled && !requestsStore.selected}
										<span class="text-xs text-[var(--text-muted)]"></span>
									{:else if transformEnabled && !requestsStore.selected}
										<span class="text-xs text-[var(--text-muted)]">Select a request from the left panel to test your transform.</span>
									{/if}
								</div>
								<div class="flex items-center gap-2">
									{#if transformEnabled && transformScript}
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
										Save Script
									</button>
								</div>
							</div>
						</div>
					{:else if rightTab === 'settings'}
						<div class="p-4">
							<div class="mb-4">
								<h3 class="text-sm font-semibold mb-1">Endpoint Settings</h3>
								<p class="text-xs text-[var(--text-muted)]">
									Configure forwarding, custom responses, and endpoint properties.
								</p>
							</div>
							<ForwardConfig endpoint={ep} onUpdate={handleSettingsUpdate} testRequest={requestsStore.selected} />
						</div>
					{/if}
				</div>
			</div>
		</div>
	</div>
{/if}
