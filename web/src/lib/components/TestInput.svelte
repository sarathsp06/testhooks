<script lang="ts">
	import CodeEditor from './CodeEditor.svelte';
	import { formatRequestReference } from '$lib/wasm';
	import { SAMPLE_PRESETS, draftToRequest, requestToDraft, type SampleDraft } from '$lib/samples';
	import type { CapturedRequest } from '$lib/types';
	import { BookOpen, ChevronDown, ChevronRight, Pencil } from 'lucide-svelte';

	let {
		captured,
		slug,
		request = $bindable(null)
	}: {
		/** The selected request, or the latest one */
		captured: CapturedRequest | null;
		slug: string;
		/** The request scripts are tested against */
		request?: CapturedRequest | null;
	} = $props();

	const storageKey = $derived(`testhooks:sample:${slug}`);

	function loadDraft(): SampleDraft {
		try {
			const saved = localStorage.getItem(storageKey);
			if (saved) return JSON.parse(saved) as SampleDraft;
		} catch {
			// Storage unavailable or corrupt: fall back to a preset.
		}
		return { ...SAMPLE_PRESETS[0].draft };
	}

	let draft = $state<SampleDraft>(loadDraft());
	// Until the user picks, follow whether there's a captured request (they load async).
	let chosen = $state<'captured' | 'sample' | null>(null);
	const source = $derived(chosen ?? (captured ? 'captured' : 'sample'));
	let showShape = $state(false);

	// Remember the sample per endpoint so it survives a reload.
	$effect(() => {
		const json = JSON.stringify(draft);
		try {
			localStorage.setItem(storageKey, json);
		} catch {
			// Not essential.
		}
	});

	$effect(() => {
		request = source === 'captured' ? captured : draftToRequest(draft);
	});

	const bodyIsJSON = $derived.by(() => {
		if (!draft.body.trim()) return null;
		try {
			JSON.parse(draft.body);
			return true;
		} catch {
			return false;
		}
	});

	function usePreset(d: SampleDraft) {
		draft = { ...d };
		chosen = 'sample';
	}

	function editCaptured() {
		if (!captured) return;
		draft = requestToDraft(captured);
		chosen = 'sample';
	}

	function formatBody() {
		try {
			draft.body = JSON.stringify(JSON.parse(draft.body), null, 2);
		} catch {
			// Leave invalid JSON as typed.
		}
	}

	function ago(iso: string): string {
		const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
		if (s < 60) return `${s}s ago`;
		if (s < 3600) return `${Math.round(s / 60)} min ago`;
		return new Date(iso).toLocaleString();
	}

	const methods = ['POST', 'PUT', 'PATCH', 'GET', 'DELETE'];
</script>

<div class="flex flex-col gap-3 bg-[var(--bg-card)] border border-[var(--border)] rounded-lg p-3">
	<div class="flex items-center justify-between gap-2 flex-wrap">
		<div>
			<h3 class="text-sm font-semibold">Test input</h3>
			<p class="text-xs text-[var(--text-muted)]">The request your scripts run against when you test or dry run. Nothing is sent anywhere.</p>
		</div>
		<div class="flex items-center border border-[var(--border)] rounded overflow-hidden" role="radiogroup" aria-label="Test input source">
			<button
				role="radio"
				aria-checked={source === 'captured'}
				onclick={() => (chosen = 'captured')}
				class="text-[11px] px-2.5 py-1 transition-colors {source === 'captured' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
			>
				Captured request
			</button>
			<button
				role="radio"
				aria-checked={source === 'sample'}
				onclick={() => (chosen = 'sample')}
				class="text-[11px] px-2.5 py-1 transition-colors {source === 'sample' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
			>
				Sample payload
			</button>
		</div>
	</div>

	{#if source === 'captured'}
		{#if captured}
			<div class="flex items-center justify-between gap-2 flex-wrap text-xs bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-2">
				<span class="font-mono">
					<span class="text-[var(--accent)]">{captured.method}</span>
					{captured.path}
					<span class="text-[var(--text-muted)]">· {captured.content_type || 'no content type'} · {ago(captured.created_at)}</span>
				</span>
				<button
					onclick={editCaptured}
					class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1"
					title="Copy this request into the sample editor to change it"
				>
					<Pencil class="w-3 h-3" />
					Edit a copy
				</button>
			</div>
			<p class="text-[11px] text-[var(--text-muted)]">Pick another request in the list on the left to test against it.</p>
		{:else}
			<div class="text-xs text-[var(--text-muted)] bg-[var(--bg)] border border-dashed border-[var(--border)] rounded px-3 py-3">
				No requests captured yet. Send one to your webhook URL, or
				<button class="underline hover:no-underline text-[var(--text)]" onclick={() => (chosen = 'sample')}>use a sample payload</button>.
			</div>
		{/if}
	{:else}
		<div class="flex items-center gap-1.5 flex-wrap">
			<span class="text-[11px] text-[var(--text-muted)] mr-1">Start from</span>
			{#each SAMPLE_PRESETS as preset (preset.id)}
				<button
					onclick={() => usePreset(preset.draft)}
					class="text-[11px] px-2.5 py-1 rounded-full border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
				>
					{preset.label}
				</button>
			{/each}
		</div>

		<div class="flex gap-2">
			<label class="sr-only" for="sample-method">Method</label>
			<select
				id="sample-method"
				bind:value={draft.method}
				class="text-xs font-mono bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5"
			>
				{#each methods as m (m)}
					<option value={m}>{m}</option>
				{/each}
			</select>
			<label class="sr-only" for="sample-path">Path and query</label>
			<input
				id="sample-path"
				bind:value={draft.path}
				placeholder="/events?debug=1"
				class="flex-1 min-w-0 text-xs font-mono bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5"
			/>
		</div>

		<label class="flex flex-col gap-1 text-[11px] text-[var(--text-muted)]">
			<span>Headers, one per line as <code class="font-mono">Name: value</code></span>
			<textarea
				bind:value={draft.headers}
				rows="3"
				spellcheck="false"
				class="text-xs font-mono text-[var(--text)] bg-[var(--bg)] border border-[var(--border)] rounded px-2 py-1.5 resize-y"
			></textarea>
		</label>

		<div class="flex flex-col gap-1">
			<div class="flex items-center justify-between gap-2">
				<span class="text-[11px] text-[var(--text-muted)]">Body: paste your own payload here</span>
				<span class="flex items-center gap-2 text-[11px]">
					{#if bodyIsJSON === true}
						<span class="text-[var(--green)]">Valid JSON, available as req.json</span>
						<button onclick={formatBody} class="underline hover:no-underline text-[var(--text-muted)]">Format</button>
					{:else if bodyIsJSON === false}
						<span class="text-[var(--yellow)]">Not JSON, so req.json is null</span>
					{/if}
				</span>
			</div>
			<CodeEditor value={draft.body} onchange={(v) => (draft.body = v)} language="json" minHeight="120px" placeholder={'{ "type": "invoice.paid" }'} />
		</div>
	{/if}

	{#if request}
		<div>
			<button
				onclick={() => (showShape = !showShape)}
				class="text-[11px] text-[var(--text-muted)] hover:text-[var(--text)] flex items-center gap-1"
				aria-expanded={showShape}
			>
				{#if showShape}<ChevronDown class="w-3 h-3" />{:else}<ChevronRight class="w-3 h-3" />{/if}
				<BookOpen class="w-3 h-3" />
				What your script gets as <code class="font-mono">req</code>
			</button>
			{#if showShape}
				<pre class="mt-2 px-3 py-2 text-[11px] font-mono text-[var(--text-muted)] bg-[var(--bg)] border border-[var(--border)] rounded overflow-x-auto max-h-64 leading-relaxed whitespace-pre">{formatRequestReference(request)}

// Helpers: req.header("name"), req.setHeader("name", value), req.removeHeader("name")</pre>
			{/if}
		</div>
	{/if}
</div>
