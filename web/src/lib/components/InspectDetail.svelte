<script lang="ts">
	import { initWasm, runTransform } from '$lib/wasm';
	import { copyToClipboard, formatBytes } from '$lib/utils';
	import { bodyKind, bodyText, curlFor, highlightJSON, hostOf, parseJSON, queryString, replay, statusLine, when } from '$lib/requestutil';
	import { fieldChanges, pretty } from '$lib/scriptdiff';
	import type { CapturedRequest, CapturedResponse, Endpoint, ForwardResultData } from '$lib/types';

	let {
		request,
		endpoint,
		response,
		forwardResult,
		onTryTransform
	}: {
		request: CapturedRequest;
		endpoint: Endpoint;
		response?: CapturedResponse;
		forwardResult?: ForwardResultData;
		/** Open the transform editor with this request as the input. */
		onTryTransform: () => void;
	} = $props();

	type Tab = 'body' | 'headers' | 'query' | 'forwarded' | 'fwdresponse';
	let tab = $state<Tab>('body');
	let copied = $state(false);
	let replayed = $state<string | null>(null);

	const cfg = $derived(endpoint.config ?? {});
	const script = $derived(cfg.wasm_script && !cfg.transform_disabled && (!cfg.transform_language || cfg.transform_language === 'javascript') ? cfg.wasm_script : '');
	const headerCount = $derived(Object.keys(request.headers ?? {}).length);
	const text = $derived(bodyText(request));
	const prettyBody = $derived(pretty(text));
	const isJSON = $derived(bodyKind(request) === 'JSON body');

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
		{ id: 'headers', label: `Headers ${headerCount}` },
		{ id: 'query', label: 'Query' },
		{ id: 'forwarded', label: 'What was forwarded' },
		{ id: 'fwdresponse', label: 'Forward response' }
	]);
</script>

{#snippet step(n: string, title: string, sub: string, tone: 'plain' | 'good' | 'bad' = 'plain')}
	<li class="bg-surface border border-line rounded-[12px] p-3.5 flex flex-col gap-1">
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
			<button type="button" onclick={doReplay} class="text-[13px] font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-3.5 min-h-10 cursor-pointer hover:bg-chip">Replay</button>
			<button type="button" onclick={onTryTransform} class="text-[13px] font-medium text-on-ink bg-ink rounded-[9px] px-3.5 min-h-10 flex items-center cursor-pointer hover:opacity-90">
				Test a transform on this
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
		{:else if fwdPending}
			{@render step('3 · forward', 'Sending…', hostOf(cfg.forward_url))}
		{:else if forwardResult}
			{#if forwardResult.ok}
				{@render step('3 · forward', statusLine(forwardResult.status_code), `${hostOf(forwardResult.url)} · ${forwardResult.latency_ms} ms`)}
			{:else}
				{@render step('3 · forward', forwardResult.status_code > 0 ? statusLine(forwardResult.status_code) : 'Failed', forwardResult.error || hostOf(forwardResult.url), 'bad')}
			{/if}
		{:else}
			{@render step('3 · forward', 'Sent', `${hostOf(cfg.forward_url)} · result not shown`)}
		{/if}

		{#if response}
			{@render step('4 · reply', statusLine(response.status), response.source === 'handler' ? 'custom reply' : response.source === 'forward_passthrough' ? "forward target's reply" : 'default response')}
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
		{:else if tab === 'headers'}
			{@render kv(Object.entries(request.headers ?? {}).map(([k, v]) => [k, v.join(', ')]), 'No headers.')}
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
				{@render kv(Object.entries(forwardedReq.headers ?? {}).map(([k, v]) => [k, (v ?? []).join(', ')]), 'No headers.')}
				<div class="border-t border-line">
					{@render bodyView(pretty(forwardedReq.body ?? ''), parseJSON(forwardedReq.body ?? '') !== undefined)}
				</div>
			{/if}
		{:else}
			{#if !forwardResult || fwdPending}
				<p class="m-0 px-5 py-[18px] text-[13px] text-body">
					{#if !cfg.forward_url}This endpoint doesn't forward.{:else if fwdPending}Waiting for the forward target…{:else}The forward target's reply is shown for requests that arrive while this page is open.{/if}
				</p>
			{:else}
				<div class="px-5 pt-[18px] font-mono text-[13px] {forwardResult.ok ? 'text-accent' : 'text-danger'}">
					{forwardResult.status_code > 0 ? statusLine(forwardResult.status_code) : 'No response'} · {forwardResult.latency_ms} ms{#if forwardResult.error && forwardResult.error !== '__pending__'} · {forwardResult.error}{/if}
				</div>
				{@render kv(Object.entries(forwardResult.response_headers ?? {}), 'No headers.')}
				<div class="border-t border-line">
					{@render bodyView(pretty(forwardResult.response_body ?? ''), parseJSON(forwardResult.response_body ?? '') !== undefined)}
				</div>
			{/if}
		{/if}
	</div>
</section>
