<script lang="ts">
	import { initWasm, runTransform } from '$lib/wasm';
	import { copyToClipboard, formatBytes } from '$lib/utils';
	import { ago, bodyKind, bodyText, curlFor, eventLabel, forwardMayBeRunning, highlightJSON, hostOf, parseJSON, queryString, replay, statusLine, when } from '$lib/requestutil';
	import { diffLines, fieldChanges, pretty } from '$lib/scriptdiff';
	import { toJSONSchema, toTypeScript, typeName } from '$lib/typegen';
	import { getRequestsStore } from '$lib/stores/requests.svelte';
	import type { CapturedRequest, CapturedResponse, Endpoint, ForwardResultData } from '$lib/types';

	let {
		request,
		endpoint,
		response,
		forwardResult,
		previous,
		onTryTransform,
		onDelete
	}: {
		request: CapturedRequest;
		endpoint: Endpoint;
		response?: CapturedResponse;
		forwardResult?: ForwardResultData;
		/** The request to compare with in the Diff tab. Left out: the latest earlier request on this endpoint in the loaded list. */
		previous?: CapturedRequest | null;
		/** Open the transform editor with this request as the input. */
		onTryTransform: () => void;
		/** Delete this request. */
		onDelete: () => void;
	} = $props();

	type Tab = 'body' | 'diff' | 'headers' | 'query' | 'forwarded' | 'fwdresponse';
	let tab = $state<Tab>('body');
	let copied = $state(false);
	let replayed = $state<string | null>(null);

	const cfg = $derived(endpoint.config ?? {});
	const script = $derived(cfg.wasm_script && !cfg.transform_disabled && (!cfg.transform_language || cfg.transform_language === 'javascript') ? cfg.wasm_script : '');
	const headerCount = $derived(Object.keys(request.headers ?? {}).length);
	const text = $derived(bodyText(request));
	const prettyBody = $derived(pretty(text));
	const isJSON = $derived(bodyKind(request) === 'JSON body');

	// The request before this one on the same endpoint, for the Diff tab.
	const requestsStore = getRequestsStore();
	const earlier = $derived.by(() => {
		if (previous !== undefined) return previous;
		const t = new Date(request.created_at).getTime();
		let best: CapturedRequest | null = null;
		for (const r of requestsStore.items) {
			if (r.id === request.id || r.endpoint_id !== request.endpoint_id) continue;
			const rt = new Date(r.created_at).getTime();
			if (rt < t && (!best || rt > new Date(best.created_at).getTime())) best = r;
		}
		return best;
	});
	const diff = $derived.by(() => {
		if (!earlier || tab !== 'diff') return null;
		const binary = earlier.body_encoding === 'base64' || request.body_encoding === 'base64';
		const before = pretty(bodyText(earlier));
		const after = pretty(text);
		return { binary, same: before === after, lines: binary ? [] : diffLines(before, after) };
	});

	// Copy the body's shape as a TypeScript type or a JSON Schema.
	let typeMenu = $state(false);
	let typeMenuEl = $state<HTMLElement>();
	let typeCopied = $state(false);
	async function copyType(kind: 'ts' | 'schema') {
		typeMenu = false;
		const event = eventLabel(request);
		const name = typeName(event === 'JSON body' ? '' : event);
		const value = parseJSON(text);
		if (await copyToClipboard(kind === 'ts' ? toTypeScript(value, name) : toJSONSchema(value, name))) {
			typeCopied = true;
			setTimeout(() => (typeCopied = false), 2000);
		}
	}

	// What the transform does to this request, using the current script.
	type Outcome =
		| { kind: 'none' }
		| { kind: 'error'; error: string }
		| { kind: 'skipped' }
		| { kind: 'ok'; forwarded: CapturedRequest; added: number; removed: number; changed: boolean; ms: number };
	let outcome = $state<Outcome>({ kind: 'none' });

	$effect(() => {
		const req = request;
		const s = script;
		if (!s) {
			outcome = { kind: 'none' };
			return;
		}
		let stale = false;
		(async () => {
			await initWasm();
			const r = await runTransform(s, req);
			if (stale) return;
			if (!r.ok) outcome = { kind: 'error', error: r.error ?? 'Failed' };
			else if (r.dropped) outcome = { kind: 'skipped' };
			else {
				const fwd = { ...req, ...r.data } as CapturedRequest;
				const fc = fieldChanges(bodyText(req), fwd.body ?? '');
				const changed = JSON.stringify(fwd) !== JSON.stringify({ ...req });
				outcome = { kind: 'ok', forwarded: fwd, added: fc?.added ?? 0, removed: fc?.removed ?? 0, changed, ms: r.duration };
			}
		})();
		return () => (stale = true);
	});

	const forwardedReq = $derived(outcome.kind === 'ok' ? outcome.forwarded : request);
	const fwdPending = $derived(forwardResult?.error === '__pending__');
	// Server mode: a forward that started (or whose start isn't recorded yet)
	// and may still be running, or one that started and never finished.
	const fwdRunning = $derived(
		endpoint.mode === 'server' && (!forwardResult || forwardResult.in_progress) && forwardMayBeRunning(request.created_at)
	);
	const fwdStalled = $derived(!!forwardResult?.in_progress && !fwdRunning);
	const stalledHint = 'The forward started but never finished: the server stopped before the target answered. The target may or may not have received it.';
	const noResultHint =
		'No forward result was recorded for this request. Requests captured before results were saved have none, and so does an async forward that never finished.';

	async function copyCurl() {
		if (await copyToClipboard(curlFor(request, endpoint.slug))) {
			copied = true;
			setTimeout(() => (copied = false), 2000);
		}
	}

	async function doReplay() {
		replayed = 'Sending…';
		try {
			replayed = `Sent again: ${statusLine(await replay(request, endpoint.slug))}`;
		} catch (err) {
			replayed = err instanceof Error ? err.message : 'Replay failed';
		}
		setTimeout(() => (replayed = null), 3000);
	}

	const tabs = $derived<{ id: Tab; label: string }[]>([
		{ id: 'body', label: 'Body' },
		{ id: 'diff', label: 'Diff' },
		{ id: 'headers', label: `Headers ${headerCount}` },
		{ id: 'query', label: 'Query' },
		{ id: 'forwarded', label: 'What was forwarded' },
		{ id: 'fwdresponse', label: 'Forward response' }
	]);
</script>

{#snippet step(n: string, title: string, sub: string, tone: 'plain' | 'good' | 'bad' = 'plain', hint?: string)}
	<li title={hint} class="bg-surface border border-line rounded-[12px] p-3.5 flex flex-col gap-1">
		<span class="font-mono text-xs text-faint">{n}</span>
		<span class="text-sm font-semibold {tone === 'bad' ? 'text-danger' : ''}">{title}</span>
		<span class="text-[13px] text-body">{sub}</span>
	</li>
{/snippet}

{#snippet bodyView(body: string, json: boolean)}
	{#if !body}
		<p class="m-0 px-5 py-[18px] text-[13px] text-body">No body.</p>
	{:else if json}
		<pre class="m-0 px-5 py-[18px] font-mono text-[13px] leading-[1.75] text-soft overflow-x-auto">{@html highlightJSON(body)}</pre>
	{:else}
		<pre class="m-0 px-5 py-[18px] font-mono text-[13px] leading-[1.75] text-soft overflow-x-auto whitespace-pre-wrap break-all">{body}</pre>
	{/if}
{/snippet}

{#snippet kv(entries: [string, string][], empty: string)}
	{#if entries.length}
		<dl class="m-0 px-5 py-[18px] grid grid-cols-[minmax(120px,max-content)_1fr] gap-x-6 gap-y-1.5 font-mono text-[13px]">
			{#each entries as [k, v], i (i)}
				<dt class="text-faint">{k}</dt>
				<dd class="m-0 text-soft break-all">{v}</dd>
			{/each}
		</dl>
	{:else}
		<p class="m-0 px-5 py-[18px] text-[13px] text-body">{empty}</p>
	{/if}
{/snippet}

<svelte:window
	onkeydown={(e) => e.key === 'Escape' && (typeMenu = false)}
	onclick={(e) => typeMenu && !typeMenuEl?.contains(e.target as Node) && (typeMenu = false)}
/>

<section aria-label="Request detail" class="flex-[999_1_560px] min-w-0 p-6 flex flex-col gap-5">
	<div class="flex justify-between items-start gap-4 flex-wrap">
		<div class="flex flex-col gap-1.5 min-w-0">
			<h1 class="m-0 font-mono text-xl font-medium break-all">
				<span class={request.method === 'GET' ? 'text-[var(--method-get)]' : 'text-accent'}>{request.method}</span>
				{request.path}{queryString(request)}
			</h1>
			<span class="text-[13px] text-body">
				{when(request.created_at)} · {formatBytes(request.size)} · from {request.ip}
				{#if request.signature?.status === 'verified'}
					· <span class="text-accent">signature verified</span>
				{:else if request.signature?.status === 'invalid'}
					· <span class="text-danger" title={request.signature.error}>signature invalid</span>
				{/if}
			</span>
		</div>
		<div class="flex gap-2 flex-wrap items-center">
			{#if replayed}<span class="text-[13px] text-body">{replayed}</span>{/if}
			<button type="button" onclick={copyCurl} class="text-[13px] font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-3.5 min-h-10 cursor-pointer hover:bg-chip">
				{copied ? 'Copied' : 'Copy as cURL'}
			</button>
			{#if isJSON}
				<div class="relative" bind:this={typeMenuEl}>
					<button
						type="button"
						onclick={() => (typeMenu = !typeMenu)}
						aria-haspopup="menu"
						aria-expanded={typeMenu}
						title="Copy the shape of this body as a type, guessed from this one request"
						class="text-[13px] font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-3.5 min-h-10 cursor-pointer hover:bg-chip"
					>
						{typeCopied ? 'Copied' : 'Copy as type'}
					</button>
					{#if typeMenu}
						<div role="menu" class="absolute right-0 top-[calc(100%+4px)] z-10 bg-surface border border-line-strong rounded-[9px] p-1 flex flex-col min-w-[190px] shadow-sm">
							<button type="button" role="menuitem" onclick={() => copyType('ts')} class="text-left text-[13px] font-medium text-ink rounded-[7px] px-3 min-h-9 cursor-pointer hover:bg-chip">Copy TypeScript type</button>
							<button type="button" role="menuitem" onclick={() => copyType('schema')} class="text-left text-[13px] font-medium text-ink rounded-[7px] px-3 min-h-9 cursor-pointer hover:bg-chip">Copy JSON Schema</button>
						</div>
					{/if}
				</div>
			{/if}
			<button type="button" onclick={doReplay} class="text-[13px] font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-3.5 min-h-10 cursor-pointer hover:bg-chip">Replay</button>
			<button type="button" onclick={onTryTransform} class="text-[13px] font-medium text-on-ink bg-ink rounded-[9px] px-3.5 min-h-10 flex items-center cursor-pointer hover:opacity-90">
				Test a transform on this
			</button>
			<button
				type="button"
				onclick={() => confirm('Delete this request?') && onDelete()}
				title="Delete this request"
				aria-label="Delete this request"
				class="text-[13px] font-medium text-soft bg-surface border border-line-strong rounded-[9px] px-3.5 min-h-10 cursor-pointer hover:text-danger hover:bg-chip"
			>
				Delete
			</button>
		</div>
	</div>

	<ol aria-label="What happened to this request" class="m-0 p-0 list-none grid grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-2.5">
		{@render step('1 · received', 'Captured', `${headerCount} headers, ${bodyKind(request)}`)}

		{#if outcome.kind === 'none'}
			{@render step('2 · transform', cfg.wasm_script ? 'Off' : 'None', 'forwarded as captured')}
		{:else if outcome.kind === 'error'}
			{@render step('2 · transform', 'Failed', 'original forwarded unchanged', 'bad')}
		{:else if outcome.kind === 'skipped'}
			{@render step('2 · transform', 'Skipped', 'returned null, not forwarded')}
		{:else}
			<li class="bg-surface border border-line rounded-[12px] p-3.5 flex flex-col gap-1">
				<span class="font-mono text-xs text-faint">2 · transform</span>
				<span class="text-sm font-semibold">{outcome.changed ? 'Changed' : 'Unchanged'}</span>
				<span class="text-[13px] text-body">
					{#if outcome.added || outcome.removed}
						{#if outcome.added}<span class="text-accent">+{outcome.added} field{outcome.added === 1 ? '' : 's'}</span>{/if}{#if outcome.added && outcome.removed}{', '}{/if}{#if outcome.removed}<span class="text-danger">−{outcome.removed} field{outcome.removed === 1 ? '' : 's'}</span>{/if}
					{:else}
						{outcome.changed ? 'headers or path' : 'no changes'}
					{/if}
					· {outcome.ms} ms
				</span>
			</li>
		{/if}

		{#if !cfg.forward_url}
			{@render step('3 · forward', 'None', 'no forward URL set')}
		{:else if outcome.kind === 'skipped'}
			{@render step('3 · forward', 'Skipped', hostOf(cfg.forward_url))}
		{:else if fwdPending || fwdRunning}
			{@render step('3 · forward', 'Sending…', hostOf(cfg.forward_url))}
		{:else if fwdStalled}
			{@render step('3 · forward', 'Never finished', `${hostOf(cfg.forward_url)} · the server stopped mid-forward`, 'bad', stalledHint)}
		{:else if forwardResult}
			{#if forwardResult.ok}
				{@render step('3 · forward', statusLine(forwardResult.status_code), `${hostOf(forwardResult.url)} · ${forwardResult.latency_ms} ms${forwardResult.resigned ? ' · re-signed' : forwardResult.resign_error ? ' · not re-signed' : ''}`)}
			{:else}
				{@render step('3 · forward', forwardResult.status_code > 0 ? statusLine(forwardResult.status_code) : 'Failed', forwardResult.error || hostOf(forwardResult.url), 'bad')}
			{/if}
		{:else}
			{#if endpoint.mode === 'server'}
				{@render step('3 · forward', 'No result', `${hostOf(cfg.forward_url)} · captured before results were saved, or the forward never finished`, 'plain', noResultHint)}
			{:else}
				{@render step('3 · forward', 'Sent', `${hostOf(cfg.forward_url)} · result not shown`)}
			{/if}
		{/if}

		{#if response}
			{@render step('4 · reply', statusLine(response.status), response.source === 'handler' ? 'custom reply' : response.source === 'forward_passthrough' ? "forward target's reply" : response.source === 'signature' ? 'signature rejected' : 'default response')}
		{:else if cfg.custom_response?.enabled}
			{@render step('4 · reply', 'Custom reply', 'status not shown')}
		{:else}
			{@render step('4 · reply', statusLine(endpoint.mode === 'browser' ? 202 : 200), 'default response')}
		{/if}
	</ol>

	<div class="bg-surface border border-line rounded-[14px] flex flex-col overflow-hidden">
		<div role="tablist" aria-label="Request views" class="flex gap-1 p-1.5 border-b border-line flex-wrap">
			{#each tabs as t (t.id)}
				<button
					type="button"
					role="tab"
					aria-selected={tab === t.id}
					onclick={() => (tab = t.id)}
					class="text-[13px] font-medium rounded-[7px] px-3 min-h-9 cursor-pointer {tab === t.id ? 'text-ink bg-chip' : 'text-body bg-transparent hover:text-ink'}"
				>
					{t.label}
				</button>
			{/each}
		</div>

		{#if tab === 'body'}
			{#if request.body_encoding === 'base64'}
				<p class="m-0 px-5 py-[18px] text-[13px] text-body">Binary body ({formatBytes(request.size)}). Copy as cURL or replay to send it again.</p>
			{:else}
				{@render bodyView(isJSON ? prettyBody : text, isJSON)}
			{/if}
		{:else if tab === 'diff'}
			{#if !earlier}
				<p class="m-0 px-5 py-[18px] text-[13px] text-body">Nothing earlier on this endpoint yet.</p>
			{:else}
				<div class="px-5 pt-[18px] text-[13px] text-body">
					Compared with <span class="font-mono text-soft">{earlier.method} {earlier.path}</span> · {ago(earlier.created_at)}
				</div>
				{#if diff?.binary}
					<p class="m-0 px-5 py-[18px] text-[13px] text-body">One of the bodies is binary, so there are no lines to compare.</p>
				{:else if diff?.same}
					<p class="m-0 px-5 py-[18px] text-[13px] text-body">Same body as the earlier request.</p>
				{:else if diff}
					<pre class="m-0 py-[18px] font-mono text-[13px] leading-[1.75] text-soft overflow-x-auto">{#each diff.lines as l, i (i)}<span class="block px-5 {l.kind === '+' ? 'bg-[#E6F4EC] text-[#0F5132] dark:bg-[#12301f] dark:text-[#7ee2a8]' : l.kind === '-' ? 'bg-[#FDECEA] text-[#8A1C12] dark:bg-[#3a1512] dark:text-[#f9a8a0]' : ''}">{#if l.kind !== ' '}<span aria-hidden="true">{l.kind === '+' ? '+ ' : '− '}</span>{/if}{l.text}</span>{/each}</pre>
				{/if}
			{/if}
		{:else if tab === 'headers'}
			{@render kv(Object.entries(request.headers ?? {}).map(([k, v]) => [k, v.join(', ')]), 'No headers.')}
			{#if request.hidden_headers}
				<p class="m-0 px-5 pb-[18px] text-[13px] text-body">
					{request.hidden_headers} more header{request.hidden_headers === 1 ? ' was' : 's were'} added by the hosting platform, not the sender, so {request.hidden_headers === 1 ? "it isn't" : "they aren't"} shown or forwarded. Set <code class="font-mono">HIDE_HEADERS</code> to change which.
				</p>
			{/if}
		{:else if tab === 'query'}
			{@render kv(Object.entries(request.query ?? {}).map(([k, v]) => [k, v.join(', ')]), 'No query parameters.')}
		{:else if tab === 'forwarded'}
			{#if !cfg.forward_url}
				<p class="m-0 px-5 py-[18px] text-[13px] text-body">Nothing is forwarded: this endpoint has no forward URL.</p>
			{:else if outcome.kind === 'skipped'}
				<p class="m-0 px-5 py-[18px] text-[13px] text-body">Not forwarded: the transform returned <code class="font-mono">null</code>.</p>
			{:else}
				<div class="px-5 pt-[18px] font-mono text-[13px] text-soft">
					<span class={forwardedReq.method === 'GET' ? 'text-[var(--method-get)]' : 'text-accent'}>{forwardedReq.method}</span>
					{cfg.forward_url.replace(/\/$/, '')}{forwardedReq.path}
				</div>
				{#if forwardResult?.resigned}
					<p class="m-0 px-5 pt-2 text-[13px] text-body">Re-signed: sent with a fresh <code class="font-mono">webhook-timestamp</code> and <code class="font-mono">webhook-signature</code> over this body, in place of the ones below.</p>
				{:else if forwardResult?.resign_error}
					<p class="m-0 px-5 pt-2 text-[13px] text-danger">{forwardResult.resign_error[0].toUpperCase() + forwardResult.resign_error.slice(1)}. Sent with the headers below.</p>
				{:else if cfg.signature?.resign}
					<p class="m-0 px-5 pt-2 text-[13px] text-body">When forwarded, a request signed with the webhook secret gets a fresh <code class="font-mono">webhook-timestamp</code> and <code class="font-mono">webhook-signature</code> over this body.</p>
				{/if}
				{@render kv(Object.entries(forwardedReq.headers ?? {}).map(([k, v]) => [k, (v ?? []).join(', ')]), 'No headers.')}
				<div class="border-t border-line">
					{@render bodyView(pretty(forwardedReq.body ?? ''), parseJSON(forwardedReq.body ?? '') !== undefined)}
				</div>
			{/if}
		{:else}
			{#if !forwardResult || fwdPending || forwardResult.in_progress}
				<p class="m-0 px-5 py-[18px] text-[13px] text-body">
					{#if !cfg.forward_url}This endpoint doesn't forward.{:else if fwdPending || fwdRunning}Waiting for the forward target…{:else if fwdStalled}{stalledHint}{:else if endpoint.mode === 'server'}{noResultHint}{:else}In browser mode, the forward target's reply is shown for requests that arrive while this page is open.{/if}
				</p>
			{:else}
				<div class="px-5 pt-[18px] font-mono text-[13px] {forwardResult.ok ? 'text-accent' : 'text-danger'}">
					{forwardResult.status_code > 0 ? statusLine(forwardResult.status_code) : 'No response'} · {forwardResult.latency_ms} ms{#if forwardResult.error && forwardResult.error !== '__pending__'} · {forwardResult.error}{/if}
				</div>
				{@render kv(Object.entries(forwardResult.response_headers ?? {}), 'No headers.')}
				{#if forwardResult.response_body_truncated}
					<p class="m-0 px-5 pt-[14px] text-[13px] text-body">The reply was longer than the server stores (<code class="font-mono">MAX_STORED_RESPONSE_SIZE</code>, 64 KB by default), so only its start is shown.</p>
				{/if}
				<div class="border-t border-line">
					{@render bodyView(pretty(forwardResult.response_body ?? ''), parseJSON(forwardResult.response_body ?? '') !== undefined)}
				</div>
			{/if}
		{/if}
	</div>
</section>
