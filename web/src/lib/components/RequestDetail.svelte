<script lang="ts">
	import type { CapturedRequest, ForwardResult } from '$lib/types';
	import { methodColor, formatFullTime, formatBytes, tryFormatJSON, copyToClipboard, getWebhookURL } from '$lib/utils';
	import { forwardRequest } from '$lib/forward';
	import { Copy, Trash2, Send, ChevronDown, ChevronRight, RotateCcw, Download } from 'lucide-svelte';

	let {
		request,
		forwardUrl,
		endpointSlug,
		onDelete
	}: {
		request: CapturedRequest;
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

	const isBinary = $derived(request.body_encoding === 'base64');

	const bodyString = $derived(
		request.body
			? isBinary
				? '' // binary content — don't try to display raw base64
				: request.body
			: ''
	);

	const formattedBody = $derived(tryFormatJSON(bodyString));

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

	function generateCurl(): string {
		let cmd = `curl -X ${request.method}`;
		if (request.headers) {
			for (const [key, values] of Object.entries(request.headers)) {
				const k = key.toLowerCase();
				if (k === 'host' || k === 'content-length') continue;
				const val = Array.isArray(values) ? values[0] : values;
				cmd += ` \\\n  -H '${key}: ${val}'`;
			}
		}
		if (bodyString) {
			cmd += ` \\\n  -d '${bodyString.replace(/'/g, "'\\''")}'`;
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
		cmd += ` \\\n  '${targetUrl}'`;
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
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div
			class="w-full px-3 py-2 flex items-center justify-between text-xs font-medium text-[var(--text-muted)] hover:text-[var(--text)] transition-colors cursor-pointer select-none"
			onclick={() => (showHeaders = !showHeaders)}
		>
			<div class="flex items-center gap-1">
				{#if showHeaders}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
				Headers
				<span class="text-[var(--text-muted)]">({headerEntries.length})</span>
			</div>
			<button
				onclick={(e: MouseEvent) => { e.stopPropagation(); handleCopy(JSON.stringify(request.headers, null, 2), 'headers'); }}
				class="p-1 rounded hover:bg-[var(--bg-hover)]"
			>
				<Copy class="w-3 h-3" />
			</button>
		</div>
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
			<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
			<div
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
					<button
						onclick={(e: MouseEvent) => { e.stopPropagation(); handleCopy(bodyString, 'body'); }}
						class="p-1 rounded hover:bg-[var(--bg-hover)]"
					>
						<Copy class="w-3 h-3" />
					</button>
				{/if}
			</div>
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
</div>
