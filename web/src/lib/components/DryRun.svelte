<script lang="ts">
	import { initWasm, runTransform, runResponseHandler } from '$lib/wasm';
	import type { CapturedRequest, ForwardResponse } from '$lib/types';
	import { Play, Loader2 } from 'lucide-svelte';

	let {
		request,
		transformScript,
		handlerScript,
		forwardUrl,
		forwardMode
	}: {
		request: CapturedRequest | null;
		/** Empty when the transform is off */
		transformScript: string;
		/** Empty when the custom reply is off */
		handlerScript: string;
		forwardUrl?: string;
		forwardMode?: string;
	} = $props();

	type Line = { kind: ' ' | '+' | '-'; text: string };
	type Result = {
		transform: { kind: 'none' } | { kind: 'error'; error: string } | { kind: 'skipped' } | { kind: 'ok'; changes: string[]; body: Line[]; ms: number };
		forward: string;
		reply: { kind: 'default' } | { kind: 'error'; error: string } | { kind: 'ok'; status: number; headers: Record<string, string>; body: string; ms: number };
		usedSampleUpstream: boolean;
	};

	let running = $state(false);
	let result = $state<Result | null>(null);

	// Stand-in for the forward target's reply, so handlers that read
	// req.forward_response can be tried without calling the target.
	const sampleUpstream: ForwardResponse = {
		status: 200,
		headers: { 'content-type': 'application/json' },
		body: '{"ok":true}',
		content_type: 'application/json'
	};

	function pretty(body: string | null | undefined): string {
		if (!body) return '';
		try {
			return JSON.stringify(JSON.parse(body), null, 2);
		} catch {
			return body;
		}
	}

	// Line diff (LCS). Bodies are small; give up on huge ones and show the result only.
	function diffLines(a: string, b: string): Line[] {
		const x = a ? a.split('\n') : [];
		const y = b ? b.split('\n') : [];
		if (x.length * y.length > 250_000) return y.map((text) => ({ kind: ' ', text }));
		const dp = Array.from({ length: x.length + 1 }, () => new Array<number>(y.length + 1).fill(0));
		for (let i = x.length - 1; i >= 0; i--)
			for (let j = y.length - 1; j >= 0; j--)
				dp[i][j] = x[i] === y[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
		const out: Line[] = [];
		let i = 0;
		let j = 0;
		while (i < x.length && j < y.length) {
			if (x[i] === y[j]) out.push({ kind: ' ', text: x[i++] }), j++;
			else if (dp[i + 1][j] >= dp[i][j + 1]) out.push({ kind: '-', text: x[i++] });
			else out.push({ kind: '+', text: y[j++] });
		}
		while (i < x.length) out.push({ kind: '-', text: x[i++] });
		while (j < y.length) out.push({ kind: '+', text: y[j++] });
		return out;
	}

	function headerChanges(before: Record<string, string[]>, after: Record<string, string[]>): string[] {
		const lower = (h: Record<string, string[]>) => new Map(Object.entries(h ?? {}).map(([k, v]) => [k.toLowerCase(), v.join(', ')]));
		const a = lower(before);
		const b = lower(after);
		const out: string[] = [];
		for (const [k, v] of b) {
			if (!a.has(k)) out.push(`+ header ${k}: ${v}`);
			else if (a.get(k) !== v) out.push(`~ header ${k}: ${v}`);
		}
		for (const k of a.keys()) if (!b.has(k)) out.push(`− header ${k}`);
		return out;
	}

	async function run() {
		if (!request) return;
		running = true;
		result = null;
		try {
			await initWasm();
			let forwarded: CapturedRequest = request;
			let transform: Result['transform'] = { kind: 'none' };
			let skipped = false;

			if (transformScript) {
				const t = await runTransform(transformScript, request);
				if (!t.ok) transform = { kind: 'error', error: t.error ?? 'Unknown error' };
				else if (t.dropped) {
					transform = { kind: 'skipped' };
					skipped = true;
				} else {
					forwarded = { ...request, ...t.data } as CapturedRequest;
					const changes: string[] = [];
					if (forwarded.method !== request.method) changes.push(`~ method ${request.method} → ${forwarded.method}`);
					if (forwarded.path !== request.path) changes.push(`~ path ${request.path} → ${forwarded.path}`);
					changes.push(...headerChanges(request.headers, forwarded.headers));
					transform = { kind: 'ok', changes, body: diffLines(pretty(request.body), pretty(forwarded.body)), ms: t.duration };
				}
			}

			let forward: string;
			if (!forwardUrl) forward = 'No forward URL is set, so nothing would be forwarded.';
			else if (skipped) forward = `Not forwarded: the transform returned null.`;
			else forward = `Would be sent to ${forwardUrl} (${forwardMode === 'sync' ? 'sync' : 'async'}). A dry run doesn't send it.`;

			const usedSampleUpstream = !!handlerScript && !!forwardUrl && forwardMode === 'sync' && !skipped;
			// The server sends the default reply for a skipped request without
			// running the handler.
			let reply: Result['reply'] = { kind: 'default' };
			if (handlerScript && !skipped) {
				const r = await runResponseHandler(handlerScript, forwarded, 'javascript', usedSampleUpstream ? sampleUpstream : null);
				reply = r.ok
					? { kind: 'ok', status: r.response.status || 200, headers: r.response.headers ?? {}, body: r.response.body ?? '', ms: r.duration }
					: { kind: 'error', error: r.error ?? 'Unknown error' };
			}
			result = { transform, forward, reply, usedSampleUpstream };
		} finally {
			running = false;
		}
	}
</script>

<div class="flex flex-col gap-3 bg-[var(--bg-card)] border border-[var(--border)] rounded-lg p-3">
	<div class="flex items-center justify-between gap-2 flex-wrap">
		<div>
			<h3 class="text-sm font-semibold">Dry run</h3>
			<p class="text-xs text-[var(--text-muted)]">Runs your unsaved scripts on the test input, in the same order the server does: transform, forward, reply.</p>
		</div>
		<button
			onclick={run}
			disabled={running || !request}
			class="text-xs px-3 py-1.5 rounded bg-[var(--accent)] hover:bg-[var(--accent-hover)] text-white transition-colors flex items-center gap-1.5 disabled:opacity-50"
			title={request ? 'Run both scripts on the test input' : 'Choose a test input first'}
		>
			{#if running}<Loader2 class="w-3 h-3 animate-spin" />{:else}<Play class="w-3 h-3" />{/if}
			Dry run
		</button>
	</div>

	{#if result}
		<ol class="flex flex-col gap-2 text-xs">
			<li class="border border-[var(--border)] rounded">
				<div class="px-3 py-2 border-b border-[var(--border)] flex justify-between font-medium">
					<span>1 · Transform</span>
					{#if result.transform.kind === 'ok'}<span class="text-[var(--green)]">{result.transform.ms} ms</span>{/if}
				</div>
				<div class="px-3 py-2">
					{#if result.transform.kind === 'none'}
						<span class="text-[var(--text-muted)]">No transform, so the request is forwarded as captured.</span>
					{:else if result.transform.kind === 'error'}
						<p class="text-[var(--red)] font-mono whitespace-pre-wrap">{result.transform.error}</p>
						<p class="mt-1 text-[var(--text-muted)]">On a real request the original would be forwarded unchanged.</p>
					{:else if result.transform.kind === 'skipped'}
						<span>Returned <code class="font-mono">null</code>: the request is captured but not forwarded.</span>
					{:else}
						{#if result.transform.changes.length}
							<ul class="font-mono text-[11px] mb-2">
								{#each result.transform.changes as c (c)}<li>{c}</li>{/each}
							</ul>
						{/if}
						{#if result.transform.body.some((l) => l.kind !== ' ')}
							<pre class="font-mono text-[11px] overflow-x-auto max-h-72 border border-[var(--border)] rounded py-1">{#each result.transform.body as l, i (i)}<span class="block px-2 {l.kind === '+' ? 'bg-green-500/10 text-[var(--green)]' : l.kind === '-' ? 'bg-red-500/10 text-[var(--red)]' : 'text-[var(--text-muted)]'}">{l.kind} {l.text}</span>{/each}</pre>
						{:else if !result.transform.changes.length}
							<span class="text-[var(--text-muted)]">Nothing changed.</span>
						{:else}
							<span class="text-[var(--text-muted)]">Body unchanged.</span>
						{/if}
					{/if}
				</div>
			</li>

			<li class="border border-[var(--border)] rounded">
				<div class="px-3 py-2 border-b border-[var(--border)] font-medium">2 · Forward</div>
				<div class="px-3 py-2 text-[var(--text-muted)] break-all">{result.forward}</div>
			</li>

			<li class="border border-[var(--border)] rounded">
				<div class="px-3 py-2 border-b border-[var(--border)] flex justify-between font-medium">
					<span>3 · Reply to the sender</span>
					{#if result.reply.kind === 'ok'}<span class="text-[var(--green)]">{result.reply.ms} ms</span>{/if}
				</div>
				<div class="px-3 py-2">
					{#if result.reply.kind === 'default'}
						<span class="text-[var(--text-muted)]">{result.transform.kind === 'skipped' && handlerScript ? "Skipped requests don't run the custom reply" : 'No custom reply'}: the default <code class="font-mono">200</code> with <code class="font-mono">{'{"id": "…", "status": "ok"}'}</code>.</span>
					{:else if result.reply.kind === 'error'}
						<p class="text-[var(--red)] font-mono whitespace-pre-wrap">{result.reply.error}</p>
						<p class="mt-1 text-[var(--text-muted)]">On a real request the default reply would be sent.</p>
					{:else}
						<div class="font-mono text-[11px]">
							<div><span class="text-[var(--accent)]">{result.reply.status}</span></div>
							{#each Object.entries(result.reply.headers) as [k, v] (k)}<div class="text-[var(--text-muted)]">{k}: {v}</div>{/each}
							{#if result.reply.body}<pre class="mt-1 whitespace-pre-wrap break-all">{pretty(result.reply.body)}</pre>{/if}
						</div>
					{/if}
					{#if result.usedSampleUpstream}
						<p class="mt-2 text-[11px] text-[var(--text-muted)]">req.forward_response was a stand-in (<code class="font-mono">200 {'{"ok":true}'}</code>); the target wasn't called.</p>
					{/if}
				</div>
			</li>
		</ol>
	{/if}
</div>
