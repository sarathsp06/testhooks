<script lang="ts">
	import CodeEditor from './CodeEditor.svelte';
	import DryRun from './DryRun.svelte';
	import { SAMPLE_PRESETS, draftToRequest, requestToDraft, type SampleDraft } from '$lib/samples';
	import type { CapturedRequest } from '$lib/types';
	import { Pencil } from 'lucide-svelte';

	let {
		captured,
		selected,
		slug,
		transformScript,
		handlerScript,
		forwardUrl,
		forwardMode,
		request = $bindable(null)
	}: {
		/** Captured requests, newest first */
		captured: CapturedRequest[];
		/** The request selected in the list, if any */
		selected: CapturedRequest | null;
		slug: string;
		transformScript: string;
		handlerScript: string;
		forwardUrl?: string;
		forwardMode?: string;
		/** The request scripts are tried on */
		request?: CapturedRequest | null;
	} = $props();

	const storageKey = $derived(`testhooks:sample:${slug}`);

	function loadDraft(): SampleDraft {
		try {
			const saved = localStorage.getItem(storageKey);
			if (saved) return JSON.parse(saved) as SampleDraft;
		} catch {
			// Storage unavailable or corrupt: start from a preset.
		}
		return { ...SAMPLE_PRESETS[0].draft };
	}

	// "cap:<id>", "sample:<preset id>" or "custom". Until the user picks, follow
	// the selected request, else the latest one, else the first sample.
	let chosen = $state<string | null>(null);
	let draft = $state<SampleDraft>(loadDraft());

	const recent = $derived(captured.slice(0, 20));
	const choice = $derived(
		chosen ??
			(selected ? `cap:${selected.id}` : recent[0] ? `cap:${recent[0].id}` : `sample:${SAMPLE_PRESETS[0].id}`)
	);

	$effect(() => {
		const json = JSON.stringify(draft);
		try {
			localStorage.setItem(storageKey, json);
		} catch {
			// Not essential.
		}
	});

	$effect(() => {
		if (choice === 'custom') {
			request = draftToRequest(draft);
		} else if (choice.startsWith('sample:')) {
			const preset = SAMPLE_PRESETS.find((p) => `sample:${p.id}` === choice) ?? SAMPLE_PRESETS[0];
			request = draftToRequest(preset.draft);
		} else {
			const id = choice.slice(4);
			request = captured.find((r) => r.id === id) ?? (selected?.id === id ? selected : null);
		}
	});

	// Edit: copy whatever is shown into the editable request.
	function edit() {
		if (request) draft = requestToDraft(request);
		chosen = 'custom';
	}

	function ago(iso: string): string {
		const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
		if (s < 60) return `${s}s ago`;
		if (s < 3600) return `${Math.round(s / 60)} min ago`;
		return new Date(iso).toLocaleString();
	}

	function preview(req: CapturedRequest): { headers: string[]; more: number; label: string; body: string } {
		const all = Object.entries(req.headers ?? {}).map(([k, v]) => `${k}: ${v.join(', ')}`);
		const body = req.body ?? '';
		let label = 'req.body';
		let shown = body;
		try {
			if (body) {
				shown = JSON.stringify(JSON.parse(body), null, 2);
				label = 'req.json';
			}
		} catch {
			// Not JSON: show the raw body.
		}
		const lines = shown.split('\n');
		if (lines.length > 14) shown = lines.slice(0, 14).join('\n') + '\n…';
		return { headers: all.slice(0, 4), more: Math.max(0, all.length - 4), label, body: shown };
	}

	const bodyIsJSON = $derived.by(() => {
		if (!draft.body.trim()) return null;
		try {
			JSON.parse(draft.body);
			return true;
		} catch {
			return false;
		}
	});

	function formatBody() {
		try {
			draft.body = JSON.stringify(JSON.parse(draft.body), null, 2);
		} catch {
			// Leave invalid JSON as typed.
		}
	}

	const methods = ['POST', 'PUT', 'PATCH', 'GET', 'DELETE'];
</script>

<section class="flex flex-col gap-3 bg-[var(--bg-card)] border border-[var(--border)] rounded-lg p-3" aria-label="Try your scripts">
	<h3 class="text-sm font-semibold">Try your scripts</h3>

	<label class="flex items-center gap-2 text-xs flex-wrap">
		<span class="text-[var(--text-muted)]">Try it on</span>
		<select
			value={choice}
			onchange={(e) => (chosen = e.currentTarget.value)}
			class="flex-1 min-w-0 text-xs bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5"
		>
			{#if recent.length}
				<optgroup label="Captured requests">
					{#each recent as r (r.id)}
						<option value={`cap:${r.id}`}>{r.method} {r.path} · {ago(r.created_at)}</option>
					{/each}
				</optgroup>
			{/if}
			<optgroup label="Sample requests">
				{#each SAMPLE_PRESETS as p (p.id)}
					<option value={`sample:${p.id}`}>{p.label}</option>
				{/each}
			</optgroup>
			<option value="custom">Your own request…</option>
		</select>
	</label>

	{#if choice === 'custom'}
		<div class="flex flex-col gap-2 border border-[var(--border)] rounded p-2">
			<div class="flex gap-2">
				<label class="sr-only" for="try-method">Method</label>
				<select id="try-method" bind:value={draft.method} class="text-xs font-mono bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5">
					{#each methods as m (m)}<option value={m}>{m}</option>{/each}
				</select>
				<label class="sr-only" for="try-path">Path and query</label>
				<input id="try-path" bind:value={draft.path} placeholder="/events?debug=1" class="flex-1 min-w-0 text-xs font-mono bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5" />
			</div>
			<label class="flex flex-col gap-1 text-[11px] text-[var(--text-muted)]">
				<span>Headers, one per line as <code class="font-mono">Name: value</code></span>
				<textarea bind:value={draft.headers} rows="3" spellcheck="false" class="text-xs font-mono text-[var(--text)] bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5 resize-y"></textarea>
			</label>
			<div class="flex flex-col gap-1">
				<div class="flex items-center justify-between gap-2 text-[11px]">
					<span class="text-[var(--text-muted)]">Body: type or paste a payload</span>
					<span class="flex items-center gap-2">
						{#if bodyIsJSON === true}
							<span class="text-[var(--green)]">Valid JSON, available as req.json</span>
							<button onclick={formatBody} class="underline hover:no-underline text-[var(--text-muted)]">Format</button>
						{:else if bodyIsJSON === false}
							<span class="text-[var(--yellow)]">Not JSON, so req.json is null</span>
						{/if}
					</span>
				</div>
				<CodeEditor value={draft.body} onchange={(v) => (draft.body = v)} language="json" minHeight="100px" placeholder={'{ "type": "invoice.paid" }'} />
			</div>
		</div>
	{:else if request}
		{@const p = preview(request)}
		<div class="relative bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-2 font-mono text-[11px] leading-relaxed">
			<button
				onclick={edit}
				class="absolute top-1.5 right-1.5 font-sans text-[11px] px-2 py-0.5 rounded border border-[var(--border)] bg-[var(--bg-card)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] flex items-center gap-1"
				title="Change the method, path, headers or body"
			>
				<Pencil class="w-3 h-3" />
				Edit
			</button>
			<div><span class="text-[var(--accent)]">{request.method}</span> {request.path}{#if request.query && Object.keys(request.query).length}?{new URLSearchParams(Object.entries(request.query).flatMap(([k, vs]) => vs.map((v) => [k, v]))).toString()}{/if}</div>
			{#each p.headers as h (h)}<div class="text-[var(--text-muted)] truncate pr-16">{h}</div>{/each}
			{#if p.more}<div class="text-[var(--text-muted)]">+{p.more} more headers</div>{/if}
			{#if p.body}
				<div class="mt-1 text-[var(--text-muted)] font-sans">{p.label}</div>
				<pre class="whitespace-pre-wrap break-all max-h-56 overflow-y-auto">{p.body}</pre>
			{:else}
				<div class="mt-1 text-[var(--text-muted)] font-sans">No body</div>
			{/if}
		</div>
	{/if}

	<DryRun {request} {transformScript} {handlerScript} {forwardUrl} {forwardMode} />
</section>
