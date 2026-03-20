<script lang="ts">
	import type { Endpoint } from '$lib/types';
	import { SUPPORTED_LANGUAGES, DEFAULT_RESPONSE_SCRIPTS, RESPONSE_HANDLER_EXAMPLES, formatRequestReference, type TransformLanguage } from '$lib/wasm';
	import type { CapturedRequest } from '$lib/types';
	import CodeEditor from './CodeEditor.svelte';
	import { Server, Monitor, Globe, Reply, Info, AlertCircle, Check, MessageSquare, Save, BookOpen } from 'lucide-svelte';
	import Tooltip from './Tooltip.svelte';

	let {
		endpoint,
		onUpdate,
		testRequest
	}: {
		endpoint: Endpoint;
		onUpdate: (patch: { name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> }) => void;
		testRequest?: CapturedRequest | null;
	} = $props();

	let editName = $state('');
	let urlError = $state('');
	let saved = $state(false);

	// Forward URL and mode from endpoint config
	let forwardUrl = $state('');
	let forwardMode = $state<'sync' | 'async'>('async');

	// Derived accessors for custom response config from prop
	const customResponse = $derived(endpoint.config?.custom_response as Record<string, unknown> | undefined);

	// Local mutable state for custom response script — synced from prop
	let customResponseEnabled = $state<boolean>(false);
	let customResponseScript = $state<string>('');
	let customResponseLanguage = $state<TransformLanguage>('javascript');

	// Sync local state from endpoint prop when it changes
	$effect(() => {
		editName = endpoint.name;
	});

	$effect(() => {
		forwardUrl = (endpoint.config?.forward_url as string) ?? '';
		forwardMode = ((endpoint.config?.forward_mode as string) || 'async') as 'sync' | 'async';
	});

	$effect(() => {
		const cr = customResponse;
		customResponseEnabled = !!cr?.enabled;
		customResponseScript = (cr?.script as string) ?? '';
		customResponseLanguage = ((cr?.language as string) || 'javascript') as TransformLanguage;
	});

	const isServerMode = $derived(endpoint.mode === 'server');

	// Track whether settings have unsaved changes
	let dirty = $state(false);

	function markDirty() {
		dirty = true;
		saved = false;
	}

	function validateUrl(url: string): boolean {
		if (!url.trim()) {
			urlError = '';
			return true; // empty is valid (clears the forward URL)
		}
		try {
			const parsed = new URL(url);
			if (isServerMode && (parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1')) {
				urlError = 'localhost targets only work in browser mode — the server cannot reach your machine.';
				return false;
			}
			urlError = '';
			return true;
		} catch {
			urlError = 'Please enter a valid URL (e.g. https://example.com/webhook)';
			return false;
		}
	}

	function handleNameBlur() {
		if (editName !== endpoint.name) {
			onUpdate({ name: editName });
		}
	}

	function handleModeChange(mode: 'server' | 'browser') {
		if (mode !== endpoint.mode) {
			onUpdate({ mode });
		}
	}

	function toggleCustomResponse() {
		customResponseEnabled = !customResponseEnabled;
		markDirty();
	}

	function handleResponseScriptChange(value: string) {
		customResponseScript = value;
		markDirty();
	}

	function handleResponseLanguageChange(lang: TransformLanguage) {
		customResponseLanguage = lang;
		customResponseScript = '';
		markDirty();
	}

	function saveSettings() {
		// Validate forward URL before saving
		if (!validateUrl(forwardUrl)) return;

		const customResponseConfig = customResponseEnabled
			? {
					enabled: true,
					script: customResponseScript,
					language: customResponseLanguage
				}
			: customResponseScript
				? { enabled: false, script: customResponseScript, language: customResponseLanguage }
				: undefined;

		onUpdate({
			config: {
				...endpoint.config,
				forward_url: forwardUrl.trim() || undefined,
				forward_mode: forwardUrl.trim() ? forwardMode : undefined,
				custom_response: customResponseConfig
			}
		});

		dirty = false;
		saved = true;
		setTimeout(() => (saved = false), 2000);
	}

	let showResponseExample = $state(false);
	let showResponseReqRef = $state(false);
</script>

<div class="flex flex-col gap-6 max-w-2xl">
	<!-- Section 1: General -->
	<section>
		<h4 class="text-xs font-semibold text-[var(--text)] uppercase tracking-wider mb-3">General</h4>
		<div class="flex items-start gap-4">
			<!-- Name -->
			<div class="flex-1">
				<label class="text-xs text-[var(--text-muted)] mb-1 flex items-center">
					Endpoint Name
					<Tooltip text="An optional label to help you identify this endpoint. Does not affect the webhook URL." />
				</label>
				<input
					bind:value={editName}
					onblur={handleNameBlur}
					placeholder="My Webhook"
					class="w-full bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-1.5 text-sm text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)]"
				/>
			</div>

			<!-- Mode toggle -->
			<div>
				<label class="text-xs text-[var(--text-muted)] mb-1 flex items-center">
					Mode
					<Tooltip text="Server mode stores requests in the database and runs transforms/forwarding on the server (always-on). Browser mode relays requests via WebSocket with no server storage -- all processing happens in your browser." />
				</label>
				<div class="flex gap-1 bg-[var(--bg)] border border-[var(--border)] rounded p-1">
					<button
						class="px-3 py-1.5 rounded text-xs font-medium transition-colors flex items-center gap-1.5 {endpoint.mode === 'server' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
						onclick={() => handleModeChange('server')}
					>
						<Server class="w-3 h-3" />
						Server
					</button>
					<button
						class="px-3 py-1.5 rounded text-xs font-medium transition-colors flex items-center gap-1.5 {endpoint.mode === 'browser' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
						onclick={() => handleModeChange('browser')}
					>
						<Monitor class="w-3 h-3" />
						Browser
					</button>
				</div>
			</div>
		</div>

		<!-- Mode description -->
		<div class="mt-2 flex items-start gap-2 text-[10px] text-[var(--text-muted)] bg-[var(--bg)] rounded px-3 py-2 border border-[var(--border)]">
			<Info class="w-3 h-3 flex-shrink-0 mt-0.5" />
			{#if isServerMode}
				<p><strong class="text-[var(--text)]">Server mode</strong> — Requests are stored in the database. Transforms and forwarding run on the server (always-on, headless). The browser is a viewer.</p>
			{:else}
				<p><strong class="text-[var(--text)]">Browser mode</strong> — Requests are relayed via WebSocket and never stored on the server. Transforms and forwarding run in your browser. Supports localhost targets. Data is lost if the tab is closed.</p>
			{/if}
		</div>
	</section>

	<!-- Divider -->
	<hr class="border-[var(--border)]" />

	<!-- Section 2: Forwarding -->
	<section>
		<div class="flex items-center gap-2 mb-1">
			{#if isServerMode}
				<Globe class="w-3.5 h-3.5 text-[var(--text-muted)]" />
			{:else}
				<Reply class="w-3.5 h-3.5 text-[var(--text-muted)]" />
			{/if}
			<h4 class="text-xs font-semibold text-[var(--text)] uppercase tracking-wider">Forwarding</h4>
			<Tooltip text="Forward incoming webhooks to a target URL. In server mode, forwarding uses the (optionally transformed) payload. Transform changes apply before forwarding." />
		</div>
		<p class="text-[10px] text-[var(--text-muted)] mb-3">
			{#if isServerMode}
				Requests are forwarded <strong>server-side</strong> via Go HTTP. Runs on every inbound request, even when the browser is closed. Only public URLs work (no localhost).
			{:else}
				Requests are forwarded <strong>from your browser</strong> via <code class="text-[10px]">fetch()</code>. Only works while this tab is open. Localhost targets work since the request originates from your machine.
			{/if}
		</p>

		<div class="flex flex-col gap-3">
			<!-- Forward URL input -->
			<div class="flex flex-col gap-1">
				<label class="text-xs text-[var(--text-muted)]">Target URL</label>
				<input
					bind:value={forwardUrl}
					placeholder={isServerMode ? 'https://example.com/webhook' : 'https://example.com/webhook or http://localhost:3000'}
					oninput={() => { urlError = ''; markDirty(); }}
					class="w-full bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-1.5 text-sm text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)] {urlError ? 'border-[var(--red)]' : ''}"
				/>
				{#if urlError}
					<p class="text-[10px] text-[var(--red)] flex items-center gap-1">
						<AlertCircle class="w-3 h-3" />
						{urlError}
					</p>
				{/if}
			</div>

			<!-- Forward Mode toggle -->
			{#if forwardUrl.trim()}
				<div class="flex items-center gap-3">
					<label class="text-xs text-[var(--text-muted)]">Mode</label>
					<div class="flex gap-1 bg-[var(--bg)] border border-[var(--border)] rounded p-1">
						<button
							class="px-3 py-1 rounded text-xs font-medium transition-colors {forwardMode === 'async' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
							onclick={() => { forwardMode = 'async'; markDirty(); }}
						>
							Async
						</button>
						<button
							class="px-3 py-1 rounded text-xs font-medium transition-colors {forwardMode === 'sync' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
							onclick={() => { forwardMode = 'sync'; markDirty(); }}
						>
							Sync
						</button>
					</div>
					<Tooltip text={forwardMode === 'async'
						? 'Async: fire-and-forget. The webhook sender gets an immediate response without waiting for the forward target.'
						: 'Sync: wait for the forward target to respond. If no custom response handler is set, the target\'s response is returned directly to the webhook sender. If a handler exists, it receives both the request and the forward response.'} />
				</div>

				{#if forwardMode === 'sync'}
					<div class="flex items-start gap-2 text-[10px] text-[var(--text-muted)] bg-[var(--bg)] rounded px-3 py-2 border border-[var(--border)]">
						<Info class="w-3 h-3 flex-shrink-0 mt-0.5" />
						<p>
							<strong class="text-[var(--text)]">Sync mode</strong> — The webhook response is blocked until the forward target replies.
							{#if endpoint.config?.custom_response?.enabled}
								Your custom response handler will receive the forward target's response as <code class="text-[10px]">req.forward_response</code>.
							{:else}
								The forward target's response will be returned directly to the webhook sender.
							{/if}
						</p>
					</div>
				{/if}
			{:else}
				<p class="text-[10px] text-[var(--text-muted)] italic">No forward URL configured. Enter a URL above to forward incoming requests.</p>
			{/if}
		</div>
	</section>

	<!-- Divider -->
	<hr class="border-[var(--border)]" />

	<!-- Section 3: Custom Response (Script-based) -->
	<section>
		<div class="flex items-center justify-between mb-1">
			<div class="flex items-center gap-2">
				<MessageSquare class="w-3.5 h-3.5 text-[var(--text-muted)]" />
				<h4 class="text-xs font-semibold text-[var(--text)] uppercase tracking-wider">Custom Response</h4>
				<Tooltip text="Control the HTTP response sent back to the webhook sender. Your handler function receives the request and returns a response object (status, headers, body). This is independent of Transform -- Transform modifies data for forwarding, Custom Response controls what the sender sees." />
			</div>
			<label class="relative inline-flex items-center cursor-pointer">
				<input
					type="checkbox"
					checked={customResponseEnabled}
					onchange={toggleCustomResponse}
					class="sr-only peer"
				/>
				<div class="w-8 h-4.5 bg-[var(--border)] peer-focus:outline-none rounded-full peer peer-checked:bg-[var(--accent)] transition-colors after:content-[''] after:absolute after:top-[2px] after:start-[2px] after:bg-white after:rounded-full after:h-3.5 after:w-3.5 after:transition-all peer-checked:after:translate-x-full"></div>
			</label>
		</div>
		<p class="text-[10px] text-[var(--text-muted)] mb-3">
			Write a script that receives the request and returns a custom response ({@html '<code class="text-[10px]">{ status, headers, body }</code>'}).
			{#if isServerMode}
				Runs server-side on every inbound request via Wazero (JavaScript only).
			{:else}
				Runs in-browser via WASM when this tab is open.
			{/if}
		</p>

		{#if customResponseEnabled}
			<div class="flex flex-col gap-2">
				<!-- Language picker -->
				<div class="flex items-center gap-2">
					<span class="text-[11px] text-[var(--text-muted)]">Language:</span>
					<div class="flex items-center border border-[var(--border)] rounded overflow-hidden">
						{#each SUPPORTED_LANGUAGES as lang}
							<button
								onclick={() => handleResponseLanguageChange(lang.id)}
								class="text-[11px] px-2.5 py-1 transition-colors {customResponseLanguage === lang.id
									? 'bg-[var(--accent)] text-white'
									: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
							>
								{lang.label}
							</button>
						{/each}
					</div>
					{#if !customResponseScript}
						<button
							onclick={() => { customResponseScript = DEFAULT_RESPONSE_SCRIPTS[customResponseLanguage]; markDirty(); }}
							class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
						>
							Insert template
						</button>
					{/if}
					<button
						onclick={() => (showResponseExample = !showResponseExample)}
						class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
					>
						{showResponseExample ? 'Hide' : 'Show'} example
					</button>
					<button
						onclick={() => (showResponseReqRef = !showResponseReqRef)}
						class="text-[11px] px-2 py-1 rounded border border-[var(--border)] hover:bg-[var(--bg-hover)] transition-colors flex items-center gap-1 {showResponseReqRef ? 'text-[var(--accent)] border-[var(--accent)]/40' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
						title="Show request object reference"
					>
						<BookOpen class="w-3 h-3" />
						Reference
					</button>
				</div>

				{#if showResponseExample}
					<pre class="text-[10px] text-[var(--text-muted)] bg-[var(--bg)] border border-[var(--border)] rounded p-2 overflow-x-auto whitespace-pre">{RESPONSE_HANDLER_EXAMPLES[customResponseLanguage]}</pre>
				{/if}

				{#if showResponseReqRef}
					<div class="bg-[var(--bg)] border border-[var(--border)] rounded">
						<div class="px-3 py-2 text-xs font-medium text-[var(--text-muted)] border-b border-[var(--border)] flex items-center gap-1.5">
							<BookOpen class="w-3 h-3" />
							{#if testRequest}
								<span>Request Object — <span class="text-[var(--accent)]">{testRequest.method} {testRequest.path}</span></span>
							{:else}
								<span>Request Object Shape ({SUPPORTED_LANGUAGES.find(l => l.id === customResponseLanguage)?.label})</span>
							{/if}
						</div>
						<pre class="px-3 py-2 text-[11px] font-mono text-[var(--text-muted)] overflow-x-auto max-h-48 leading-relaxed whitespace-pre">{formatRequestReference(testRequest, customResponseLanguage)}</pre>
						{#if !testRequest}
							<div class="px-3 py-1.5 text-[10px] text-[var(--text-muted)] italic border-t border-[var(--border)]">
								Select a request to see its actual data here.
							</div>
						{/if}
					</div>
				{/if}

				{#if isServerMode && customResponseLanguage !== 'javascript'}
					<div class="flex items-start gap-2 text-[10px] text-[var(--yellow)] bg-yellow-500/5 border border-yellow-500/20 rounded px-3 py-2">
						<AlertCircle class="w-3 h-3 flex-shrink-0 mt-0.5" />
						<p>Server mode only supports JavaScript. {customResponseLanguage === 'lua' ? 'Lua' : 'Jsonnet'} will only run when the browser tab is open.</p>
					</div>
				{/if}

				<CodeEditor
					value={customResponseScript}
					onchange={handleResponseScriptChange}
					placeholder={DEFAULT_RESPONSE_SCRIPTS[customResponseLanguage].split('\n')[0]}
					minHeight="140px"
					language={customResponseLanguage}
				/>
			</div>
		{:else}
			<div class="text-[10px] text-[var(--text-muted)] italic bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-2">
				Disabled — the server returns <code class="text-[10px]">200 OK</code> with the default JSON response.
			</div>
		{/if}
	</section>

	<!-- Save Settings button -->
	<div class="flex items-center justify-end gap-3 pt-2">
		{#if saved}
			<span class="text-xs text-[var(--green)] flex items-center gap-1">
				<Check class="w-3 h-3" /> Settings saved
			</span>
		{/if}
		<button
			onclick={saveSettings}
			class="text-xs px-4 py-1.5 rounded bg-[var(--accent)] hover:bg-[var(--accent-hover)] text-white transition-colors flex items-center gap-1.5 {dirty ? 'ring-2 ring-[var(--accent)]/30' : ''}"
		>
			<Save class="w-3 h-3" />
			Save Settings
		</button>
	</div>

	<!-- Divider -->
	<hr class="border-[var(--border)]" />

	<!-- Section 4: Danger Zone -->
	<section>
		<h4 class="text-xs font-semibold text-[var(--red)] uppercase tracking-wider flex items-center mb-3">
			Danger Zone
			<Tooltip text="Permanent actions that cannot be undone." />
		</h4>
		<div class="flex items-center justify-between bg-[var(--bg)] border border-[var(--red)]/20 rounded-lg px-3 py-2.5">
			<div>
				<p class="text-xs text-[var(--text)]">Delete this endpoint</p>
				<p class="text-[10px] text-[var(--text-muted)]">Permanently remove this endpoint and all captured requests.</p>
			</div>
			<button
				onclick={() => {
					if (confirm('Are you sure? This will delete the endpoint and all captured requests permanently.')) {
						onUpdate({ config: { ...endpoint.config, _delete: true } });
					}
				}}
				class="text-xs px-3 py-1.5 rounded border border-[var(--red)]/30 text-[var(--red)] hover:bg-[var(--red)]/10 transition-colors flex-shrink-0"
			>
				Delete Endpoint
			</button>
		</div>
	</section>
</div>
