<script lang="ts">
	import type { Endpoint } from '$lib/types';
	import { Server, Monitor, Globe, Reply, Info, AlertCircle, Check, Save, ShieldCheck } from 'lucide-svelte';
	import Tooltip from './Tooltip.svelte';
	import { getWebhookURL, slugNameError, SLUG_NAME_MAX } from '$lib/utils';

	let {
		endpoint,
		forwardPrivate = false,
		onUpdate
	}: {
		endpoint: Endpoint;
		/** The server may forward to private and loopback addresses (ALLOW_PRIVATE_FORWARD_TARGETS). */
		forwardPrivate?: boolean;
		onUpdate: (patch: { name?: string; slug_name?: string; mode?: 'server' | 'browser'; config?: Record<string, unknown> }) => void | Promise<void>;
	} = $props();

	let editName = $state('');
	let newSlugName = $state('');
	let renaming = $state(false);
	let renameError = $state('');
	const newSlugNameError = $derived(slugNameError(newSlugName));
	let urlError = $state('');
	let saved = $state(false);
	let saveError = $state('');

	// Forward URL and mode from endpoint config
	let forwardUrl = $state('');
	let forwardMode = $state<'sync' | 'async'>('async');
	let persistRequests = $state(false);

	// Standard Webhooks signature verification
	// The secret is write-only: the server only says whether one is stored.
	let sigSecret = $state('');
	let secretSet = $state(false);
	let clearSecret = $state(false);
	let sigPublicKey = $state('');
	let sigEnforce = $state(false);

	// Sync local state from endpoint prop when it changes
	$effect(() => {
		editName = endpoint.name;
	});

	$effect(() => {
		forwardUrl = (endpoint.config?.forward_url as string) ?? '';
		forwardMode = ((endpoint.config?.forward_mode as string) || 'async') as 'sync' | 'async';
		persistRequests = !!endpoint.config?.persist_requests;
		sigSecret = '';
		secretSet = !!endpoint.config?.signature?.secret_set;
		clearSecret = false;
		sigPublicKey = endpoint.config?.signature?.public_key ?? '';
		sigEnforce = !!endpoint.config?.signature?.enforce;
	});

	const isServerMode = $derived(endpoint.mode === 'server');
	const hasSecret = $derived(sigSecret.trim() !== '' || (secretSet && !clearSecret));

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
			if (isServerMode && !forwardPrivate && (parsed.hostname === 'localhost' || parsed.hostname === '127.0.0.1')) {
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
			Promise.resolve(onUpdate({ name: editName })).catch((e) => (saveError = e.message));
		}
	}

	async function changeURL() {
		if (newSlugNameError || renaming) return;
		if (!confirm('Senders using the old URL will get 404 until you update them. Change the URL?')) return;
		renaming = true;
		renameError = '';
		try {
			await onUpdate({ slug_name: newSlugName.trim().toLowerCase() });
			newSlugName = '';
		} catch (e) {
			renameError = e instanceof Error ? e.message : 'Failed to change the URL';
		} finally {
			renaming = false;
		}
	}

	function handleModeChange(mode: 'server' | 'browser') {
		if (mode !== endpoint.mode) {
			Promise.resolve(onUpdate({ mode })).catch((e) => (saveError = e.message));
		}
	}

	async function saveSettings() {
		// Validate forward URL before saving
		if (!validateUrl(forwardUrl)) return;

		saveError = '';
		try {
			await onUpdate({
				config: {
					...endpoint.config,
					forward_url: forwardUrl.trim() || undefined,
					forward_mode: forwardUrl.trim() ? forwardMode : undefined,
					persist_requests: persistRequests || undefined,
					signature:
						hasSecret || sigPublicKey.trim()
							? {
									// Omitted keeps the stored secret; "" removes it.
									secret: sigSecret.trim() || (clearSecret ? '' : undefined),
									public_key: sigPublicKey.trim() || undefined,
									enforce: sigEnforce || undefined
								}
							: undefined
				}
			});
		} catch (e) {
			saveError = e instanceof Error ? e.message : 'Failed to save settings';
			return;
		}

		dirty = false;
		saved = true;
		setTimeout(() => (saved = false), 2000);
	}
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
				<p><strong class="text-[var(--text)]">Server mode</strong> — Requests are stored on the server. Transforms and forwarding run on the server (always-on, headless). The browser is a viewer.</p>
			{:else}
				<p><strong class="text-[var(--text)]">Browser mode</strong> — Requests are relayed via WebSocket and never stored on the server. Transforms and forwarding run in your browser. Supports localhost targets. Data is lost if the tab is closed.</p>
			{/if}
		</div>

		<!-- Webhook URL -->
		<div class="mt-4">
			<label for="slug-name" class="text-xs text-[var(--text-muted)] mb-1 flex items-center">
				Webhook URL
				<Tooltip text="Changing it gives this endpoint a new URL: the start you type (or none) and three new random words. Captured requests and settings stay." />
			</label>
			<p class="text-xs font-mono text-[var(--text)] mb-2 break-all">{getWebhookURL(endpoint.slug)}</p>
			<div class="flex items-center gap-2 flex-wrap">
				<input
					id="slug-name"
					bind:value={newSlugName}
					placeholder="Start the new URL with (optional)"
					maxlength={SLUG_NAME_MAX + 10}
					autocapitalize="off"
					autocomplete="off"
					spellcheck="false"
					aria-invalid={!!newSlugNameError}
					class="flex-1 min-w-[200px] bg-[var(--bg)] border rounded px-3 py-1.5 text-sm font-mono text-[var(--text)] placeholder:font-sans placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)] {newSlugNameError ? 'border-[var(--red)]/60' : 'border-[var(--border)]'}"
				/>
				<button
					onclick={changeURL}
					disabled={!!newSlugNameError || renaming}
					class="text-xs px-3 py-1.5 rounded border border-[var(--border)] text-[var(--text)] hover:border-[var(--accent)] transition-colors disabled:opacity-50 flex-shrink-0"
				>
					{renaming ? 'Changing…' : 'Change URL'}
				</button>
			</div>
			{#if newSlugNameError}
				<p class="mt-1 text-[10px] text-[var(--red)]">{newSlugNameError}</p>
			{:else}
				<p class="mt-1 text-[10px] text-[var(--text-muted)]">
					New URL: <span class="font-mono">/h/{newSlugName.trim() ? newSlugName.trim().toLowerCase() + '-' : ''}three-random-words</span>. Senders using the old URL will get 404 until you update them.
				</p>
			{/if}
			{#if renameError}
				<p class="mt-1 text-[10px] text-[var(--red)] flex items-center gap-1"><AlertCircle class="w-3 h-3" />{renameError}</p>
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
				Requests are forwarded <strong>server-side</strong> via Go HTTP. Runs on every inbound request, even when the browser is closed. {#if forwardPrivate}This server can also reach private addresses and its own <code class="text-[10px]">localhost</code>.{:else}Only public addresses work: localhost and private addresses (10.x, 192.168.x) are refused. Use browser mode to reach them.{/if}
			{:else}
				Requests are forwarded <strong>from this tab</strong> with <code class="text-[10px]">fetch()</code>, so <code class="text-[10px]">localhost</code>, Docker and anything else your machine can reach all work. Only while this tab is open. Your app must allow the cross-origin request (CORS); see <a class="underline" href="https://sarathsp06.github.io/testhooks/guides/localhost/" target="_blank" rel="noopener">Webhooks to localhost</a>.
			{/if}
		</p>

		<div class="flex flex-col gap-3">
			<!-- Forward URL input -->
			<div class="flex flex-col gap-1">
				<span class="text-xs text-[var(--text-muted)]">Target URL</span>
				<input
					bind:value={forwardUrl}
					placeholder={isServerMode ? 'https://example.com/webhook' : 'http://localhost:3000/webhooks/stripe'}
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
			{#if forwardUrl.trim() && !isServerMode}
				<!-- Browser mode: one switch. Sync forwarding here means the sender gets the local app's reply. -->
				<label class="flex items-start gap-3 cursor-pointer group">
					<button
						role="switch"
						aria-checked={forwardMode === 'sync'}
						aria-label="Reply to the sender with my app's response"
						onclick={() => { forwardMode = forwardMode === 'sync' ? 'async' : 'sync'; markDirty(); }}
						class="relative flex-shrink-0 mt-0.5 w-9 h-5 rounded-full transition-colors {forwardMode === 'sync' ? 'bg-[var(--accent)]' : 'bg-[var(--border)]'}"
					>
						<span
							class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform {forwardMode === 'sync' ? 'translate-x-4' : ''}"
						></span>
					</button>
					<span class="flex flex-col gap-0.5">
						<span class="text-xs text-[var(--text)] group-hover:text-[var(--accent)] transition-colors">
							Reply to the sender with my app's response
						</span>
						<span class="text-[10px] text-[var(--text-muted)]">
							{#if forwardMode === 'sync'}
								Testhooks holds the sender's request open while this tab forwards it. Your app's status, headers and body go back to the sender, like a tunnel. No reply within 10 seconds, or no tab open, and the sender gets <code class="text-[10px]">202</code>.
								{#if endpoint.config?.custom_response?.enabled}
									Your reply script runs after the forward and sees your app's reply as <code class="text-[10px]">req.forward_response</code>.
								{/if}
							{:else}
								Off: the sender gets <code class="text-[10px]">202 Accepted</code> straight away and the forward happens in the background.
							{/if}
						</span>
					</span>
				</label>
			{:else if forwardUrl.trim()}
				<div class="flex items-center gap-3">
					<span class="text-xs text-[var(--text-muted)]">Mode</span>
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
					: 'Sync: wait for the forward target to respond. The forward target\'s response is passed to your custom response handler (if configured), which can use it to build the final HTTP response. Without a handler, the target\'s response is sent back to the webhook sender as-is.'} />
				</div>

				{#if forwardMode === 'sync'}
					<div class="flex items-start gap-2 text-[10px] text-[var(--text-muted)] bg-[var(--bg)] rounded px-3 py-2 border border-[var(--border)]">
						<Info class="w-3 h-3 flex-shrink-0 mt-0.5" />
					<p>
						<strong class="text-[var(--text)]">Sync mode</strong> — The webhook response is blocked until the forward target replies.
						{#if endpoint.config?.custom_response?.enabled}
							Your custom response handler will receive the forward target's response as <code class="text-[10px]">req.forward_response</code> and can use it to build the final HTTP response.
						{:else}
							The forward target's response is sent back to the webhook sender as-is. Enable a custom response handler (in the Transform tab) to process it first.
						{/if}
					</p>
					</div>
				{/if}
			{:else}
				<p class="text-[10px] text-[var(--text-muted)] italic">No forward URL configured. Enter a URL above to forward incoming requests.</p>
			{/if}
		</div>
	</section>

	<!-- Section 3: Persist Requests (browser mode only) -->
	{#if !isServerMode}
		<!-- Divider -->
		<hr class="border-[var(--border)]" />

		<section>
			<h4 class="text-xs font-semibold text-[var(--text)] uppercase tracking-wider mb-1">Storage</h4>
			<p class="text-[10px] text-[var(--text-muted)] mb-3">
				Browser-mode endpoints don't store requests on the server by default. Enable persistence to keep a server-side copy of incoming requests so they're available when you return later.
			</p>
			<label class="flex items-center gap-3 cursor-pointer group">
				<button
					role="switch"
					aria-checked={persistRequests}
					aria-label="Persist requests to database"
					onclick={() => { persistRequests = !persistRequests; markDirty(); }}
					class="relative w-9 h-5 rounded-full transition-colors {persistRequests ? 'bg-[var(--accent)]' : 'bg-[var(--border)]'}"
				>
					<span
						class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform {persistRequests ? 'translate-x-4' : ''}"
					></span>
				</button>
				<span class="text-xs text-[var(--text)] group-hover:text-[var(--accent)] transition-colors">
					Persist requests to database
				</span>
				<Tooltip text="When enabled, the server stores a copy of each incoming request in the database (same as server mode). Processing (transforms, forwarding) still happens in your browser." />
			</label>
			{#if persistRequests}
				<div class="mt-2 flex items-start gap-2 text-[10px] text-[var(--text-muted)] bg-[var(--bg)] rounded px-3 py-2 border border-[var(--border)]">
					<Info class="w-3 h-3 flex-shrink-0 mt-0.5" />
					<p>Requests will be stored on the server and available when you return. Transforms and forwarding still run in your browser only.</p>
				</div>
			{/if}
		</section>
	{/if}

	<!-- Signature verification -->
	<hr class="border-[var(--border)]" />

	<section>
		<div class="flex items-center gap-2 mb-1">
			<ShieldCheck class="w-3.5 h-3.5 text-[var(--text-muted)]" />
			<h4 class="text-xs font-semibold text-[var(--text)] uppercase tracking-wider">Signature Verification</h4>
			<Tooltip text="Verify Standard Webhooks signatures (webhook-id, webhook-timestamp, webhook-signature headers), as sent by Sparrow, Svix and others." />
		</div>
		<p class="text-[10px] text-[var(--text-muted)] mb-3">
			Paste the sender's webhook secret (<code class="text-[10px]">whsec_…</code>) or Ed25519 public key. Each captured request is then marked verified or invalid.
		</p>
		<div class="flex flex-col gap-3">
			<label class="flex flex-col gap-1">
				<span class="text-xs text-[var(--text-muted)] flex items-center gap-2">
					Secret (HMAC, v1)
					{#if secretSet && !clearSecret && !sigSecret}
						<span class="text-[var(--green)]">saved, encrypted</span>
						<button
							type="button"
							onclick={() => { clearSecret = true; markDirty(); }}
							class="ml-auto text-[var(--text-muted)] hover:text-[var(--red)] transition-colors"
						>Remove</button>
					{/if}
				</span>
				<input
					bind:value={sigSecret}
					oninput={markDirty}
					type="password"
					placeholder={secretSet && !clearSecret ? 'Saved. Paste a new secret to replace it' : 'whsec_...'}
					autocomplete="off"
					spellcheck="false"
					class="w-full bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-1.5 text-sm font-mono text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)]"
				/>
			</label>
			<label class="flex flex-col gap-1">
				<span class="text-xs text-[var(--text-muted)]">Public key (Ed25519 hex, v1a)</span>
				<input
					bind:value={sigPublicKey}
					oninput={markDirty}
					placeholder="optional"
					autocomplete="off"
					spellcheck="false"
					class="w-full bg-[var(--bg)] border border-[var(--border)] rounded px-3 py-1.5 text-sm font-mono text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)]"
				/>
			</label>
			{#if hasSecret || sigPublicKey.trim()}
				<label class="flex items-center gap-3 cursor-pointer group">
					<button
						role="switch"
						aria-checked={sigEnforce}
						aria-label="Reject requests with invalid signatures"
						onclick={() => { sigEnforce = !sigEnforce; markDirty(); }}
						class="relative w-9 h-5 rounded-full transition-colors {sigEnforce ? 'bg-[var(--accent)]' : 'bg-[var(--border)]'}"
					>
						<span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform {sigEnforce ? 'translate-x-4' : ''}"></span>
					</button>
					<span class="text-xs text-[var(--text)] group-hover:text-[var(--accent)] transition-colors">Reject invalid signatures with 401</span>
					<Tooltip text="Respond like a real receiver would. Rejected requests are still captured here. Senders such as Sparrow treat 401 as permanent and won't retry." />
				</label>
			{/if}
		</div>
	</section>

	<!-- Save Settings button -->
	<div class="flex items-center justify-end gap-3 pt-2">
		{#if saveError}
			<span class="text-xs text-[var(--red)]">{saveError}</span>
		{/if}
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
