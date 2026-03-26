<script lang="ts">
	import type { CapturedRequest, CapturedResponse, ForwardResult, ForwardResultData } from '$lib/types';
	import { methodColor, formatFullTime, formatBytes, tryFormatJSON, copyToClipboard, getWebhookURL } from '$lib/utils';
	import { forwardRequest } from '$lib/forward';
	import { Copy, Trash2, Send, ChevronDown, ChevronRight, RotateCcw, Download, ArrowDownLeft, ArrowUpRight } from 'lucide-svelte';

	let {
		request,
		response,
		forwardResult: fwdTargetResult,
		forwardUrl,
		endpointSlug,
		onDelete
	}: {
		request: CapturedRequest;
		response?: CapturedResponse;
		forwardResult?: ForwardResultData;
		forwardUrl?: string;
		endpointSlug?: string;
		onDelete: (id: string) => void;
	} = $props();

	let copied = $state<string | null>(null);
	let showHeaders = $state(true);
	let showQuery = $state(true);
	let showBody = $state(true);
	let forwardResult = $state<ForwardResult | null>(null);
	let forwarding = $state(false);
	let replaying = $state(false);
	let replayResult = $state<{ ok: boolean; status?: number; error?: string } | null>(null);
	let showResponse = $state(true);
	let showForwardTarget = $state(true);

	const isBinary = $derived(request.body_encoding === 'base64');

	const bodyString = $derived(
		request.body
			? isBinary
				? '' // binary content — don't try to display raw base64
				: request.body
			: ''
	);

	const formattedBody = $derived(tryFormatJSON(bodyString));

	const responseBodyString = $derived(response?.body || '');
	const formattedResponseBody = $derived(tryFormatJSON(responseBodyString));
	const responseHeaderEntries = $derived(
		response?.headers ? Object.entries(response.headers) : []
	);
	const responseStatusColor = $derived(
		response
			? response.status >= 200 && response.status < 300
				? 'text-[var(--green)]'
				: response.status >= 400
					? 'text-[var(--red)]'
					: 'text-[var(--yellow)]'
			: ''
	);
	const responseSourceLabel = $derived(
		response?.source === 'handler'
			? 'Custom Handler'
			: response?.source === 'forward_passthrough'
				? 'Forward Passthrough'
				: 'Default'
	);

	// Forward target response derived values
	const fwdTargetPending = $derived(fwdTargetResult?.error === '__pending__');
	const fwdTargetBodyString = $derived(fwdTargetResult?.response_body || '');
	const formattedFwdTargetBody = $derived(tryFormatJSON(fwdTargetBodyString));
	const fwdTargetHeaderEntries = $derived(
		fwdTargetResult?.response_headers ? Object.entries(fwdTargetResult.response_headers) : []
	);
	const fwdTargetStatusColor = $derived(
		fwdTargetResult
			? fwdTargetPending
				? 'text-[var(--text-muted)]'
				: fwdTargetResult.status_code >= 200 && fwdTargetResult.status_code < 300
					? 'text-[var(--green)]'
					: fwdTargetResult.status_code >= 400
						? 'text-[var(--red)]'
						: 'text-[var(--yellow)]'
			: ''
	);

	const headerEntries = $derived(
		request.headers ? Object.entries(request.headers) : []
	);

	const queryEntries = $derived(
		request.query ? Object.entries(request.query) : []
	);

	async function handleCopy(text: string, key: string) {
		await copyToClipboard(text);
		copied = key;
		setTimeout(() => (copied = null), 2000);
	}

	async function handleForward() {
		if (!forwardUrl) return;
		forwarding = true;
		forwardResult = await forwardRequest(request, forwardUrl);
		forwarding = false;
	}

	async function handleReplay() {
		if (!endpointSlug) return;
		replaying = true;
		replayResult = null;
		try {
			let url = getWebhookURL(endpointSlug) + request.path;
			// Append original query parameters
			if (request.query && Object.keys(request.query).length > 0) {
				const params = new URLSearchParams();
				for (const [key, values] of Object.entries(request.query)) {
					const vals = Array.isArray(values) ? values : [values];
					for (const v of vals) {
						params.append(key, v);
					}
				}
				url += '?' + params.toString();
			}
			const headers: Record<string, string> = {};
			if (request.headers) {
				for (const [key, values] of Object.entries(request.headers)) {
					const k = key.toLowerCase();
					if (k === 'host' || k === 'content-length' || k === 'connection') continue;
					headers[key] = Array.isArray(values) ? values[0] : values;
				}
			}
			const res = await fetch(url, {
				method: request.method,
				headers,
				body: ['GET', 'HEAD'].includes(request.method) ? undefined : (bodyString || undefined)
			});
			replayResult = { ok: res.ok, status: res.status };
		} catch (err) {
			replayResult = { ok: false, error: err instanceof Error ? err.message : 'Replay failed' };
		} finally {
			replaying = false;
		}
	}

	function exportRequestJSON() {
		const data = JSON.stringify(request, null, 2);
		const blob = new Blob([data], { type: 'application/json' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `request-${request.id.slice(0, 8)}.json`;
		a.click();
		URL.revokeObjectURL(url);
	}

	// M-11: Shell-escape a string for safe embedding in single-quoted cURL arguments.
	// Replaces single quotes with the standard shell escape sequence: end quote, escaped quote, re-open quote.
	function shellEscape(s: string): string {
		return s.replace(/'/g, "'\\''");
	}

	function generateCurl(): string {
		let cmd = `curl -X ${shellEscape(request.method)}`;
		if (request.headers) {
			for (const [key, values] of Object.entries(request.headers)) {
				const k = key.toLowerCase();
				if (k === 'host' || k === 'content-length') continue;
				const val = Array.isArray(values) ? values[0] : values;
				cmd += ` \\\n  -H '${shellEscape(key)}: ${shellEscape(String(val))}'`;
			}
		}
		if (bodyString) {
			cmd += ` \\\n  -d '${shellEscape(bodyString)}'`;
		}
		let targetUrl = endpointSlug ? getWebhookURL(endpointSlug) + request.path : request.path;
		if (request.query && Object.keys(request.query).length > 0) {
			const params = new URLSearchParams();
			for (const [key, values] of Object.entries(request.query)) {
				const vals = Array.isArray(values) ? values : [values];
				for (const v of vals) {
					params.append(key, v);
				}
			}
			targetUrl += '?' + params.toString();
		}
		cmd += ` \\\n  '${shellEscape(targetUrl)}'`;
		return cmd;
	}
</script>

<div class="p-4 flex flex-col gap-4">
	<!-- Header -->
	<div class="flex items-center justify-between">
		<div class="flex items-center gap-3">
			<span class="text-sm font-mono font-bold {methodColor(request.method)}">
				{request.method}
			</span>
			<span class="text-sm font-mono text-[var(--text)]">{request.path}</span>
		</div>
		<div class="flex items-center gap-2">
			<button
				onclick={() => handleCopy(generateCurl(), 'curl')}
				class="text-xs px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
			>
				{copied === 'curl' ? 'Copied!' : 'Copy as cURL'}
			</button>
			{#if endpointSlug}
				<button
					onclick={handleReplay}
					disabled={replaying}
					class="text-xs px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1 disabled:opacity-50"
					title="Replay this request to the webhook endpoint"
				>
					<RotateCcw class="w-3 h-3" />
					{replaying ? 'Replaying...' : 'Replay'}
				</button>
			{/if}
			{#if forwardUrl}
				<button
					onclick={handleForward}
					disabled={forwarding}
					class="text-xs px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1 disabled:opacity-50"
				>
					<Send class="w-3 h-3" />
					{forwarding ? 'Forwarding...' : 'Forward'}
				</button>
			{/if}
			<button
				onclick={exportRequestJSON}
				class="text-xs px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1"
				title="Export as JSON"
			>
				<Download class="w-3 h-3" />
			</button>
			<button
				onclick={() => onDelete(request.id)}
				class="p-1 rounded hover:bg-red-500/10 text-[var(--text-muted)] hover:text-[var(--red)] transition-colors"
				title="Delete request"
			>
				<Trash2 class="w-4 h-4" />
			</button>
		</div>
	</div>

	<!-- Metadata -->
	<div class="flex items-center gap-4 text-xs text-[var(--text-muted)]">
		<span>{formatFullTime(request.created_at)}</span>
		<span>{formatBytes(request.size)}</span>
		<span>{request.ip}</span>
		{#if request.content_type}
			<span>{request.content_type}</span>
		{/if}
	</div>

	<!-- Forward result -->
	{#if forwardResult}
		<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded p-3">
			<div class="text-xs font-medium text-[var(--text-muted)] mb-2">Forward Result</div>
			<div class="flex items-center gap-2 text-xs py-1">
				<span class="w-2 h-2 rounded-full {forwardResult.ok ? 'bg-[var(--green)]' : 'bg-[var(--red)]'}"></span>
				<span class="font-mono text-[var(--text-muted)] flex-1 truncate">{forwardResult.url}</span>
				{#if forwardResult.status}
					<span class="{forwardResult.ok ? 'text-[var(--green)]' : 'text-[var(--red)]'}">{forwardResult.status}</span>
				{/if}
				<span class="text-[var(--text-muted)]">{forwardResult.latency}ms</span>
				{#if forwardResult.error}
					<span class="text-[var(--red)]">{forwardResult.error}</span>
				{/if}
			</div>
		</div>
	{/if}

	<!-- Replay result -->
	{#if replayResult}
		<div class="text-xs px-3 py-2 rounded border {replayResult.ok ? 'border-green-500/30 bg-green-500/5 text-[var(--green)]' : 'border-red-500/30 bg-red-500/5 text-[var(--red)]'}">
			{#if replayResult.ok}
				Replay sent: {replayResult.status}
			{:else if replayResult.error}
				Replay failed: {replayResult.error}
			{:else}
				Replay returned: {replayResult.status}
			{/if}
		</div>
	{/if}

	<!-- Headers -->
	<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded">
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<button
			class="w-full px-3 py-2 flex items-center justify-between text-xs font-medium text-[var(--text-muted)] hover:text-[var(--text)] transition-colors cursor-pointer select-none"
			onclick={() => (showHeaders = !showHeaders)}
		>
			<div class="flex items-center gap-1">
				{#if showHeaders}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
				Headers
				<span class="text-[var(--text-muted)]">({headerEntries.length})</span>
			</div>
			<span
				onclick={(e: MouseEvent) => { e.stopPropagation(); handleCopy(JSON.stringify(request.headers, null, 2), 'headers'); }}
				class="p-1 rounded hover:bg-[var(--bg-hover)]"
				role="button"
				tabindex="-1"
			>
				<Copy class="w-3 h-3" />
			</span>
		</button>
		{#if showHeaders}
			<div class="px-3 pb-3 max-h-60 overflow-y-auto">
				<table class="w-full text-xs">
					<tbody>
						{#each headerEntries as [key, values]}
							<tr class="border-t border-[var(--border)]">
								<td class="py-1 pr-3 text-[var(--text-muted)] font-mono whitespace-nowrap align-top">{key}</td>
								<td class="py-1 text-[var(--text)] font-mono break-all">
									{Array.isArray(values) ? values.join(', ') : values}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>

	<!-- Query params -->
	{#if queryEntries.length > 0}
		<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded">
			<button
				class="w-full px-3 py-2 flex items-center gap-1 text-xs font-medium text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
				onclick={() => (showQuery = !showQuery)}
			>
				{#if showQuery}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
				Query Parameters
				<span class="text-[var(--text-muted)]">({queryEntries.length})</span>
			</button>
			{#if showQuery}
				<div class="px-3 pb-3">
					<table class="w-full text-xs">
						<tbody>
							{#each queryEntries as [key, values]}
								<tr class="border-t border-[var(--border)]">
									<td class="py-1 pr-3 text-[var(--text-muted)] font-mono whitespace-nowrap">{key}</td>
									<td class="py-1 text-[var(--text)] font-mono break-all">
										{Array.isArray(values) ? values.join(', ') : values}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Body -->
	{#if bodyString || isBinary}
		<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded">
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<button
				class="w-full px-3 py-2 flex items-center justify-between text-xs font-medium text-[var(--text-muted)] hover:text-[var(--text)] transition-colors cursor-pointer select-none"
				onclick={() => (showBody = !showBody)}
			>
				<div class="flex items-center gap-1">
					{#if showBody}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
					Body
					<span class="text-[var(--text-muted)]">({formatBytes(request.size)})</span>
					{#if isBinary}
						<span class="ml-1 px-1 py-0.5 rounded bg-orange-500/10 text-orange-400 text-[10px]">Binary</span>
					{:else if formattedBody.isJSON}
						<span class="ml-1 px-1 py-0.5 rounded bg-blue-500/10 text-blue-400 text-[10px]">JSON</span>
					{/if}
				</div>
				{#if !isBinary}
					<span
						onclick={(e: MouseEvent) => { e.stopPropagation(); handleCopy(bodyString, 'body'); }}
						class="p-1 rounded hover:bg-[var(--bg-hover)]"
						role="button"
						tabindex="-1"
					>
						<Copy class="w-3 h-3" />
					</span>
				{/if}
			</button>
			{#if showBody}
				<div class="px-3 pb-3">
					{#if isBinary}
						<div class="text-xs text-[var(--text-muted)] bg-[var(--bg)] rounded p-3">
							Binary content ({formatBytes(request.size)}) — {request.content_type || 'unknown type'}
						</div>
					{:else}
						<pre class="text-xs font-mono text-[var(--text)] bg-[var(--bg)] rounded p-3 overflow-x-auto max-h-96 whitespace-pre-wrap break-all">{formattedBody.formatted}</pre>
					{/if}
				</div>
			{/if}
		</div>
	{/if}

	<!-- Response -->
	{#if response}
		<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded">
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<button
				class="w-full px-3 py-2 flex items-center justify-between text-xs font-medium text-[var(--text-muted)] hover:text-[var(--text)] transition-colors cursor-pointer select-none"
				onclick={() => (showResponse = !showResponse)}
			>
				<div class="flex items-center gap-1.5">
					{#if showResponse}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
					<ArrowDownLeft class="w-3 h-3" />
					Response
					<span class="font-mono font-bold {responseStatusColor}">{response.status}</span>
					<span class="px-1 py-0.5 rounded text-[10px] {response.source === 'handler' ? 'bg-purple-500/10 text-purple-400' : response.source === 'forward_passthrough' ? 'bg-blue-500/10 text-blue-400' : 'bg-[var(--border)] text-[var(--text-muted)]'}">{responseSourceLabel}</span>
				</div>
				{#if responseBodyString}
					<span
						onclick={(e: MouseEvent) => { e.stopPropagation(); handleCopy(responseBodyString, 'response-body'); }}
						class="p-1 rounded hover:bg-[var(--bg-hover)]"
						role="button"
						tabindex="-1"
					>
						<Copy class="w-3 h-3" />
					</span>
				{/if}
			</button>
			{#if showResponse}
				<div class="px-3 pb-3 flex flex-col gap-2">
					<!-- Response headers -->
					{#if responseHeaderEntries.length > 0}
						<div>
							<div class="text-[10px] text-[var(--text-muted)] mb-1 uppercase tracking-wide">Headers</div>
							<div class="max-h-40 overflow-y-auto">
								<table class="w-full text-xs">
									<tbody>
										{#each responseHeaderEntries as [key, value]}
											<tr class="border-t border-[var(--border)]">
												<td class="py-0.5 pr-3 text-[var(--text-muted)] font-mono whitespace-nowrap align-top">{key}</td>
												<td class="py-0.5 text-[var(--text)] font-mono break-all">{value}</td>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						</div>
					{/if}

					<!-- Response body -->
					{#if responseBodyString}
						<div>
							<div class="text-[10px] text-[var(--text-muted)] mb-1 uppercase tracking-wide flex items-center gap-1">
								Body
								{#if formattedResponseBody.isJSON}
									<span class="px-1 py-0.5 rounded bg-blue-500/10 text-blue-400 text-[10px] normal-case">JSON</span>
								{/if}
							</div>
							<pre class="text-xs font-mono text-[var(--text)] bg-[var(--bg)] rounded p-3 overflow-x-auto max-h-64 whitespace-pre-wrap break-all">{formattedResponseBody.formatted}</pre>
						</div>
					{/if}
				</div>
			{/if}
		</div>
	{/if}

	<!-- Forward Target Response -->
	{#if fwdTargetResult}
		<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded">
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<button
				class="w-full px-3 py-2 flex items-center justify-between text-xs font-medium text-[var(--text-muted)] hover:text-[var(--text)] transition-colors cursor-pointer select-none"
				onclick={() => (showForwardTarget = !showForwardTarget)}
			>
				<div class="flex items-center gap-1.5">
					{#if showForwardTarget}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
					<ArrowUpRight class="w-3 h-3" />
					Forward Target Response
					{#if fwdTargetPending}
						<span class="text-[var(--text-muted)] animate-pulse">Forwarding...</span>
					{:else}
						<span class="font-mono font-bold {fwdTargetStatusColor}">{fwdTargetResult.status_code}</span>
						<span class="text-[var(--text-muted)]">{fwdTargetResult.latency_ms}ms</span>
					{/if}
				</div>
				{#if fwdTargetBodyString && !fwdTargetPending}
					<span
						onclick={(e: MouseEvent) => { e.stopPropagation(); handleCopy(fwdTargetBodyString, 'fwd-target-body'); }}
						class="p-1 rounded hover:bg-[var(--bg-hover)]"
						role="button"
						tabindex="-1"
					>
						<Copy class="w-3 h-3" />
					</span>
				{/if}
			</button>
			{#if showForwardTarget}
				<div class="px-3 pb-3 flex flex-col gap-2">
					{#if fwdTargetPending}
						<!-- Pending state -->
						<div class="text-xs text-[var(--text-muted)] flex items-center gap-2">
							<span class="w-3 h-3 border-2 border-[var(--text-muted)] border-t-transparent rounded-full animate-spin"></span>
							Forwarding to <span class="font-mono">{fwdTargetResult.url}</span>...
						</div>
					{:else}
						<!-- Forward URL -->
						<div class="text-xs font-mono text-[var(--text-muted)] truncate">{fwdTargetResult.url}</div>

						<!-- Error -->
						{#if fwdTargetResult.error}
							<div class="text-xs text-[var(--red)] bg-red-500/5 border border-red-500/20 rounded px-2 py-1">
								{fwdTargetResult.error}
							</div>
						{/if}

						<!-- Forward target response headers -->
						{#if fwdTargetHeaderEntries.length > 0}
							<div>
								<div class="text-[10px] text-[var(--text-muted)] mb-1 uppercase tracking-wide">Headers</div>
								<div class="max-h-40 overflow-y-auto">
									<table class="w-full text-xs">
										<tbody>
											{#each fwdTargetHeaderEntries as [key, value]}
												<tr class="border-t border-[var(--border)]">
													<td class="py-0.5 pr-3 text-[var(--text-muted)] font-mono whitespace-nowrap align-top">{key}</td>
													<td class="py-0.5 text-[var(--text)] font-mono break-all">{value}</td>
												</tr>
											{/each}
										</tbody>
									</table>
								</div>
							</div>
						{/if}

						<!-- Forward target response body -->
						{#if fwdTargetBodyString}
							<div>
								<div class="text-[10px] text-[var(--text-muted)] mb-1 uppercase tracking-wide flex items-center gap-1">
									Body
									{#if formattedFwdTargetBody.isJSON}
										<span class="px-1 py-0.5 rounded bg-blue-500/10 text-blue-400 text-[10px] normal-case">JSON</span>
									{/if}
								</div>
								<pre class="text-xs font-mono text-[var(--text)] bg-[var(--bg)] rounded p-3 overflow-x-auto max-h-64 whitespace-pre-wrap break-all">{formattedFwdTargetBody.formatted}</pre>
							</div>
						{/if}
					{/if}
				</div>
			{/if}
		</div>
	{/if}
</div>
