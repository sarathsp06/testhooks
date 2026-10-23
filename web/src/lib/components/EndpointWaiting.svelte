<script lang="ts">
	import { getWebhookURL, copyToClipboard } from '$lib/utils';
	import { Copy, Check, Loader2 } from 'lucide-svelte';
	import type { Endpoint } from '$lib/types';
	import { PROVIDER_SAMPLES, samplesByProvider, type ProviderSample } from '$lib/samples';

	let {
		endpoint,
		onOpenTab
	}: {
		endpoint: Endpoint;
		/** Switch the endpoint page to one of its configuration tabs. */
		onOpenTab: (tab: 'transform' | 'reply' | 'settings') => void;
	} = $props();

	// Which sample the Send button posts. The picker is a plain <select>, so
	// the card stays one button and one line, however many providers are listed.
	let sampleId = $state(PROVIDER_SAMPLES[0].id);
	const sampleGroups = samplesByProvider();
	const sample = $derived(PROVIDER_SAMPLES.find((s) => s.id === sampleId) ?? PROVIDER_SAMPLES[0]);

	const url = $derived(getWebhookURL(endpoint.slug));
	const curlCommand = $derived(`curl -X POST \\\n  ${url} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"hello":"world"}'`);
	const isServer = $derived(endpoint.mode === 'server');
	const hasTransform = $derived(!!endpoint.config?.wasm_script);
	const hasForward = $derived(!!endpoint.config?.forward_url);
	const hasReply = $derived(!!endpoint.config?.custom_response?.enabled);
	const sig = $derived(endpoint.config?.signature);
	const hasSignature = $derived(!!(sig?.secret_set || sig?.public_key));

	let copiedUrl = $state(false);
	let copiedCurl = $state(false);
	let sampleState = $state<'idle' | 'sending' | 'sent' | 'error'>('idle');
	let sampleError = $state('');

	async function copyUrl() {
		if (await copyToClipboard(url)) {
			copiedUrl = true;
			setTimeout(() => (copiedUrl = false), 2000);
		}
	}

	async function copyCurl() {
		if (await copyToClipboard(curlCommand)) {
			copiedCurl = true;
			setTimeout(() => (copiedCurl = false), 2000);
		}
	}

	async function sendSample() {
		sampleState = 'sending';
		sampleError = '';
		try {
			const s: ProviderSample = sample;
			const res = await fetch(`/h/${endpoint.slug}${s.path}`, {
				method: 'POST',
				headers: s.headers,
				body: typeof s.body === 'string' ? s.body : JSON.stringify(s.body)
			});
			// Any reply means the request was captured; a custom reply may use any status.
			sampleState = res.status === 404 ? 'error' : 'sent';
			if (res.status === 404) sampleError = 'This endpoint no longer exists.';
		} catch (err) {
			sampleState = 'error';
			sampleError = err instanceof Error ? err.message : 'Request failed';
		}
	}
</script>

{#snippet stepIcon(on: boolean)}
	{#if on}
		<span class="flex-none w-6 h-6 rounded-full bg-[#1A7F4B] text-white flex items-center justify-center" aria-hidden="true">
			<Check class="w-[13px] h-[13px]" strokeWidth={3} />
		</span>
	{:else}
		<span class="flex-none w-6 h-6 rounded-full border-[1.5px] border-dashed border-faint/70" aria-hidden="true"></span>
	{/if}
{/snippet}

{#snippet stepAction(on: boolean, label: string, tab: 'transform' | 'reply' | 'settings')}
	<button
		type="button"
		onclick={() => onOpenTab(tab)}
		class="flex-none text-[13px] font-medium text-accent hover:underline py-0.5"
	>
		{on ? 'Edit' : label}
	</button>
{/snippet}

<div class="w-full max-w-[1240px] mx-auto px-4 sm:px-6 pt-8 sm:pt-10 pb-16 flex flex-wrap gap-8 items-start">
	<section class="flex-[999_1_560px] min-w-0 flex flex-col gap-7">
		<!-- URL -->
		<div class="flex flex-col gap-2.5">
			<span class="text-[13px] font-medium text-soft">Your webhook URL</span>
			<div class="flex items-stretch bg-code rounded-xl overflow-hidden border border-transparent dark:border-line">
				<div class="flex-auto min-w-0 break-all sm:break-normal sm:overflow-x-auto sm:whitespace-nowrap px-[18px] py-4 font-mono text-[15px] text-code-ink">{url}</div>
				<button
					type="button"
					onclick={copyUrl}
					class="flex-none min-w-[104px] border-0 border-l border-code-line bg-code-button text-code-ink text-sm font-medium flex items-center justify-center gap-2 px-[18px] cursor-pointer hover:text-code-green transition-colors"
				>
					{#if copiedUrl}
						<Check class="w-[15px] h-[15px] text-code-green" /> Copied
					{:else}
						<Copy class="w-[15px] h-[15px]" /> Copy
					{/if}
				</button>
			</div>
			<span class="text-[13px] text-faint">
				Any method and any path after it work, for example <span class="font-mono text-soft break-all">/h/{endpoint.slug}/events</span>.
			</span>
			<span class="text-[13px] text-faint">Anyone with this URL can send to it, and anyone with this page's link can read what arrived. Requests stay on this server and, by default, the oldest are pruned past 500 requests or 10 MB per endpoint. For private traffic, <a class="underline hover:text-accent" href="https://sarathsp06.github.io/testhooks/getting-started/installation/" target="_blank" rel="noopener">self-host it</a>.</span>
		</div>

		<!-- Waiting card -->
		<div class="bg-surface border border-line rounded-[14px] p-5 sm:p-8 flex flex-col gap-6">
			<div class="flex items-start gap-3.5">
				<span class="relative mt-[9px] w-3.5 h-3.5 flex-none flex items-center justify-center" aria-hidden="true">
					<span class="absolute inset-0 rounded-full bg-accent/20 motion-safe:animate-ping"></span>
					<span class="absolute inset-0 rounded-full bg-accent/20"></span>
					<span class="relative w-2 h-2 rounded-full bg-accent"></span>
				</span>
				<div class="flex flex-col gap-0.5">
					<h1 class="m-0 text-[22px] font-semibold tracking-[-0.015em] text-ink">Waiting for your first request</h1>
					<span class="text-sm text-body">It appears here the moment it arrives. No refresh needed.</span>
				</div>
			</div>

			<div class="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3">
				<div class="border-[1.5px] border-ink rounded-xl p-[18px] flex flex-col gap-3">
					<span class="font-mono text-xs text-accent">quickest</span>
					<span class="text-[15px] font-semibold text-ink">Send a sample from here</span>
					<span class="text-sm leading-normal text-body">Posts an example event to your URL so you can look around right away. Pick a provider's shape if you have one in mind.</span>
					<label class="flex flex-col gap-1 text-[13px] text-soft">
						<span>Sample</span>
						<select
							bind:value={sampleId}
							class="font-sans text-sm text-ink bg-page border border-line-strong rounded-[9px] px-3 min-h-10 focus:outline-none focus:border-accent"
						>
							{#each sampleGroups as g (g.provider)}
								<optgroup label={g.provider}>
									{#each g.items as item (item.id)}
										<option value={item.id}>{item.label}</option>
									{/each}
								</optgroup>
							{/each}
						</select>
					</label>
					<div class="mt-auto flex flex-col gap-2">
						<button
							type="button"
							onclick={sendSample}
							disabled={sampleState === 'sending'}
							class="self-start text-sm font-medium text-on-ink bg-ink border-0 rounded-[9px] px-4 min-h-11 cursor-pointer flex items-center gap-2 hover:opacity-90 disabled:opacity-60 transition-opacity"
						>
							{#if sampleState === 'sending'}
								<Loader2 class="w-4 h-4 animate-spin" /> Sending…
							{:else}
								Send sample request
							{/if}
						</button>
						{#if sampleState === 'sent'}
							<span class="text-[13px] text-accent" role="status">Sent. It should show up in a moment.</span>
						{:else if sampleState === 'error'}
							<span class="text-[13px] text-danger" role="status">Couldn't send it: {sampleError}</span>
						{/if}
					</div>
				</div>

				<div class="border border-line rounded-xl p-[18px] flex flex-col gap-3 min-w-0">
					<span class="font-mono text-xs text-faint">terminal</span>
					<span class="text-[15px] font-semibold text-ink">Use curl</span>
					<pre class="m-0 bg-code text-code-ink rounded-[9px] p-3 font-mono text-xs leading-[1.6] whitespace-pre-wrap break-all">{curlCommand}</pre>
					<button
						type="button"
						onclick={copyCurl}
						class="mt-auto self-start text-sm font-medium text-ink bg-surface border border-line-strong rounded-[9px] px-4 min-h-11 cursor-pointer flex items-center gap-2 hover:bg-chip transition-colors"
					>
						{#if copiedCurl}
							<Check class="w-4 h-4 text-accent" /> Copied
						{:else}
							Copy command
						{/if}
					</button>
				</div>

				<div class="border border-line rounded-xl p-[18px] flex flex-col gap-3">
					<span class="font-mono text-xs text-faint">real events</span>
					<span class="text-[15px] font-semibold text-ink">From a service</span>
					<span class="text-sm leading-normal text-body">Paste the URL into the service's webhook settings, then trigger an event there.</span>
					<span class="mt-auto text-[13px] leading-normal text-faint">
						The service must be able to reach this server. A local install only receives requests from your machine.
					</span>
				</div>
			</div>
		</div>
	</section>

	<!-- Pipeline -->
	<aside class="flex-[1_1_320px] min-w-0 flex flex-col gap-3.5">
		<div class="flex flex-col gap-1">
			<h2 class="m-0 text-[15px] font-semibold text-ink">What happens to each request</h2>
			<span class="text-sm text-body">Capturing is always on. Add the rest only if you need it.</span>
		</div>
		<ol class="m-0 p-0 list-none flex flex-col gap-2">
			<li class="flex gap-3 items-start bg-surface border border-line rounded-xl px-4 py-3.5">
				{@render stepIcon(true)}
				<span class="flex flex-col gap-0.5 flex-1 min-w-0">
					<span class="text-sm font-semibold text-ink">Capture</span>
					<span class="text-[13px] text-body">
						{isServer ? 'Headers, query and body, saved on the server.' : 'Headers, query and body, kept in this browser tab.'}
					</span>
				</span>
				<span class="flex-none text-xs font-medium text-accent">On</span>
			</li>
			<li class="flex gap-3 items-start bg-surface border border-line rounded-xl px-4 py-3.5">
				{@render stepIcon(hasSignature)}
				<span class="flex flex-col gap-0.5 flex-1 min-w-0">
					<span class="text-sm font-semibold text-ink">Verify signatures</span>
					<span class="text-[13px] text-body">
						{#if hasSignature}
							Checks Standard Webhooks signatures{sig?.enforce ? ' and rejects invalid ones with 401' : ''}.
						{:else}
							Paste the sender's webhook secret (Sparrow, Svix, Standard Webhooks) to check each signature.
						{/if}
					</span>
				</span>
				{@render stepAction(hasSignature, 'Add', 'settings')}
			</li>
			<li class="flex gap-3 items-start bg-surface border border-line rounded-xl px-4 py-3.5">
				{@render stepIcon(hasTransform)}
				<span class="flex flex-col gap-0.5 flex-1 min-w-0">
					<span class="text-sm font-semibold text-ink">Transform</span>
					<span class="text-[13px] text-body">Reshape the request with a few lines of JavaScript.</span>
				</span>
				{@render stepAction(hasTransform, 'Add', 'transform')}
			</li>
			<li class="flex gap-3 items-start bg-surface border border-line rounded-xl px-4 py-3.5">
				{@render stepIcon(hasForward)}
				<span class="flex flex-col gap-0.5 flex-1 min-w-0">
					<span class="text-sm font-semibold text-ink">Forward</span>
					<span class="text-[13px] text-body break-words">
						{#if hasForward}
							To <span class="font-mono">{endpoint.config?.forward_url}</span>
						{:else}
							Send it on to another URL{isServer ? ', with retries' : ', even localhost'}.
						{/if}
					</span>
				</span>
				{@render stepAction(hasForward, 'Add', 'settings')}
			</li>
			<li class="flex gap-3 items-start bg-surface border border-line rounded-xl px-4 py-3.5">
				{@render stepIcon(hasReply)}
				<span class="flex flex-col gap-0.5 flex-1 min-w-0">
					<span class="text-sm font-semibold text-ink">Reply</span>
					<span class="text-[13px] text-body">
						{#if hasReply}
							A script decides the status and body.
						{:else if hasForward && endpoint.config?.forward_mode === 'sync'}
							{isServer ? "The forward target's reply" : "Your app's reply"} goes back to the sender as-is.
						{:else}
							Default <span class="font-mono">{isServer ? '200 OK' : '202 Accepted'}</span>. Script a different status or body{#if !isServer}, or have your local app answer{/if}.
						{/if}
					</span>
				</span>
				{@render stepAction(hasReply, 'Change', 'reply')}
			</li>
		</ol>
		<a
			href="https://sarathsp06.github.io/testhooks/guides/modes/"
			target="_blank"
			rel="noopener noreferrer"
			class="text-[13px] text-soft underline hover:text-accent self-start"
		>
			How server and browser mode differ
		</a>
		<button
			type="button"
			onclick={() => onOpenTab('settings')}
			class="mt-2 text-left bg-surface border border-line rounded-[12px] px-4 py-3.5 flex flex-col gap-1 cursor-pointer hover:border-line-strong"
		>
			<span class="text-sm font-semibold text-ink">Endpoint settings</span>
			<span class="text-[13px] leading-normal text-body">Name, mode, forwarding, signature verification, or delete this endpoint.</span>
		</button>
	</aside>
</div>
