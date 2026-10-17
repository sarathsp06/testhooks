<script lang="ts">
	import { goto } from '$app/navigation';
	import { createEndpoint, listEndpoints, deleteEndpoint } from '$lib/api';
	import { getWebhookURL, formatTime, copyToClipboard } from '$lib/utils';
	import { SUPPORTED_LANGUAGES, DEFAULT_SCRIPTS, DEFAULT_RESPONSE_SCRIPTS, initWasm, isWasmReady, validateResponseHandler, type TransformLanguage } from '$lib/wasm';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import {
		Trash2,
		Copy,
		ExternalLink,
		Server,
		Monitor,
		ChevronDown,
		ChevronRight,
		Send,
		Code2,
		MessageSquare,
		Webhook,
		X,
		Check,
		AlertTriangle,
		Loader2
	} from 'lucide-svelte';
	import type { Endpoint, EndpointConfig } from '$lib/types';

	let endpoints = $state<Endpoint[]>([]);
	let loading = $state(true);
	let creating = $state(false);
	let copied = $state<string | null>(null);
	let deleteConfirm = $state<string | null>(null);
	let createError = $state<string | null>(null);
	let loadError = $state<string | null>(null);
	let deleteError = $state<string | null>(null);
	let forwardUrlError = $state<string | null>(null);

	// Creation form state
	let newName = $state('');
	let newMode = $state<'server' | 'browser'>('server');
	let showAdvanced = $state(false);

	// Advanced: Forward URL (single URL + mode)
	let newForwardUrl = $state('');
	let newForwardMode = $state<'async' | 'sync'>('async');

	// Advanced: Transform
	let newTransformEnabled = $state(false);
	let newTransformLanguage = $state<TransformLanguage>('javascript');
	let newTransformScript = $state('');

	// Advanced: Custom Response (script-based)
	let newCustomResponseEnabled = $state(false);
	let newCustomResponseLanguage = $state<TransformLanguage>('javascript');
	let newCustomResponseScript = $state('');
	let newResponseValidating = $state(false);
	let newResponseValidationResult = $state<{ valid: boolean; error?: string } | null>(null);

	async function handleNewResponseValidate() {
		if (!isWasmReady(newCustomResponseLanguage)) {
			await initWasm(newCustomResponseLanguage);
		}
		if (!isWasmReady(newCustomResponseLanguage)) return;
		newResponseValidating = true;
		newResponseValidationResult = null;
		try {
			newResponseValidationResult = await validateResponseHandler(
				newCustomResponseScript || DEFAULT_RESPONSE_SCRIPTS[newCustomResponseLanguage],
				newCustomResponseLanguage
			);
		} finally {
			newResponseValidating = false;
		}
	}

	async function loadEndpoints() {
		loading = true;
		loadError = null;
		try {
			endpoints = await listEndpoints();
		} catch (err) {
			endpoints = [];
			loadError = err instanceof Error ? err.message : 'Failed to load endpoints';
		} finally {
			loading = false;
		}
	}

	function resetForm() {
		newName = '';
		newMode = 'server';
		showAdvanced = false;
		newForwardUrl = '';
		newForwardMode = 'async';
		newTransformEnabled = false;
		newTransformLanguage = 'javascript';
		newTransformScript = '';
		newCustomResponseEnabled = false;
		newCustomResponseLanguage = 'javascript';
		newCustomResponseScript = '';
	}

	async function handleCreate() {
		creating = true;
		createError = null;
		try {
			const config: EndpointConfig = {};

			if (newForwardUrl.trim()) {
				config.forward_url = newForwardUrl.trim();
				config.forward_mode = newForwardMode;
			}
			if (newTransformEnabled && newTransformScript.trim()) {
				config.wasm_script = newTransformScript;
				config.transform_language = newTransformLanguage;
			}
			if (newCustomResponseEnabled && newCustomResponseScript.trim()) {
				config.custom_response = {
					enabled: true,
					script: newCustomResponseScript,
					language: newCustomResponseLanguage
				};
			}

			const hasConfig = Object.keys(config).length > 0;

			const ep = await createEndpoint({
				name: newName || undefined,
				mode: newMode,
				config: hasConfig ? config : undefined
			});
			resetForm();
			goto(`/${ep.slug}`);
		} catch (err) {
			createError = err instanceof Error ? err.message : 'Failed to create endpoint. Please try again.';
		} finally {
			creating = false;
		}
	}

	function validateForwardUrl(): boolean {
		const url = newForwardUrl.trim();
		if (!url) return true;
		try {
			new URL(url);
			forwardUrlError = null;
			return true;
		} catch {
			forwardUrlError = 'Invalid URL. Must include protocol (e.g. https://example.com)';
			return false;
		}
	}

	async function handleDelete(id: string) {
		deleteError = null;
		const backup = endpoints;
		try {
			endpoints = endpoints.filter((e) => e.id !== id);
			await deleteEndpoint(id);
		} catch (err) {
			endpoints = backup;
			deleteError = err instanceof Error ? err.message : 'Failed to delete endpoint';
		} finally {
			deleteConfirm = null;
		}
	}

	async function handleCopy(slug: string) {
		await copyToClipboard(getWebhookURL(slug));
		copied = slug;
		setTimeout(() => (copied = null), 2000);
	}

	$effect(() => {
		loadEndpoints();
	});

</script>

<div class="min-h-screen">
	<!-- ===== CREATE ENDPOINT SECTION ===== -->
	<section id="create-section" class="pt-10 pb-6 bg-[var(--bg)]">
		<div class="max-w-3xl mx-auto px-6">
			<div class="mb-6">
				<h1 class="text-2xl font-semibold tracking-tight">Capture, inspect and forward webhooks</h1>
				<p class="mt-1.5 text-sm text-[var(--text-muted)]">
					Create an endpoint, point a webhook sender at its URL and watch each request arrive live.
					In browser mode your tab can also forward requests to localhost.
					<a href="https://sarathsp06.github.io/testhooks/getting-started/quickstart/" target="_blank" rel="noopener noreferrer" class="text-[var(--accent)] hover:underline">How it works</a>
				</p>
			</div>
			<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded-xl overflow-hidden">
				<div class="px-6 pt-6 pb-4">
					<div class="flex items-center gap-3 mb-1">
						<Webhook class="w-5 h-5 text-[var(--accent)]" />
						<h2 class="text-lg font-semibold font-fira">Create New Endpoint</h2>
					</div>
					<p class="text-xs text-[var(--text-muted)] mb-4 ml-8">Get a unique URL to capture, inspect, and forward webhooks in real time.</p>

					<!-- Row 1: Name + Mode (stacked on phones) -->
					<div class="flex flex-col sm:flex-row gap-3 mb-3">
						<input
							bind:value={newName}
							placeholder="Endpoint name (optional)"
							class="flex-1 min-w-0 bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-2 text-sm text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)] transition-colors"
						/>
						<div class="flex gap-1 self-start bg-[var(--bg)] border border-[var(--border)] rounded-lg p-1">
							<button
								class="px-3 py-1.5 rounded-md text-xs font-medium transition-colors flex items-center gap-1.5 {newMode === 'server' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
								onclick={() => (newMode = 'server')}
								title="Stores requests in database, always-on transforms & forwarding"
							>
								<Server class="w-3.5 h-3.5" />Server
							</button>
							<button
								class="px-3 py-1.5 rounded-md text-xs font-medium transition-colors flex items-center gap-1.5 {newMode === 'browser' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
								onclick={() => (newMode = 'browser')}
								title="No data stored on server, all processing in browser"
							>
								<Monitor class="w-3.5 h-3.5" />Browser
							</button>
						</div>
					</div>

					<!-- Mode description -->
					<div class="text-[11px] text-[var(--text-muted)] mb-4 px-1">
						{#if newMode === 'server'}
							Requests are stored on the server. Transforms and forwarding run server-side, always-on even without a browser.
						{:else}
							Privacy-first: payloads never touch the server's disk. Transforms and forwarding run in your browser. Ideal for localhost targets.
						{/if}
					</div>
				</div>

				<!-- Advanced Configuration toggle -->
				<button
					class="w-full px-6 py-2.5 border-t border-[var(--border)] flex items-center gap-2 cursor-pointer select-none hover:bg-[var(--bg-hover)] transition-colors"
					onclick={() => (showAdvanced = !showAdvanced)}
				>
					{#if showAdvanced}
						<ChevronDown class="w-3.5 h-3.5 text-[var(--text-muted)]" />
					{:else}
						<ChevronRight class="w-3.5 h-3.5 text-[var(--text-muted)]" />
					{/if}
					<span class="text-xs font-medium text-[var(--text-muted)]">Advanced Configuration</span>
					{#if newForwardUrl.trim() || newTransformEnabled || newCustomResponseEnabled}
						<span class="text-[10px] px-1.5 py-0.5 rounded bg-[var(--accent)]/10 text-[var(--accent)]">
							{[
								newForwardUrl.trim() ? 'forwarding' : '',
								newTransformEnabled ? 'transform' : '',
								newCustomResponseEnabled ? 'custom response' : ''
							].filter(Boolean).join(', ')}
						</span>
					{/if}
				</button>

				{#if showAdvanced}
					<div class="px-6 py-4 border-t border-[var(--border)] flex flex-col gap-5">
						<!-- Forward URL (single) -->
						<div>
							<div class="flex items-center gap-2 mb-2">
								<Send class="w-3.5 h-3.5 text-[var(--text-muted)]" />
								<span class="text-xs font-medium text-[var(--text)]">Forward URL</span>
								<span class="text-[10px] text-[var(--text-muted)]">
									{newMode === 'browser' ? '(browser-side fetch - localhost works)' : '(server-side HTTP - public URLs only)'}
								</span>
							</div>
							<div class="flex flex-col gap-2">
								<input
									bind:value={newForwardUrl}
									placeholder="https://example.com/webhook or http://localhost:3000/hook"
									oninput={() => (forwardUrlError = null)}
									onblur={validateForwardUrl}
									class="bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-1.5 text-sm text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)] {forwardUrlError ? 'border-red-500/50' : ''} transition-colors"
								/>
								{#if forwardUrlError}
									<p class="text-[11px] text-[var(--red)] px-1">{forwardUrlError}</p>
								{/if}
								{#if newForwardUrl.trim()}
									<div class="flex items-center gap-3">
										<span class="text-[11px] text-[var(--text-muted)]">Mode:</span>
										<div class="flex items-center border border-[var(--border)] rounded-lg overflow-hidden">
											<button
												onclick={() => (newForwardMode = 'async')}
												class="text-[11px] px-3 py-1 transition-colors {newForwardMode === 'async'
													? 'bg-[var(--accent)] text-white'
													: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
											>
												Async
											</button>
											<button
												onclick={() => (newForwardMode = 'sync')}
												class="text-[11px] px-3 py-1 transition-colors {newForwardMode === 'sync'
													? 'bg-[var(--accent)] text-white'
													: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
											>
												Sync
											</button>
										</div>
										<span class="text-[10px] text-[var(--text-muted)]">
											{newForwardMode === 'async' ? 'Fire-and-forget, does not block response' : 'Waits for target response, usable in handler'}
										</span>
									</div>
								{/if}
							</div>
						</div>

						<!-- Transform Script -->
						<div>
							<div class="flex items-center justify-between mb-2">
								<div class="flex items-center gap-2">
									<Code2 class="w-3.5 h-3.5 text-[var(--text-muted)]" />
									<span class="text-xs font-medium text-[var(--text)]">Transform Script</span>
								</div>
								<label class="flex items-center gap-2 cursor-pointer">
									<span class="text-[10px] text-[var(--text-muted)]">{newTransformEnabled ? 'Enabled' : 'Disabled'}</span>
									<input
										type="checkbox"
										bind:checked={newTransformEnabled}
										class="w-3.5 h-3.5 rounded border-[var(--border)] accent-[var(--accent)]"
									/>
								</label>
							</div>

							{#if newTransformEnabled}
								<div class="flex flex-col gap-2">
									<div class="flex items-center gap-2">
										<span class="text-[11px] text-[var(--text-muted)]">Language:</span>
										<div class="flex items-center border border-[var(--border)] rounded-lg overflow-hidden">
											{#each SUPPORTED_LANGUAGES as lang}
												<button
													onclick={() => { newTransformLanguage = lang.id; newTransformScript = ''; }}
													class="text-[11px] px-2.5 py-1 transition-colors {newTransformLanguage === lang.id
														? 'bg-[var(--accent)] text-white'
														: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
												>
													{lang.label}
												</button>
											{/each}
										</div>
										{#if !newTransformScript}
											<button
												onclick={() => (newTransformScript = DEFAULT_SCRIPTS[newTransformLanguage])}
												class="text-[11px] px-2 py-1 rounded-lg border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
											>
												Insert template
											</button>
										{/if}
									</div>
									<CodeEditor
										value={newTransformScript}
										onchange={(v) => (newTransformScript = v)}
										placeholder={DEFAULT_SCRIPTS[newTransformLanguage].split('\n')[0]}
										minHeight="140px"
										language={newTransformLanguage}
									/>
								</div>
							{/if}
						</div>

						<!-- Custom Response (script-based) -->
						<div>
							<div class="flex items-center justify-between mb-2">
								<div class="flex items-center gap-2">
									<MessageSquare class="w-3.5 h-3.5 text-[var(--text-muted)]" />
									<span class="text-xs font-medium text-[var(--text)]">Custom Response</span>
									<span class="text-[10px] text-[var(--text-muted)]">(what webhook senders receive back)</span>
								</div>
								<label class="flex items-center gap-2 cursor-pointer">
									<span class="text-[10px] text-[var(--text-muted)]">{newCustomResponseEnabled ? 'Enabled' : 'Disabled'}</span>
									<input
										type="checkbox"
										bind:checked={newCustomResponseEnabled}
										class="w-3.5 h-3.5 rounded border-[var(--border)] accent-[var(--accent)]"
									/>
								</label>
							</div>

							{#if newCustomResponseEnabled}
								<div class="flex flex-col gap-2">
									<div class="flex items-center gap-2">
										<span class="text-[11px] text-[var(--text-muted)]">Language:</span>
										<div class="flex items-center border border-[var(--border)] rounded-lg overflow-hidden">
											{#each SUPPORTED_LANGUAGES as lang}
												<button
													onclick={() => { newCustomResponseLanguage = lang.id; newCustomResponseScript = ''; }}
													class="text-[11px] px-2.5 py-1 transition-colors {newCustomResponseLanguage === lang.id
														? 'bg-[var(--accent)] text-white'
														: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
												>
													{lang.label}
												</button>
											{/each}
										</div>
										{#if !newCustomResponseScript}
											<button
												onclick={() => (newCustomResponseScript = DEFAULT_RESPONSE_SCRIPTS[newCustomResponseLanguage])}
												class="text-[11px] px-2 py-1 rounded-lg border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
											>
												Insert template
											</button>
										{/if}
										<button
											onclick={handleNewResponseValidate}
											disabled={newResponseValidating || !newCustomResponseScript}
											class="text-[11px] px-2.5 py-1 rounded-lg border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors flex items-center gap-1 disabled:opacity-50"
										>
											{#if newResponseValidating}
												<Loader2 class="w-3 h-3 animate-spin" />
											{:else}
												<Check class="w-3 h-3" />
											{/if}
											Validate
										</button>
									</div>
									{#if newMode === 'server' && newCustomResponseLanguage !== 'javascript'}
										<p class="text-[10px] text-[var(--yellow)]">Server mode only supports JavaScript. {newCustomResponseLanguage === 'lua' ? 'Lua' : 'Jsonnet'} will only run when the browser tab is open.</p>
									{/if}
									<CodeEditor
										value={newCustomResponseScript}
										onchange={(v) => (newCustomResponseScript = v)}
										placeholder={DEFAULT_RESPONSE_SCRIPTS[newCustomResponseLanguage].split('\n')[0]}
										minHeight="140px"
										language={newCustomResponseLanguage}
									/>

									{#if newResponseValidationResult}
										<div class="text-xs px-3 py-2 rounded border {newResponseValidationResult.valid ? 'border-green-500/30 bg-green-500/5 text-[var(--green)]' : 'border-red-500/30 bg-red-500/5 text-[var(--red)]'}">
											{#if newResponseValidationResult.valid}
												<span class="flex items-center gap-1"><Check class="w-3 h-3" /> Handler script is valid</span>
											{:else}
												<span class="flex items-center gap-1"><AlertTriangle class="w-3 h-3" /> {newResponseValidationResult.error}</span>
											{/if}
										</div>
									{/if}
								</div>
							{/if}
						</div>
					</div>
				{/if}

				<!-- Error message -->
				{#if createError}
					<div class="mx-6 mt-4 px-3 py-2 rounded bg-red-500/10 border border-red-500/20 text-sm text-[var(--red)] flex items-center justify-between">
						<span>{createError}</span>
						<button onclick={() => (createError = null)} class="ml-2 p-0.5 rounded hover:bg-red-500/20 transition-colors">
							<X class="w-3.5 h-3.5" />
						</button>
					</div>
				{/if}

				<!-- Create button -->
				<div class="px-6 py-4 border-t border-[var(--border)]">
					<button
						onclick={handleCreate}
						disabled={creating}
						class="w-full bg-[var(--accent)] hover:bg-[var(--accent-hover)] text-white rounded-xl px-4 py-2.5 text-sm font-medium transition-colors disabled:opacity-50 flex items-center justify-center gap-2 shadow-lg shadow-blue-500/20"
					>
						<Webhook class="w-4 h-4" />
						{creating ? 'Creating...' : 'Create Endpoint'}
					</button>
				</div>
			</div>
		</div>
	</section>

	<!-- ===== ENDPOINTS LIST ===== -->
	<section class="pt-4 pb-12 bg-[var(--bg)]">
		<div class="max-w-3xl mx-auto px-6">
			{#if deleteError}
				<div class="mb-3 px-3 py-2 rounded bg-red-500/10 border border-red-500/20 text-sm text-[var(--red)] flex items-center justify-between">
					<span>{deleteError}</span>
					<button onclick={() => (deleteError = null)} class="ml-2 p-0.5 rounded hover:bg-red-500/20 transition-colors">
						<X class="w-3.5 h-3.5" />
					</button>
				</div>
			{/if}
			<h2 class="text-lg font-semibold font-fira mb-4">
				Your Endpoints
				{#if endpoints.length > 0}
					<span class="text-sm font-normal text-[var(--text-muted)] ml-2">({endpoints.length})</span>
				{/if}
			</h2>

			{#if loading}
				<div class="text-center py-12 text-[var(--text-muted)]">Loading...</div>
			{:else if loadError}
				<div class="px-4 py-3 rounded bg-red-500/10 border border-red-500/20 text-sm text-[var(--red)] flex items-center justify-between">
					<span>{loadError}</span>
					<button onclick={loadEndpoints} class="ml-2 text-xs underline hover:no-underline">Retry</button>
				</div>
			{:else if endpoints.length === 0}
				<div class="text-center py-12 text-[var(--text-muted)]">
					<p class="mb-2">No endpoints yet.</p>
					<p class="text-sm">Create one above to get started.</p>
				</div>
			{:else}
				<div class="flex flex-col gap-2">
					{#each endpoints as ep (ep.id)}
						<div class="bg-[var(--bg)] border border-[var(--border)] rounded-lg p-4 hover:border-[var(--accent)]/30 transition-colors group">
							<div class="flex items-center justify-between">
								<a href="/{ep.slug}" class="flex-1 min-w-0">
									<div class="flex items-center gap-3">
										<span class="text-sm font-mono text-[var(--accent)]">{ep.slug}</span>
										{#if ep.name}
											<span class="text-sm text-[var(--text)] truncate">{ep.name}</span>
										{/if}
										<span class="text-[10px] px-1.5 py-0.5 rounded {ep.mode === 'server' ? 'bg-blue-500/10 text-blue-400' : 'bg-purple-500/10 text-purple-400'} flex items-center gap-1">
											{#if ep.mode === 'server'}<Server class="w-2.5 h-2.5" />{:else}<Monitor class="w-2.5 h-2.5" />{/if}
											{ep.mode}
										</span>
										<!-- Config badges -->
										{#if ep.config?.forward_url}
											<span class="text-[10px] px-1.5 py-0.5 rounded bg-green-500/10 text-[var(--green)]">
												forward
											</span>
										{/if}
										{#if ep.config?.wasm_script}
											<span class="text-[10px] px-1.5 py-0.5 rounded bg-orange-500/10 text-[var(--orange)]">
												{ep.config.transform_language ?? 'js'} transform
											</span>
										{/if}
									</div>
									<div class="text-xs text-[var(--text-muted)] mt-1">
										Created {formatTime(ep.created_at)} &middot; <code class="text-[10px]">{getWebhookURL(ep.slug)}</code>
									</div>
								</a>

								<div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
									<button
										onclick={() => handleCopy(ep.slug)}
										class="p-2 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
										title="Copy webhook URL"
									>
										{#if copied === ep.slug}
											<span class="text-[10px] text-[var(--green)]">Copied!</span>
										{:else}
											<Copy class="w-4 h-4" />
										{/if}
									</button>
									<a
										href="/{ep.slug}"
										class="p-2 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
										title="Open"
									>
										<ExternalLink class="w-4 h-4" />
									</a>
									{#if deleteConfirm === ep.id}
										<button
											onclick={() => handleDelete(ep.id)}
											class="text-[10px] px-2 py-1 rounded bg-red-500/10 text-[var(--red)] hover:bg-red-500/20 transition-colors"
										>
											Confirm
										</button>
										<button
											onclick={() => (deleteConfirm = null)}
											class="text-[10px] px-2 py-1 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] transition-colors"
										>
											Cancel
										</button>
									{:else}
										<button
											onclick={() => (deleteConfirm = ep.id)}
											class="p-2 rounded hover:bg-red-500/10 text-[var(--text-muted)] hover:text-[var(--red)] transition-colors"
											title="Delete endpoint"
										>
											<Trash2 class="w-4 h-4" />
										</button>
									{/if}
								</div>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</section>

	<footer class="py-6 border-t border-[var(--border)]">
		<div class="max-w-3xl mx-auto px-6 flex items-center justify-between gap-4 text-xs text-[var(--text-muted)]">
			<span>testhooks · self-hosted webhook inspector</span>
			<span class="flex gap-4">
				<a href="https://sarathsp06.github.io/testhooks/" target="_blank" rel="noopener noreferrer" class="hover:text-[var(--accent)] transition-colors">Docs</a>
				<a href="https://github.com/sarathsp06/testhooks" target="_blank" rel="noopener noreferrer" class="hover:text-[var(--accent)] transition-colors">GitHub</a>
			</span>
		</div>
	</footer>
</div>
