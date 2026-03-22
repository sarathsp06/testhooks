<script lang="ts">
	import CodeEditor from './CodeEditor.svelte';
	import {
		initWasm,
		isWasmReady,
		runTransform,
		validateScript,
		SUPPORTED_LANGUAGES,
		DEFAULT_SCRIPTS,
		formatRequestReference,
		type TransformLanguage
	} from '$lib/wasm';
	import type { CapturedRequest, TransformResult } from '$lib/types';
	import { Play, Check, AlertTriangle, Loader2, BookOpen, ChevronDown, ChevronRight, FileCode } from 'lucide-svelte';

	let {
		script = '',
		language = 'javascript' as TransformLanguage,
		onScriptChange,
		onLanguageChange,
		testRequest
	}: {
		script?: string;
		language?: TransformLanguage;
		onScriptChange: (script: string) => void;
		onLanguageChange: (language: TransformLanguage) => void;
		testRequest?: CapturedRequest | null;
	} = $props();

	let wasmReady = $state(false);
	let wasmLoading = $state(false);
	let wasmError = $state<string | null>(null);
	let validating = $state(false);
	let validationResult = $state<{ valid: boolean; error?: string } | null>(null);
	let testResult = $state<TransformResult | null>(null);
	let testing = $state(false);
	let showReference = $state(false);

	async function loadWasm() {
		wasmLoading = true;
		wasmError = null;
		try {
			await initWasm(language);
			wasmReady = isWasmReady(language);
		} catch (err) {
			wasmReady = false;
			wasmError = err instanceof Error ? err.message : 'Failed to load WASM engine';
		} finally {
			wasmLoading = false;
		}
	}

	async function handleValidate() {
		if (!isWasmReady(language)) await loadWasm();
		if (!isWasmReady(language)) return;
		validating = true;
		validationResult = null;
		try {
			validationResult = await validateScript(script || DEFAULT_SCRIPTS[language], language);
		} finally {
			validating = false;
		}
	}

	async function handleTest() {
		if (!testRequest) return;
		if (!isWasmReady(language)) await loadWasm();
		if (!isWasmReady(language)) return;
		testing = true;
		testResult = null;
		try {
			testResult = await runTransform(script || DEFAULT_SCRIPTS[language], testRequest, language);
		} finally {
			testing = false;
		}
	}

	function handleInsertDefault() {
		onScriptChange(DEFAULT_SCRIPTS[language]);
	}

	function handleLanguageSwitch(lang: TransformLanguage) {
		if (lang === language) return;
		validationResult = null;
		testResult = null;
		wasmError = null;
		onLanguageChange(lang);
		initWasm(lang).catch(() => {});
	}
</script>

<div class="flex flex-col gap-3">
	<!-- Top row: language picker + actions -->
	<div class="flex items-center justify-between gap-2 flex-wrap">
		<div class="flex items-center gap-2">
			<span class="text-xs text-[var(--text-muted)] font-medium">Language</span>
			<div class="flex items-center border border-[var(--border)] rounded overflow-hidden">
				{#each SUPPORTED_LANGUAGES as lang}
					<button
						onclick={() => handleLanguageSwitch(lang.id)}
						class="text-[11px] px-2.5 py-1 transition-colors {language === lang.id
							? 'bg-[var(--accent)] text-white'
							: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
					>
						{lang.label}
					</button>
				{/each}
			</div>
		</div>
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
			<button
				onclick={handleTest}
				disabled={testing || !testRequest || !script}
				class="text-[11px] px-2.5 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1 disabled:opacity-50"
				title={!testRequest ? 'Select a request to test against' : 'Run transform on selected request'}
			>
				{#if testing}
					<Loader2 class="w-3 h-3 animate-spin" />
				{:else}
					<Play class="w-3 h-3" />
				{/if}
				Test
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
					<span>Request Object Shape ({SUPPORTED_LANGUAGES.find(l => l.id === language)?.label})</span>
				{/if}
			</div>
			<pre class="px-3 py-2 text-[11px] font-mono text-[var(--text-muted)] overflow-x-auto max-h-48 leading-relaxed whitespace-pre">{formatRequestReference(testRequest, language)}</pre>
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
		placeholder={DEFAULT_SCRIPTS[language].split('\n')[0]}
		minHeight="180px"
		{language}
	/>

	<!-- WASM status -->
	{#if wasmLoading}
		<div class="flex items-center gap-1.5 text-xs text-[var(--text-muted)]">
			<Loader2 class="w-3 h-3 animate-spin" />
			Loading {SUPPORTED_LANGUAGES.find(l => l.id === language)?.label ?? language} WASM engine...
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

	<!-- Test result -->
	{#if testResult}
		<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded">
			<div class="px-3 py-2 text-xs font-medium text-[var(--text-muted)] border-b border-[var(--border)] flex items-center justify-between">
				<span>Transform Result</span>
				<span class="{testResult.ok ? 'text-[var(--green)]' : 'text-[var(--red)]'}">{testResult.duration}ms</span>
			</div>
			{#if testResult.ok}
				<pre class="px-3 py-2 text-xs font-mono text-[var(--text)] overflow-x-auto max-h-48 whitespace-pre-wrap break-all">{JSON.stringify(testResult.data, null, 2)}</pre>
			{:else}
				<div class="px-3 py-2 text-xs text-[var(--red)]">{testResult.error}</div>
			{/if}
		</div>
	{/if}
</div>
