<script lang="ts">
	import CodeEditor from './CodeEditor.svelte';
	import { initWasm, runTransform, runResponseHandler, isLegacyLanguage } from '$lib/wasm';
	import { SAMPLE_PRESETS, draftToRequest, requestToDraft, type SampleDraft } from '$lib/samples';
	import { ago, bodyText, eventLabel, parseJSON, queryString, statusLine } from '$lib/requestutil';
	import { diffLines, fieldChanges, otherChanges, pretty, type Line } from '$lib/scriptdiff';
	import type { CapturedRequest, Endpoint, ForwardResponse } from '$lib/types';

	let {
		kind,
		endpoint,
		captured,
		initialRequestId = null,
		onSave,
		onClose
	}: {
		kind: 'transform' | 'reply';
		endpoint: Endpoint;
		/** Captured requests, newest first */
		captured: CapturedRequest[];
		/** Start with this captured request as the input */
		initialRequestId?: string | null;
		/** Save the script; resolves when stored */
		onSave: (script: string, enabled: boolean) => Promise<void>;
		onClose: () => void;
	} = $props();

	const isTransform = $derived(kind === 'transform');
	const cfg = $derived(endpoint.config ?? {});

	// ── Templates ("Start from") ──
	const TRANSFORM_TEMPLATES: { label: string; code: string }[] = [
		{
			label: 'Add a field',
			code: `// Runs for each request before it's forwarded.
// req.json is the parsed body when it's JSON.
function transform(req) {
  req.json.received_at = new Date().toISOString();
  return req;
}

// Return null to skip a request (it's still captured).`
		},
		{
			label: 'Remove a field',
			code: `function transform(req) {
  // Drop fields the receiver shouldn't see.
  delete req.json.internal_id;
  return req;
}`
		},
		{
			label: 'Rename keys',
			code: `function transform(req) {
  // Rename "type" to "event" before forwarding.
  req.json.event = req.json.type;
  delete req.json.type;
  return req;
}`
		},
		{
			label: 'Skip some events',
			code: `function transform(req) {
  // Returning null captures the request but doesn't forward it.
  if (req.json?.type === "ping") return null;
  return req;
}`
		},
		{
			label: 'Add a header',
			code: `function transform(req) {
  req.setHeader("X-Forwarded-By", "testhooks");
  return req;
}`
		},
		{
			label: 'Wrap in an envelope',
			code: `function transform(req) {
  req.json = {
    event: req.json,
    received_at: new Date().toISOString(),
    source: req.header("user-agent") || "unknown"
  };
  return req;
}`
		}
	];
	const REPLY_TEMPLATES: { label: string; code: string }[] = [
		{
			label: 'Always 200',
			code: `// Decides what the sender gets back.
function handler(req) {
  return { status: 200, body: JSON.stringify({ ok: true }) };
}`
		},
		{
			label: 'Echo the body',
			code: `function handler(req) {
  return {
    status: 200,
    headers: { "Content-Type": req.content_type || "text/plain" },
    body: req.body
  };
}`
		},
		{
			label: 'Fail with 500',
			code: `// Test how the sender handles errors and retries.
function handler(req) {
  return { status: 500, body: "simulated failure" };
}`
		},
		{
			label: 'Fail sometimes',
			code: `function handler(req) {
  if (Math.random() < 0.3) return { status: 503, body: "try again" };
  return { status: 200, body: "ok" };
}`
		},
		{
			label: "Pass the forward target's reply",
			code: `// Needs sync forwarding (Settings → Forwarding).
function handler(req) {
  const upstream = req.forward_response;
  if (!upstream) return { status: 202, body: "accepted" };
  return { status: upstream.status, body: upstream.body, content_type: upstream.content_type };
}`
		}
	];
	const templates = $derived(isTransform ? TRANSFORM_TEMPLATES : REPLY_TEMPLATES);

	// ── Script state ──
	const savedScript = $derived(isTransform ? (cfg.wasm_script ?? '') : (cfg.custom_response?.script ?? ''));
	const savedLang = $derived(isTransform ? cfg.transform_language : cfg.custom_response?.language);
	const savedEnabled = $derived(isTransform ? !!cfg.wasm_script && !cfg.transform_disabled : !!cfg.custom_response?.enabled);
	// svelte-ignore state_referenced_locally
	let legacy = $state(isLegacyLanguage(savedLang) ? savedLang : null);
	// svelte-ignore state_referenced_locally
	let script = $state(legacy ? '' : savedScript || templates[0].code);
	// svelte-ignore state_referenced_locally
	let enabled = $state(savedEnabled || !savedScript);
	let saving = $state(false);
	let saveError = $state<string | null>(null);
	const dirty = $derived(!legacy && (script !== savedScript || enabled !== savedEnabled));

	function useTemplate(code: string) {
		if (dirty && script !== savedScript && !templates.some((t) => t.code === script)) {
			if (!confirm('Replace your script with this example?')) return;
		}
		legacy = null;
		script = code;
	}

	async function save() {
		saving = true;
		saveError = null;
		try {
			await onSave(script, enabled);
			onClose();
		} catch (err) {
			saveError = err instanceof Error ? err.message : 'Save failed';
		} finally {
			saving = false;
		}
	}

	function cancel() {
		if (dirty && !confirm('Discard your changes?')) return;
		onClose();
	}

	// ── Input ("Try it on") ──
	const storageKey = $derived(`testhooks:sample:${endpoint.slug}`);
	function loadDraft(): SampleDraft {
		try {
			const saved = localStorage.getItem(storageKey);
			if (saved) return JSON.parse(saved) as SampleDraft;
		} catch {
			// Storage unavailable or corrupt.
		}
		return { ...SAMPLE_PRESETS[0].draft };
	}
	let draft = $state<SampleDraft>(loadDraft());
	$effect(() => {
		const json = JSON.stringify(draft);
		try {
			localStorage.setItem(storageKey, json);
		} catch {
			// Not essential.
		}
	});

	const recent = $derived(captured.slice(0, 50));
	// svelte-ignore state_referenced_locally
	let chosen = $state<string | null>(initialRequestId ? `cap:${initialRequestId}` : null);
	const choice = $derived(chosen ?? (recent[0] ? `cap:${recent[0].id}` : `sample:${SAMPLE_PRESETS[0].id}`));
	let editingInput = $state(false);

	const input = $derived.by<CapturedRequest | null>(() => {
		if (choice === 'custom') return draftToRequest(draft);
		if (choice.startsWith('sample:')) {
			const p = SAMPLE_PRESETS.find((x) => `sample:${x.id}` === choice) ?? SAMPLE_PRESETS[0];
			return draftToRequest(p.draft);
		}
		return captured.find((r) => `cap:${r.id}` === choice) ?? null;
	});

	function editInput() {
		if (input) draft = requestToDraft(input);
		chosen = 'custom';
		editingInput = true;
	}

	function optionLabel(r: CapturedRequest): string {
		const ev = eventLabel(r);
		return `${r.method} ${r.path} · ${ev === 'no body' || ev === 'JSON body' ? ev : ev} · ${ago(r.created_at)}`;
	}

	// ── Running ──
	// A reply runs on the request after the saved transform, like the server.
	const savedTransform = $derived(
		!isTransform && cfg.wasm_script && !cfg.transform_disabled && !isLegacyLanguage(cfg.transform_language) ? cfg.wasm_script : ''
	);
	const sampleUpstream: ForwardResponse = { status: 200, headers: { 'content-type': 'application/json' }, body: '{"ok":true}', content_type: 'application/json' };
	const useUpstream = $derived(!!cfg.forward_url && cfg.forward_mode === 'sync');

	type Result =
		| { kind: 'error'; error: string }
		| { kind: 'skipped'; ms: number }
		| { kind: 'forwarded'; lines: Line[]; added: number; removed: number; other: string[]; ms: number }
		| { kind: 'reply'; status: number; headers: Record<string, string>; body: string; ms: number; skippedByTransform: boolean };

	async function runOn(req: CapturedRequest, code: string): Promise<Result> {
		await initWasm();
		if (isTransform) {
			const r = await runTransform(code, req);
			if (!r.ok) return { kind: 'error', error: r.error ?? 'Failed' };
			if (r.dropped) return { kind: 'skipped', ms: r.duration };
			const fwd = { ...req, ...r.data } as CapturedRequest;
			const before = pretty(bodyText(req));
			const after = pretty(fwd.body ?? '');
			const lines = diffLines(before, after);
			const counted = fieldChanges(bodyText(req), fwd.body ?? '');
			return {
				kind: 'forwarded',
				lines,
				added: counted?.added ?? lines.filter((l) => l.kind === '+').length,
				removed: counted?.removed ?? lines.filter((l) => l.kind === '-').length,
				other: otherChanges(req, fwd),
				ms: r.duration
			};
		}
		let handlerReq = req;
		if (savedTransform) {
			const t = await runTransform(savedTransform, req);
			if (t.ok && t.dropped) return { kind: 'reply', status: endpoint.mode === 'browser' ? 202 : 200, headers: {}, body: '{"status":"ok","id":"…"}', ms: t.duration, skippedByTransform: true };
			if (t.ok) handlerReq = { ...req, ...t.data } as CapturedRequest;
		}
		const r = await runResponseHandler(code, handlerReq, 'javascript', useUpstream ? sampleUpstream : null);
		if (!r.ok) return { kind: 'error', error: r.error ?? 'Failed' };
		return { kind: 'reply', status: r.response.status || 200, headers: r.response.headers ?? {}, body: r.response.body ?? '', ms: r.duration, skippedByTransform: false };
	}

	let result = $state<Result | null>(null);
	$effect(() => {
		const req = input;
		const code = script;
		if (!req || legacy) {
			result = null;
			return;
		}
		let stale = false;
		const t = setTimeout(async () => {
			const r = await runOn(req, code);
			if (!stale) result = r;
		}, 250);
		return () => {
			stale = true;
			clearTimeout(t);
		};
	});

	// ── "Check it against more traffic" ──
	type Check = { total: number; pass: number; skipped: number; failures: { req: CapturedRequest; error: string }[] };
	let check = $state<Check | null>(null);
	$effect(() => {
		const reqs = recent;
		const code = script;
		if (!reqs.length || legacy) {
			check = null;
			return;
		}
		let stale = false;
		const t = setTimeout(async () => {
			const c: Check = { total: reqs.length, pass: 0, skipped: 0, failures: [] };
			for (const r of reqs) {
				const out = await runOn(r, code);
				if (stale) return;
				if (out.kind === 'error') c.failures.push({ req: r, error: out.error });
				else if (out.kind === 'skipped') c.skipped++;
				else c.pass++;
			}
			check = c;
		}, 600);
		return () => {
			stale = true;
			clearTimeout(t);
		};
	});

	function showFailure() {
		const f = check?.failures[0];
		if (f) chosen = `cap:${f.req.id}`;
	}

	const inputJSON = $derived(input ? parseJSON(bodyText(input)) : undefined);
	const contentType = $derived(input ? (Object.entries(input.headers ?? {}).find(([k]) => k.toLowerCase() === 'content-type')?.[1][0] ?? '') : '');
	const methods = ['POST', 'PUT', 'PATCH', 'GET', 'DELETE'];
</script>

<div class="min-h-screen bg-page text-ink flex flex-col">
	<header class="border-b border-line bg-page">
		<div class="px-6 min-h-[60px] flex items-center justify-between gap-4 flex-wrap">
			<nav aria-label="Breadcrumb" class="flex items-center gap-2.5 text-sm flex-wrap">
				<a href="/" class="flex items-center gap-2.5 no-underline text-ink">
					<span class="w-[26px] h-[26px] rounded-[7px] bg-[#111613] dark:bg-[#1B231F] flex items-center justify-center">
						<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="#5BD08F" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 12h5l2-5 3 10 2-5h4"></path></svg>
					</span>
					<span class="font-semibold text-base tracking-[-0.01em]">testhooks</span>
				</a>
				<span class="text-[#9AA39E]" aria-hidden="true">/</span>
				<button type="button" onclick={cancel} class="font-medium text-ink hover:text-accent cursor-pointer">{endpoint.name || endpoint.slug}</button>
				<span class="text-[#9AA39E]" aria-hidden="true">/</span>
				<span class="font-medium">{isTransform ? 'Transform' : 'Reply'}</span>
			</nav>
			<div class="flex items-center gap-2.5 flex-wrap">
				<label class="flex items-center gap-2.5 text-sm font-medium cursor-pointer">
					<input type="checkbox" bind:checked={enabled} disabled={!!legacy} class="w-[18px] h-[18px] accent-[#1A7F4B] m-0" />
					Run on every request
				</label>
				<button type="button" onclick={cancel} class="text-sm font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-4 min-h-10 flex items-center cursor-pointer hover:bg-chip">
					Cancel
				</button>
				<button type="button" onclick={save} disabled={saving || !!legacy} class="text-sm font-medium text-on-ink bg-ink rounded-[9px] px-[18px] min-h-10 cursor-pointer hover:opacity-90 disabled:opacity-50">
					{saving ? 'Saving…' : isTransform ? 'Save transform' : 'Save reply'}
				</button>
			</div>
		</div>
	</header>

	<main class="flex-1 p-6 flex flex-col gap-[18px] box-border">
		<div class="flex justify-between items-end gap-4 flex-wrap">
			<div class="flex flex-col gap-1.5 max-w-[720px]">
				<h1 class="m-0 text-[26px] font-semibold tracking-[-0.02em]">
					{isTransform ? "Change the request before it's forwarded" : 'Choose what the sender gets back'}
				</h1>
				<p class="m-0 text-[15px] leading-[1.55] text-body">
					{#if isTransform}
						Write a JavaScript function that gets the request and returns the one to forward. Try it on real requests below; nothing is saved until you click Save.
					{:else}
						Write a JavaScript function that gets the request and returns the status, headers and body to reply with. Try it on real requests below; nothing is saved until you click Save.
					{/if}
				</p>
			</div>
			<div class="flex flex-col gap-2">
				<span class="text-xs font-medium text-soft font-mono tracking-[0.04em] uppercase">Start from</span>
				<div class="flex gap-1.5 flex-wrap">
					{#each templates as t (t.label)}
						<button
							type="button"
							onclick={() => useTemplate(t.code)}
							class="text-[13px] font-medium rounded-full px-3 min-h-[34px] cursor-pointer {script === t.code ? 'text-ink bg-chip border border-ink' : 'text-soft bg-surface border border-line-strong hover:text-ink'}"
						>
							{t.label}
						</button>
					{/each}
				</div>
			</div>
		</div>

		{#if saveError}
			<p class="m-0 text-sm text-danger">{saveError}</p>
		{/if}

		{#if legacy}
			<div class="bg-surface border border-line rounded-[14px] p-4 flex flex-col gap-3">
				<p class="m-0 text-sm">
					This {isTransform ? 'transform' : 'reply'} is written in {legacy === 'lua' ? 'Lua' : legacy === 'jsonnet' ? 'Jsonnet' : legacy}. testhooks now runs JavaScript only, so it's skipped until you rewrite it. Pick a starting point above, or start from the first one:
				</p>
				<pre class="m-0 font-mono text-[12.5px] text-soft bg-page border border-line rounded-lg p-3 overflow-x-auto">{savedScript}</pre>
				<button type="button" onclick={() => useTemplate(templates[0].code)} class="self-start text-sm font-medium text-on-ink bg-ink rounded-[9px] px-4 min-h-10 cursor-pointer">
					Start a JavaScript version
				</button>
			</div>
		{:else}
			<div class="grid grid-cols-[repeat(auto-fit,minmax(340px,1fr))] gap-3.5 items-stretch">
				<!-- 1 · Input -->
				<section aria-label="Sample request" class="bg-surface border border-line rounded-[14px] flex flex-col overflow-hidden">
					<div class="px-4 py-3.5 border-b border-line flex flex-col gap-2">
						<div class="flex justify-between items-center gap-2">
							<span class="text-[13px] font-semibold">1 · Input</span>
							{#if input}
								<button type="button" onclick={editInput} class="text-xs font-medium text-accent hover:underline cursor-pointer">Edit</button>
							{/if}
						</div>
						<label class="flex flex-col gap-1 text-xs text-faint">
							Try it on
							<select
								value={choice}
								onchange={(e) => {
									chosen = e.currentTarget.value;
									editingInput = chosen === 'custom';
								}}
								class="font-mono text-[13px] text-ink border border-line-strong rounded-lg px-2.5 min-h-[38px] bg-page"
							>
								{#if recent.length}
									<optgroup label="Captured requests">
										{#each recent as r (r.id)}
											<option value={`cap:${r.id}`}>{optionLabel(r)}</option>
										{/each}
									</optgroup>
								{/if}
								<optgroup label="Samples">
									{#each SAMPLE_PRESETS as p (p.id)}
										<option value={`sample:${p.id}`}>Sample · {p.label}</option>
									{/each}
								</optgroup>
								<option value="custom">Your own request…</option>
							</select>
						</label>
					</div>

					{#if choice === 'custom' && editingInput}
						<div class="flex-1 p-4 flex flex-col gap-2.5">
							<div class="flex gap-2">
								<label class="sr-only" for="ed-method">Method</label>
								<select id="ed-method" bind:value={draft.method} class="font-mono text-[13px] border border-line-strong rounded-lg px-2 min-h-[36px] bg-page">
									{#each methods as m (m)}<option value={m}>{m}</option>{/each}
								</select>
								<label class="sr-only" for="ed-path">Path and query</label>
								<input id="ed-path" bind:value={draft.path} placeholder="/events" class="flex-1 min-w-0 font-mono text-[13px] border border-line-strong rounded-lg px-2.5 min-h-[36px] bg-page" />
							</div>
							<label class="flex flex-col gap-1 text-xs text-faint">
								<span>Headers, one per line as <code class="font-mono">Name: value</code></span>
								<textarea bind:value={draft.headers} rows="3" spellcheck="false" class="font-mono text-[12.5px] text-ink border border-line-strong rounded-lg px-2.5 py-2 bg-page resize-y"></textarea>
							</label>
							<label class="flex flex-col gap-1 text-xs text-faint flex-1">
								<span>Body</span>
								<textarea bind:value={draft.body} rows="10" spellcheck="false" placeholder={'{ "type": "invoice.paid" }'} class="flex-1 font-mono text-[12.5px] text-ink border border-line-strong rounded-lg px-2.5 py-2 bg-page resize-y"></textarea>
							</label>
							<button type="button" onclick={() => (editingInput = false)} class="self-start text-xs font-medium text-accent hover:underline cursor-pointer">Done</button>
						</div>
					{:else if input}
						<pre class="m-0 flex-1 p-4 font-mono text-[12.5px] leading-[1.75] text-soft overflow-x-auto"><span class="text-faint">req.method</span>  {JSON.stringify(input.method)}
<span class="text-faint">req.path</span>    {JSON.stringify(input.path)}{#if queryString(input)}
<span class="text-faint">req.query</span>   {JSON.stringify(input.query)}{/if}{#if contentType}
<span class="text-faint">req.header("content-type")</span>  {JSON.stringify(contentType)}{/if}
{#if inputJSON !== undefined}<span class="text-faint">req.json</span>    {JSON.stringify(inputJSON, null, 2)}{:else if bodyText(input)}<span class="text-faint">req.body</span>    {JSON.stringify(bodyText(input))}
<span class="text-faint">req.json</span>    null{:else}<span class="text-faint">req.body</span>    ""{/if}</pre>
					{:else}
						<p class="m-0 p-4 text-sm text-body">Pick something to try it on.</p>
					{/if}
				</section>

				<!-- 2 · Script -->
				<section aria-label="Script" class="bg-code border border-code rounded-[14px] flex flex-col overflow-hidden min-h-[360px]">
					<div class="px-4 py-3.5 border-b border-code-line flex justify-between items-center gap-2">
						<span class="text-[13px] font-semibold text-code-ink">2 · {isTransform ? 'Your transform' : 'Your reply'}</span>
						<span class="font-mono text-xs text-[#9AA39E]">JavaScript</span>
					</div>
					<div class="flex-1 min-h-0 py-2">
						<CodeEditor value={script} onchange={(v) => (script = v)} language="javascript" minHeight="300px" bare />
					</div>
					<div class="px-4 py-3 border-t border-code-line flex flex-wrap gap-x-3.5 gap-y-1.5 font-mono text-xs text-[#9AA39E]">
						<span>req.method</span><span>req.path</span><span>req.json</span><span>req.body</span><span>req.header("name")</span>
						{#if isTransform}
							<span>req.setHeader("name", value)</span><span>req.query.name</span><span>return null to skip</span>
						{:else}
							<span>req.forward_response</span><span>{'return { status, headers, body }'}</span>
						{/if}
					</div>
				</section>

				<!-- 3 · Result -->
				<section aria-label="Result" class="bg-surface border border-line rounded-[14px] flex flex-col overflow-hidden">
					<div class="px-4 py-3.5 border-b border-line flex justify-between items-center gap-2 flex-wrap">
						<span class="text-[13px] font-semibold">3 · {isTransform ? 'What will be forwarded' : 'What the sender gets'}</span>
						{#if result?.kind === 'error'}
							<span class="text-xs text-danger">Error</span>
						{:else if result}
							<span class="flex items-center gap-1.5 text-xs text-accent">
								<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12l5 5L20 7"></path></svg>
								Ran in {result.ms} ms
							</span>
						{/if}
					</div>

					{#if !result}
						<p class="m-0 flex-1 p-4 text-sm text-body">{input ? 'Running…' : 'Pick something to try it on.'}</p>
					{:else if result.kind === 'error'}
						<pre class="m-0 flex-1 p-4 font-mono text-[12.5px] leading-[1.75] text-danger whitespace-pre-wrap">{result.error}</pre>
						<div class="px-4 py-3 border-t border-line text-[13px] text-body">
							{isTransform ? 'On a real request the original would be forwarded unchanged.' : 'On a real request the default reply would be sent.'}
						</div>
					{:else if result.kind === 'skipped'}
						<p class="m-0 flex-1 p-4 text-sm text-body">Returned <code class="font-mono">null</code>: this request is captured but not forwarded.</p>
						<div class="px-4 py-3 border-t border-line text-[13px] text-body">The sender gets the default reply.</div>
					{:else if result.kind === 'forwarded'}
						<pre class="m-0 flex-1 py-4 font-mono text-[12.5px] leading-[1.75] text-soft overflow-x-auto">{#each result.lines as l, i (i)}<span class="block px-4 {l.kind === '+' ? 'bg-[#E6F4EC] text-[#0F5132] dark:bg-[#12301f] dark:text-[#7ee2a8]' : l.kind === '-' ? 'bg-[#FDECEA] text-[#8A1C12] dark:bg-[#3a1512] dark:text-[#f9a8a0]' : ''}">{#if l.kind !== ' '}<span aria-hidden="true">{l.kind === '+' ? '+ ' : '− '}</span>{/if}{l.text}</span>{:else}<span class="block px-4 text-body">(empty body)</span>{/each}</pre>
						<div class="px-4 py-3 border-t border-line flex gap-3.5 flex-wrap text-[13px] text-body">
							<span><b class="text-[#0F5132] dark:text-[#7ee2a8] font-semibold">+{result.added}</b> added</span>
							<span><b class="text-[#8A1C12] dark:text-[#f9a8a0] font-semibold">−{result.removed}</b> removed</span>
							<span>{result.other.length ? result.other.join(', ') : 'Method, path and headers unchanged'}</span>
						</div>
					{:else}
						<div class="flex-1 p-4 font-mono text-[12.5px] leading-[1.75] text-soft overflow-x-auto">
							<div class="text-ink text-[15px] font-medium {result.status >= 400 ? 'text-danger' : ''}">{statusLine(result.status)}</div>
							{#each Object.entries(result.headers) as [k, v] (k)}<div class="text-faint">{k}: {v}</div>{/each}
							{#if result.body}<pre class="m-0 mt-2 whitespace-pre-wrap break-all">{pretty(result.body)}</pre>{:else}<div class="mt-2 text-body font-sans">Empty body</div>{/if}
						</div>
						<div class="px-4 py-3 border-t border-line text-[13px] text-body">
							{#if result.skippedByTransform}
								The transform skips this request, so the sender gets the default reply.
							{:else if useUpstream}
								<code class="font-mono">req.forward_response</code> is a stand-in here; the forward target isn't called.
							{:else}
								Runs after the transform{cfg.forward_url ? ' and the forward' : ''}.
							{/if}
						</div>
					{/if}
				</section>
			</div>

			<div class="flex justify-between items-center gap-3 flex-wrap bg-surface border border-line rounded-[12px] px-4 py-3">
				{#if !recent.length}
					<span class="text-sm text-body">Check it against more traffic: send a few requests to this endpoint and they're tested here automatically.</span>
				{:else if !check}
					<span class="text-sm text-body">Checking it against {recent.length} recent request{recent.length === 1 ? '' : 's'}…</span>
				{:else}
					<span class="text-sm text-body">
						Check it against more traffic: <b class="text-ink font-semibold">{check.total} recent request{check.total === 1 ? '' : 's'}</b>,
						{check.pass} pass{#if check.skipped}, {check.skipped} skipped{/if}{#if check.failures.length}, <b class="text-danger font-semibold">{check.failures.length} throw{check.failures.length === 1 ? 's' : ''}</b>
							({check.failures[0].req.method} {check.failures[0].req.path}{bodyText(check.failures[0].req) ? '' : ' has no body'}){/if}.
					</span>
					{#if check.failures.length}
						<button type="button" onclick={showFailure} class="text-[13px] font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-3.5 min-h-10 cursor-pointer hover:bg-chip">
							Show the failing request
						</button>
					{/if}
				{/if}
			</div>
		{/if}
	</main>
</div>
