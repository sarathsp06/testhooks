<script lang="ts">
	import CodeEditor from './CodeEditor.svelte';
	import {
		initWasm,
		isWasmReady,
		validateScript,
		DEFAULT_SCRIPTS,
		formatRequestReference
	} from '$lib/wasm';
	import type { CapturedRequest } from '$lib/types';
	import { Check, AlertTriangle, Loader2, BookOpen, ChevronDown, ChevronRight, FileCode } from 'lucide-svelte';

	let {
		script = '',
		onScriptChange,
		testRequest
	}: {
		script?: string;
		onScriptChange: (script: string) => void;
		testRequest?: CapturedRequest | null;
	} = $props();

	let wasmReady = $state(false);
	let wasmLoading = $state(false);
	let wasmError = $state<string | null>(null);
	let validating = $state(false);
	let validationResult = $state<{ valid: boolean; error?: string } | null>(null);
	let showReference = $state(false);

	async function loadWasm() {
		wasmLoading = true;
		wasmError = null;
		try {
			await initWasm();
			wasmReady = isWasmReady();
		} catch (err) {
			wasmReady = false;
			wasmError = err instanceof Error ? err.message : 'Failed to load WASM engine';
		} finally {
			wasmLoading = false;
		}
	}

	async function handleValidate() {
		if (!isWasmReady()) await loadWasm();
		if (!isWasmReady()) return;
		validating = true;
		validationResult = null;
		try {
			validationResult = await validateScript(script || DEFAULT_SCRIPTS.javascript);
		} finally {
			validating = false;
		}
	}


	function handleInsertDefault() {
		onScriptChange(DEFAULT_SCRIPTS.javascript);
	}
</script>

<div class="flex flex-col gap-3">
	<!-- Top row: actions -->
	<div class="flex items-center justify-between gap-2 flex-wrap">
		<span class="text-xs text-[var(--text-muted)] font-medium">JavaScript</span>
		<div class="flex items-center gap-1.5">
			{#if !script}
				<button
					onclick={handleInsertDefault}
					class="text-[11px] px-2.5 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1"
				>
					<FileCode class="w-3 h-3" />
					Insert template
				</button>
			{/if}
			<button
				onclick={() => (showReference = !showReference)}
				class="text-[11px] px-2.5 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] transition-colors flex items-center gap-1 {showReference ? 'text-[var(--accent)] border-[var(--accent)]/40' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
				title="Show request object reference"
			>
				<BookOpen class="w-3 h-3" />
				Reference
			</button>
			<button
				onclick={handleValidate}
				disabled={validating || !script}
				class="text-[11px] px-2.5 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1 disabled:opacity-50"
			>
				{#if validating}
					<Loader2 class="w-3 h-3 animate-spin" />
				{:else}
					<Check class="w-3 h-3" />
				{/if}
				Validate
			</button>
		</div>
	</div>

	<!-- Request object reference (collapsible) -->
	{#if showReference}
		<div class="bg-[var(--bg)] border border-[var(--border)] rounded">
			<div class="px-3 py-2 text-xs font-medium text-[var(--text-muted)] border-b border-[var(--border)] flex items-center gap-1.5">
				<BookOpen class="w-3 h-3" />
				{#if testRequest}
					<span>Request Object — <span class="text-[var(--accent)]">{testRequest.method} {testRequest.path}</span></span>
				{:else}
					<span>What your script gets</span>
				{/if}
			</div>
			<pre class="px-3 py-2 text-[11px] font-mono text-[var(--text-muted)] overflow-x-auto max-h-48 leading-relaxed whitespace-pre">{formatRequestReference(testRequest)}</pre>
			{#if !testRequest}
				<div class="px-3 py-1.5 text-[10px] text-[var(--text-muted)] italic border-t border-[var(--border)]">
					Select a request to see its actual data here.
				</div>
			{/if}
		</div>
	{/if}

	<!-- Code editor -->
	<CodeEditor
		value={script}
		onchange={onScriptChange}
		placeholder={DEFAULT_SCRIPTS.javascript.split('\n')[0]}
		minHeight="180px"
		language="javascript"
	/>

	<!-- WASM status -->
	{#if wasmLoading}
		<div class="flex items-center gap-1.5 text-xs text-[var(--text-muted)]">
			<Loader2 class="w-3 h-3 animate-spin" />
			Loading the JavaScript engine...
		</div>
	{:else if wasmError}
		<div class="flex items-center gap-2 text-xs px-3 py-2 rounded border border-red-500/30 bg-red-500/5 text-[var(--red)]">
			<AlertTriangle class="w-3 h-3 flex-shrink-0" />
			<span class="flex-1">{wasmError}</span>
			<button
				onclick={loadWasm}
				class="text-[11px] underline hover:no-underline flex-shrink-0"
			>
				Retry
			</button>
		</div>
	{/if}

	<!-- Validation result -->
	{#if validationResult}
		<div class="text-xs px-3 py-2 rounded border {validationResult.valid ? 'border-green-500/30 bg-green-500/5 text-[var(--green)]' : 'border-red-500/30 bg-red-500/5 text-[var(--red)]'}">
			{#if validationResult.valid}
				<span class="flex items-center gap-1"><Check class="w-3 h-3" /> Script is valid</span>
			{:else}
				<span class="flex items-center gap-1"><AlertTriangle class="w-3 h-3" /> {validationResult.error}</span>
			{/if}
		</div>
	{/if}
</div>
