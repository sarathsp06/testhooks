<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { getEndpointBySlug, deleteEndpoint, getLimits, recordUpgradeInterest, resignRequest } from '$lib/api';
	import { createWSClient, type WSStatus } from '$lib/ws';
	import { getEndpointStore } from '$lib/stores/endpoint.svelte';
	import { getRequestsStore } from '$lib/stores/requests.svelte';
	import { getWebhookURL, copyToClipboard } from '$lib/utils';
	import { initWasm, runTransform, runResponseHandler, isLegacyLanguage } from '$lib/wasm';
	import { forwardRequest, forwardRequestSync, forwardRequestWithResponse, relayHeaders, explainForwardError } from '$lib/forward';
	import { saveRequest as saveToIDB } from '$lib/idb';
	import EndpointWaiting from '$lib/components/EndpointWaiting.svelte';
	import InspectDetail from '$lib/components/InspectDetail.svelte';
	import ScriptEditorPage from '$lib/components/ScriptEditorPage.svelte';
	import ForwardConfig from '$lib/components/ForwardConfig.svelte';
	import type { CapturedRequest, CapturedResponse, Endpoint, Limits, ResponseNeededData, ResponseInfoData, ForwardResponse, ForwardResultData } from '$lib/types';
	import { Copy, Check, Download, Trash2 } from 'lucide-svelte';
	import { ago, bodyText, eventLabel, forwardMayBeRunning, shortURL } from '$lib/requestutil';
	import { formatBytes } from '$lib/utils';
	// Endpoint pages are one person's captured traffic: keep them out of search engines.


	const slug = $derived(page.params.slug);
	const endpointStore = getEndpointStore();
	const requestsStore = getRequestsStore();

	let wsStatus = $state<WSStatus>('disconnected');
	let wsClient: ReturnType<typeof createWSClient> | null = null;
	let copied = $state(false);
	let endpoint: Endpoint | null = null;
	let initError = $state<string | null>(null);
	// True until the first endpoint lookup settles (it may wait on the sign-in prompt).
	let initializing = $state(true);
	let deleteError = $state<string | null>(null);

	// Read the saved config at use time, so scripts saved in the editor apply
	// to the browser pipeline straight away.
	const liveCfg = () => endpointStore.current?.config ?? endpoint?.config ?? {};
	// Same for the mode, so switching modes in Settings applies without a reload.
	const liveMode = () => endpointStore.current?.mode ?? endpoint?.mode;
	const activeTransform = () => {
		const c = liveCfg();
		return c.wasm_script && !c.transform_disabled && !isLegacyLanguage(c.transform_language) ? c.wasm_script : '';
	};
	const replyActive = () => {
		const c = liveCfg();
		return !!(c.custom_response?.enabled && c.custom_response?.script && !isLegacyLanguage(c.custom_response?.language));
	};
	// Sync forwarding in browser mode: this tab forwards and the target's
	// reply (often from localhost) goes back to the sender.
	const syncForwardActive = () => {
		const c = liveCfg();
		return !!c.forward_url && c.forward_mode === 'sync';
	};
	// Mirrors browserDecidesReply on the server: when either is on, the server
	// holds the sender's request open and sends response_needed to this tab.
	const tabDecidesReply = () => replyActive() || syncForwardActive();

	type Resigned = Pick<ForwardResultData, 'resigned' | 'resign_error'>;
	const SIGNATURE_HEADERS = new Set(['webhook-id', 'webhook-timestamp', 'webhook-signature']);
	// Re-sign what this tab forwards (signature.resign), as the server does in
	// server mode: only a request the webhook secret signed gets a fresh
	// signature over the transformed body. The secret stays on the server.
	async function resignForward(captured: CapturedRequest, fwd: CapturedRequest): Promise<{ req: CapturedRequest } & Resigned> {
		if (!liveCfg().signature?.resign) return { req: fwd };
		if (captured.signature?.status !== 'verified' || captured.signature.scheme !== 'v1') {
			return { req: fwd, resign_error: 'not re-signed: the incoming request has no valid signature made with the webhook secret' };
		}
		try {
			const { headers } = await resignRequest((endpointStore.current ?? endpoint)!.id, captured, fwd.body ?? '');
			const kept = Object.entries(fwd.headers ?? {}).filter(([k]) => !SIGNATURE_HEADERS.has(k.toLowerCase()));
			const fresh = Object.entries(headers).map(([k, v]) => [k, [v]]);
			return { req: { ...fwd, headers: Object.fromEntries([...kept, ...fresh]) }, resigned: true };
		} catch (err) {
			const msg = err instanceof Error ? err.message : String(err);
			return { req: fwd, resign_error: msg.startsWith('not re-signed') ? msg : `not re-signed: ${msg}` };
		}
	}

	// ?edit=transform|reply|settings shows that screen in place of the requests;
	// ?try=<id> starts the editor on that request.
	const editing = $derived(page.url.searchParams.get('edit'));
	const tryId = $derived(page.url.searchParams.get('try'));
	let search = $state('');

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
			const cr = ep.config?.custom_response;

			// Always show what the server has stored: requests captured in server
			// mode stay after a switch to browser mode, and should stay visible.
			await requestsStore.load(ep.id);

			if (ep.config?.wasm_script || ep.mode === 'browser' || (cr?.enabled && cr?.script)) {
				initWasm().catch((err) => {
					console.warn('Script engine failed to load:', err);
				});
			}

			wsClient = createWSClient(s);
			wsClient.onStatus((status) => {
				// A reconnect may have missed requests: pick them up from the server.
				if (status === 'connected' && wsStatus !== 'connected' && everConnected) catchUp();
				if (status === 'connected') everConnected = true;
				wsStatus = status;
			});
			wsClient.onMessage(async (msg) => {
				if (msg.type === 'request') {
					let req = msg.data as CapturedRequest;

					// Browser-mode pipeline: Transform → Forward → Store
					if (liveMode() === 'browser') {
						// Step 1: Transform (returning null skips forwarding). The captured
						// request is stored as it arrived; the transformed copy is what's forwarded.
						let dropped = false;
						let fwdReq = req;
						if (activeTransform()) {
							try {
								await initWasm();
								const result = await runTransform(activeTransform(), req);
								if (result.ok && result.dropped) {
									dropped = true;
								} else if (result.ok && result.data) {
									fwdReq = { ...req, ...result.data } as CapturedRequest;
								} else if (!result.ok) {
									console.warn('Live transform error:', result.error);
								}
							} catch (err) {
								console.warn('Live transform error:', err);
							}
						}

						// Step 2: Forward (async only). When this tab decides the reply (a
						// reply script, or sync forwarding that relays the target's reply),
						// the server sends response_needed for this request and that flow
						// does the forward; doing it here too would deliver it twice.
						const fwdUrl = dropped || tabDecidesReply() ? undefined : liveCfg().forward_url;
						if (fwdUrl) {
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
							const captured = req;
							resignForward(captured, fwdReq).then(({ req: signedReq, ...signed }) =>
								forwardRequestWithResponse(signedReq, fwdUrl).then((fwdResult) => {
									requestsStore.attachForwardResult(captured.id, { ...fwdResult, ...signed });
								})
							).catch((err) => {
								// Always store a result so the UI shows the error instead of nothing.
								const message = explainForwardError(err, fwdUrl);
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
						}
					}

					requestsStore.prepend(req);

					if (liveMode() === 'browser') {
						saveToIDB(req).catch(() => {});
					}
				} else if (msg.type === 'response_needed') {
					// Server is holding the HTTP response open and waiting for this tab
					// to decide the reply: run the reply script, or, with sync forwarding
					// and no script, relay the forward target's reply as-is. That is how
					// an app on localhost answers the webhook sender.
					// Pipeline: Transform → Forward (sync) → Handler or passthrough → respond
					const data = msg.data as ResponseNeededData;
					const cr = liveCfg().custom_response;
					const useHandler = replyActive() && !!cr?.script;
					if (tabDecidesReply() && wsClient) {
						try {
							if (activeTransform() || useHandler) await initWasm();

							let handlerReq = data.request;
							let fwdResponse: ForwardResponse | null = null;

							// Step 1: Transform (if configured; null skips the forward)
							let dropped = false;
							if (activeTransform()) {
								try {
									const txResult = await runTransform(activeTransform(), handlerReq);
									if (txResult.ok && txResult.dropped) {
										dropped = true;
									} else if (txResult.ok && txResult.data) {
										handlerReq = { ...handlerReq, ...txResult.data } as CapturedRequest;
									}
								} catch (err) {
									console.warn('Transform error in response_needed:', err);
								}
							}

							// Step 2: Re-sign (if configured), then forward. The reply script
							// still sees the request as transformed.
							const fwdUrl = dropped ? undefined : liveCfg().forward_url;
							const fwdMode = liveCfg().forward_mode || 'async';
							let fwdError: string | undefined;
							const { req: signedReq, ...signed } = fwdUrl ? await resignForward(data.request, handlerReq) : { req: handlerReq };
							if (fwdUrl && fwdMode === 'sync') {
								try {
									const syncResult = await forwardRequestSync(signedReq, fwdUrl);
									fwdResponse = syncResult.response;
									fwdError = syncResult.result.error;
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
										content_type: syncResult.response?.content_type,
										...signed
									});
								} catch (err) {
									fwdError = explainForwardError(err, fwdUrl);
									console.warn('Sync forward error in response_needed:', err);
								}
							} else if (fwdUrl && fwdMode === 'async') {
								// Fire-and-forget even in response_needed flow, but capture response
								forwardRequestWithResponse(signedReq, fwdUrl).then((fwdResult) => {
									// Use the original request_id from the response_needed data
									requestsStore.attachForwardResult(data.request_id, {
										...fwdResult,
										...signed,
										request_id: data.request_id
									});
								}).catch((err) => {
									console.warn('Async forward error in response_needed:', err);
								});
							}

							// Step 3: Decide the reply. Like the server, a skipped request gets
							// the default reply. With a script, the script decides (and sees the
							// forward target's reply as req.forward_response). Without one, the
							// forward target's reply is relayed as-is, and no reply at all (CORS,
							// connection refused, timeout) becomes 502, as in server mode.
							type TabReply = {
								request_id: string;
								status: number;
								headers: Record<string, string>;
								body: string;
								content_type: string;
								source: CapturedResponse['source'];
							};
							let responseData: TabReply = {
								request_id: data.request_id,
								status: 202,
								headers: {},
								body: JSON.stringify({ status: 'ok', id: data.request_id }),
								content_type: 'application/json',
								source: 'default'
							};
							if (!dropped && useHandler) {
								const result = await runResponseHandler(cr!.script!, handlerReq, 'javascript', fwdResponse);
								if (result) {
									responseData = {
										request_id: data.request_id,
										status: result.response?.status || 200,
										headers: result.response?.headers || {},
										body: result.response?.body || '',
										content_type: result.response?.content_type || '',
										source: 'handler'
									};
								}
							} else if (!dropped && fwdUrl && fwdMode === 'sync') {
								responseData = fwdResponse
									? {
											request_id: data.request_id,
											status: fwdResponse.status,
											headers: relayHeaders(fwdResponse.headers),
											body: fwdResponse.body ?? '',
											content_type: fwdResponse.content_type || '',
											source: 'forward_passthrough'
										}
									: {
											request_id: data.request_id,
											status: 502,
											headers: {},
											body: JSON.stringify({ error: 'forward target did not respond', detail: fwdError ?? 'no response' }),
											content_type: 'application/json',
											source: 'forward_passthrough'
										};
							}
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
								source: responseData.source
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

	function open(screen: 'transform' | 'reply' | 'settings', tryRequest?: string) {
		goto(`/${slug}?edit=${screen}${tryRequest ? `&try=${tryRequest}` : ''}`);
	}

	function closeScreen() {
		goto(`/${slug}`);
	}

	async function saveConfig(patch: Record<string, unknown>) {
		const ep = endpointStore.current;
		if (!ep) throw new Error('Endpoint not loaded');
		await endpointStore.update({ config: { ...ep.config, ...patch } });
		if (endpointStore.error) {
			const msg = endpointStore.error;
			endpointStore.clearError();
			throw new Error(msg);
		}
		initWasm().catch(() => {});
	}

	function saveTransform(script: string, enabled: boolean) {
		return saveConfig({ wasm_script: script || undefined, transform_language: 'javascript', transform_disabled: enabled ? undefined : true });
	}

	function saveReply(script: string, enabled: boolean) {
		return saveConfig({ custom_response: { enabled, script, language: 'javascript' } });
	}

	async function handleSettingsUpdate(patch: { name?: string; slug_name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> }) {
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
		await endpointStore.update(patch);
		if (endpointStore.error) {
			const msg = endpointStore.error;
			endpointStore.clearError();
			throw new Error(msg);
		}
		// A new URL: reconnect under the new slug and show it in the address bar.
		const renamed = endpointStore.current?.slug;
		if (patch.slug_name !== undefined && renamed && renamed !== slug) {
			wsClient?.disconnect();
			wsClient = null;
			requestsStore.reset();
			await goto(`/${renamed}?edit=settings`, { replaceState: true });
			await init(renamed);
		}
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

	// A forward result is recorded when the forward finishes, which for async
	// forwards is after the request was listed, and the live message only
	// reaches tabs connected to the server instance that did the forwarding.
	// Fetch the request again when one is selected without a result.
	const refreshed = new Set<string>();
	$effect(() => {
		const sel = requestsStore.selected;
		const ep = endpointStore.current;
		if (!sel || !ep || ep.mode !== 'server' || !ep.config?.forward_url || refreshed.has(sel.id)) return;
		if (requestsStore.getForwardResult(sel.id)) return;
		refreshed.add(sel.id);
		requestsStore.refresh(sel.id);
	});

	// Show a request straight away, as the inspect view expects one selected.
	$effect(() => {
		if (!requestsStore.selectedId && requestsStore.items.length) requestsStore.select(requestsStore.items[0].id);
	});

	// Storage limits shown next to the stored count, and the note about a
	// paid plan when the server has one. The "I'd pay for that" click is
	// counted once per browser; the flag only hides the button after a reload.
	const ASKED_KEY = 'testhooks_upgrade_asked';
	let limits = $state<Limits | null>(null);
	let askedCount = $state<number | null>(null);
	let asked = $state(false);
	let askError = $state<string | null>(null);
	async function askForPaidPlan() {
		askError = null;
		try {
			const res = await recordUpgradeInterest(endpoint?.id ?? '');
			askedCount = res.count;
			asked = true;
			try {
				localStorage.setItem(ASKED_KEY, '1');
			} catch {
				// Private window or blocked storage: the server still counted it.
			}
		} catch (e) {
			askError = e instanceof Error ? e.message : String(e);
		}
	}

	// The WebSocket only carries requests that reached the server instance this
	// tab is connected to. With several instances (Vercel, a load balancer) the
	// rest are only in the database, so check it while the tab is visible.
	const CATCH_UP_MS = 5000;
	let everConnected = false;
	let catchUpTimer: ReturnType<typeof setInterval> | null = null;

	function catchUp() {
		const ep = endpointStore.current;
		if (!ep || document.visibilityState !== 'visible') return;
		if (liveMode() !== 'server' && !liveCfg().persist_requests) return;
		void requestsStore.catchUp(ep.id);
	}

	function onVisibilityChange() {
		if (document.visibilityState === 'visible') catchUp();
	}

	onMount(() => {
		if (slug) init(slug);
		catchUpTimer = setInterval(catchUp, CATCH_UP_MS);
		document.addEventListener('visibilitychange', onVisibilityChange);
		try {
			asked = localStorage.getItem(ASKED_KEY) === '1';
		} catch {
			// Private window or blocked storage: show the button again.
		}
		getLimits()
			.then((l) => (limits = l))
			.catch(() => {
				// An older server without /api/limits: show nothing.
			});
	});

	onDestroy(() => {
		if (catchUpTimer) clearInterval(catchUpTimer);
		if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisibilityChange);
		wsClient?.disconnect();
		requestsStore.reset();
		endpointStore.clear();
	});
</script>

<svelte:head>
	<meta name="robots" content="noindex" />
</svelte:head>

{#snippet logo()}
	<a href="/" class="flex items-center gap-2.5 no-underline text-ink">
		<span class="w-[26px] h-[26px] rounded-[7px] bg-[#111613] dark:bg-[#1B231F] flex items-center justify-center">
			<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="#5BD08F" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 12h5l2-5 3 10 2-5h4"></path></svg>
		</span>
		<span class="font-semibold text-base tracking-[-0.01em]">testhooks</span>
	</a>
{/snippet}

{#snippet stage(label: string, detail: string, on: boolean, action: string, onclick: (() => void) | null, mono = false)}
	{#if onclick}
		<li>
			<button
				type="button"
				{onclick}
				class="flex items-center gap-2 text-[13px] px-3 py-2 rounded-[9px] text-ink cursor-pointer hover:border-line-strong {on ? 'bg-page border border-line' : 'bg-surface border border-dashed border-line-dashed'}"
			>
				{#if on}
					<span class="w-2 h-2 rounded-full bg-accent" aria-hidden="true"></span>
				{:else}
					<span class="w-2 h-2 rounded-full border-[1.5px] border-[#9AA39E] box-border" aria-hidden="true"></span>
				{/if}
				<b class="font-semibold">{label}</b>
				<span class="text-body {mono ? 'font-mono text-xs' : ''}">{detail}</span>
				<span class="text-accent font-medium">{action}</span>
			</button>
		</li>
	{:else}
		<li class="flex items-center gap-2 text-[13px] px-3 py-2 rounded-[9px] bg-page border border-line">
			<span class="w-2 h-2 rounded-full bg-accent" aria-hidden="true"></span>
			<b class="font-semibold">{label}</b>
			<span class="text-body">{detail}</span>
		</li>
	{/if}
{/snippet}

{#if endpointStore.loading || initializing}
	<div class="flex items-center justify-center py-20 text-body">Loading endpoint…</div>
{:else if initError}
	<div class="flex flex-col items-center justify-center py-20 gap-3 text-center">
		<p class="m-0 text-danger">{initError}</p>
		<div class="flex gap-4 text-sm">
			<button onclick={() => slug && init(slug)} class="text-accent hover:underline cursor-pointer">Retry</button>
			<a href="/" class="text-accent hover:underline">Go back</a>
		</div>
	</div>
{:else if !endpointStore.current}
	<div class="flex flex-col items-center justify-center py-20 gap-2 text-center">
		<p class="m-0 text-body">Endpoint not found</p>
		<a href="/" class="text-accent hover:underline text-sm">Go back</a>
	</div>
{:else if editing === 'transform' || editing === 'reply'}
	{@const ep = endpointStore.current}
	{#key editing}
		<ScriptEditorPage
			kind={editing}
			endpoint={ep}
			captured={requestsStore.items}
			initialRequestId={tryId}
			onSave={editing === 'transform' ? saveTransform : saveReply}
			onClose={closeScreen}
		/>
	{/key}
{:else}
	{@const ep = endpointStore.current}
	{@const cfg = ep.config ?? {}}
	{@const waiting = requestsStore.items.length === 0 && editing !== 'settings'}
	{@const transformSet = !!cfg.wasm_script}
	{@const transformOn = transformSet && !cfg.transform_disabled && !isLegacyLanguage(cfg.transform_language)}
	{@const replyOn = !!(cfg.custom_response?.enabled && cfg.custom_response?.script)}
	<div class="min-h-screen bg-page text-ink flex flex-col">
		<header class="border-b border-line bg-page">
			<div class="{waiting ? 'max-w-[1240px] mx-auto w-full' : ''} px-6 min-h-[60px] flex items-center justify-between gap-4 flex-wrap">
				<nav aria-label="Breadcrumb" class="flex items-center gap-2.5 text-sm flex-wrap">
					{@render logo()}
					<span class="text-[#9AA39E]" aria-hidden="true">/</span>
					<span class="font-medium">{ep.name || ep.slug}</span>
					<span class="text-xs font-medium text-soft bg-chip rounded-md px-2 py-[3px]">{ep.mode === 'server' ? 'Server' : 'Browser'}</span>
					{#if !waiting}
						{#if wsStatus === 'connected'}
							<span class="flex items-center gap-1.5 text-xs text-accent"><span class="w-[7px] h-[7px] rounded-full bg-accent" aria-hidden="true"></span>Live</span>
						{:else}
							<span class="flex items-center gap-1.5 text-xs text-faint"><span class="w-[7px] h-[7px] rounded-full bg-faint" aria-hidden="true"></span>{wsStatus === 'connecting' ? 'Connecting' : 'Offline'}</span>
						{/if}
					{/if}
				</nav>
				{#if waiting}
					<div class="flex items-center gap-1 flex-wrap">
						<button type="button" onclick={() => open('settings')} class="text-sm text-soft hover:text-accent px-3 py-2.5 cursor-pointer">Settings</button>
						<a href="https://sarathsp06.github.io/testhooks/" target="_blank" rel="noopener noreferrer" class="no-underline text-sm text-soft hover:text-accent px-3 py-2.5">Docs</a>
						<a href="https://github.com/sarathsp06/testhooks" target="_blank" rel="noopener noreferrer" class="no-underline text-sm text-soft hover:text-accent px-3 py-2.5">GitHub</a>
					</div>
				{:else}
					<div class="flex items-center gap-2 flex-wrap min-w-0">
						<span class="font-mono text-[13px] text-soft bg-surface border border-line rounded-lg px-3 py-2 truncate max-w-[60vw]">{getWebhookURL(ep.slug).replace(/^https?:\/\//, '')}</span>
						<button
							type="button"
							onclick={handleCopy}
							aria-label="Copy webhook URL"
							title="Copy webhook URL"
							class="border border-line-strong bg-surface text-ink w-11 h-[38px] rounded-lg flex items-center justify-center cursor-pointer hover:bg-chip"
						>
							{#if copied}<Check class="w-[15px] h-[15px] text-accent" />{:else}<Copy class="w-[15px] h-[15px]" />{/if}
						</button>
						<a
							href="https://github.com/sarathsp06/testhooks"
							target="_blank"
							rel="noopener noreferrer"
							aria-label="Testhooks on GitHub"
							title="Testhooks on GitHub"
							class="text-soft hover:text-accent w-11 h-[38px] rounded-lg flex items-center justify-center hover:bg-chip"
						>
							<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 19c-4.3 1.4-4.3-2.5-6-3m12 5v-3.5c0-1 .1-1.4-.5-2 2.8-.3 5.5-1.4 5.5-6a4.6 4.6 0 0 0-1.3-3.2 4.2 4.2 0 0 0-.1-3.2s-1.1-.3-3.5 1.3a12.3 12.3 0 0 0-6.2 0C6.5 2.8 5.4 3.1 5.4 3.1a4.2 4.2 0 0 0-.1 3.2A4.6 4.6 0 0 0 4 9.5c0 4.6 2.7 5.7 5.5 6-.6.6-.6 1.2-.5 2V21"></path></svg>
						</a>
					</div>
				{/if}
			</div>
		</header>

		{#if waiting}
			<div class="flex-1 overflow-y-auto"><EndpointWaiting endpoint={ep} onOpenTab={(tab) => open(tab)} /></div>
		{:else}
			<nav aria-label="Pipeline" class="border-b border-line bg-surface">
				<ol class="m-0 px-6 py-3 list-none flex flex-wrap gap-2 items-center">
					{@render stage('Capture', ep.mode === 'server' ? 'always on' : 'while this tab is open', true, '', null)}
					<li aria-hidden="true" class="text-[#9AA39E]">→</li>
					{@render stage('Transform', !transformSet ? 'not set' : transformOn ? 'on' : isLegacyLanguage(cfg.transform_language) ? 'needs a rewrite' : 'off', transformOn, transformSet ? 'Edit' : 'Add', () => open('transform'))}
					<li aria-hidden="true" class="text-[#9AA39E]">→</li>
					{@render stage('Forward', cfg.forward_url ? shortURL(cfg.forward_url) : 'not set', !!cfg.forward_url, cfg.forward_url ? 'Edit' : 'Add', () => open('settings'), !!cfg.forward_url)}
					<li aria-hidden="true" class="text-[#9AA39E]">→</li>
					{@render stage('Reply', replyOn ? 'custom script' : cfg.forward_url && cfg.forward_mode === 'sync' ? (ep.mode === 'browser' ? "your app's reply" : "the target's reply") : `default ${ep.mode === 'browser' ? 202 : 200}`, replyOn || (!!cfg.forward_url && cfg.forward_mode === 'sync'), 'Change', () => open('reply'))}
					<li class="ml-auto">
						<button type="button" onclick={() => (editing === 'settings' ? closeScreen() : open('settings'))} class="text-[13px] text-soft hover:text-accent underline cursor-pointer px-1 py-2">
							{editing === 'settings' ? 'Back to requests' : 'Settings'}
						</button>
					</li>
				</ol>
			</nav>

			{#if editing === 'settings'}
				<div class="p-6 max-w-2xl w-full flex flex-col gap-4">
					<h1 class="m-0 text-[22px] font-semibold tracking-[-0.015em]">Settings</h1>
					{#if deleteError}<p class="m-0 text-sm text-danger">{deleteError}</p>{/if}
					<ForwardConfig endpoint={ep} forwardPrivate={!!limits?.forward_private_addresses} onUpdate={handleSettingsUpdate} />
				</div>
			{:else}
				{@const q = search.trim().toLowerCase()}
				{@const shown = q ? requestsStore.items.filter((r) => `${r.method} ${r.path} ${bodyText(r)} ${JSON.stringify(r.headers)}`.toLowerCase().includes(q)) : requestsStore.items}
				<div class="flex-1 flex flex-wrap min-h-0">
					<section aria-label="Requests" class="flex-[1_1_320px] max-w-full min-w-0 border-r border-line bg-surface flex flex-col">
						<div class="px-4 py-3 border-b border-line flex gap-2 items-center">
							<label class="flex-1 flex items-center gap-2 border border-line rounded-[9px] px-3 min-h-10 bg-page">
								<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="text-faint" aria-hidden="true"><circle cx="11" cy="11" r="7"></circle><path d="M20 20l-3.5-3.5"></path></svg>
								<span class="sr-only">Search requests</span>
								<input type="search" bind:value={search} placeholder="Search body, headers, path" class="flex-1 border-0 bg-transparent text-sm text-ink outline-none min-w-0" />
							</label>
							<span class="text-[13px] text-faint whitespace-nowrap">{requestsStore.total || requestsStore.items.length}</span>
							<button type="button" onclick={exportAllJSON} title="Download requests as JSON" aria-label="Download requests as JSON" class="w-8 h-8 rounded-md flex items-center justify-center text-faint hover:text-ink hover:bg-chip cursor-pointer"><Download class="w-4 h-4" /></button>
							<button type="button" onclick={() => confirm('Delete all captured requests?') && handleClearAll()} title="Delete all requests" aria-label="Delete all requests" class="w-8 h-8 rounded-md flex items-center justify-center text-faint hover:text-danger hover:bg-chip cursor-pointer"><Trash2 class="w-4 h-4" /></button>
						</div>
						{#if limits && (ep.mode === 'server' || cfg.persist_requests)}
							<div class="px-4 py-2 border-b border-line text-xs text-body flex flex-wrap items-center gap-x-2 gap-y-1">
								<span>Keeps the newest {limits.max_requests_per_endpoint.toLocaleString()} requests, up to {formatBytes(limits.max_endpoint_storage_bytes)}; older ones are deleted.</span>
								{#if limits.upgrade_note}
									<span>{limits.upgrade_note}</span>
									{#if asked}
										<span class="text-accent">{askedCount === null ? 'Noted, thanks.' : askedCount === 1 ? "Noted, thanks. You're the first to ask." : `Noted, thanks. ${askedCount.toLocaleString()} people have asked.`}</span>
									{:else}
										<button type="button" onclick={askForPaidPlan} class="text-accent underline cursor-pointer px-0 py-0 bg-transparent border-0 text-xs">I'd pay for that</button>
									{/if}
									{#if askError}<span role="alert" class="text-danger">Couldn't record that: {askError}</span>{/if}
								{/if}
							</div>
						{/if}
						{#if requestsStore.error}
							<p role="alert" class="m-0 px-4 py-2 text-[13px] text-danger border-b border-line">{requestsStore.error}</p>
						{/if}
						<ul class="m-0 p-0 list-none flex flex-col overflow-y-auto">
							{#each shown as r (r.id)}
								{@const fr = requestsStore.getForwardResult(r.id)}
								{@const selected = requestsStore.selectedId === r.id}
								<li>
									<button
										type="button"
										aria-current={selected ? 'true' : undefined}
										onclick={() => requestsStore.select(r.id)}
										class="w-full text-left flex flex-col gap-1.5 px-4 py-3.5 border-b border-chip text-ink cursor-pointer {selected ? 'bg-[var(--row-selected)] shadow-[inset_3px_0_0_var(--accent)]' : 'hover:bg-page'}"
									>
										<span class="flex justify-between gap-2">
											<span class="font-mono text-[13px] truncate"><b class="font-medium {r.method === 'GET' ? 'text-[var(--method-get)]' : 'text-accent'}">{r.method}</b> {r.path}</span>
											<span class="text-xs text-faint whitespace-nowrap">{ago(r.created_at)}</span>
										</span>
										<span class="flex gap-1.5 flex-wrap text-xs text-body">
											<span>{eventLabel(r)}</span>
											<span aria-hidden="true">·</span>
											<span>{formatBytes(r.size)}</span>
											{#if fr?.in_progress}
												<span aria-hidden="true">·</span>
												{#if forwardMayBeRunning(r.created_at)}<span>forwarding…</span>{:else}<span class="text-danger">forward never finished</span>{/if}
											{:else if fr && fr.error !== '__pending__'}
												<span aria-hidden="true">·</span>
												{#if fr.ok}<span class="text-accent">forwarded {fr.status_code}</span>{:else}<span class="text-danger">forward failed{fr.status_code > 0 ? ` ${fr.status_code}` : ''}</span>{/if}
											{/if}
											{#if r.signature?.status === 'invalid'}
												<span aria-hidden="true">·</span><span class="text-danger">signature invalid</span>
											{/if}
										</span>
									</button>
								</li>
							{:else}
								<li class="px-4 py-6 text-sm text-body">No requests match “{search}”.</li>
							{/each}
						</ul>
					</section>

					{#if requestsStore.selected}
						<InspectDetail
							request={requestsStore.selected}
							endpoint={ep}
							response={requestsStore.getResponse(requestsStore.selected.id)}
							forwardResult={requestsStore.getForwardResult(requestsStore.selected.id)}
							onTryTransform={() => open('transform', requestsStore.selected?.id)}
							onDelete={() => requestsStore.selected && requestsStore.remove(requestsStore.selected.id)}
						/>
					{:else}
						<section class="flex-[999_1_560px] min-w-0 p-6 flex items-center justify-center text-sm text-body">
							Pick a request on the left to see what happened to it.
						</section>
					{/if}
				</div>
			{/if}
		{/if}
	</div>
{/if}
