<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createEndpoint, listEndpoints, deleteEndpoint } from '$lib/api';
	import { getWebhookURL, formatTime, copyToClipboard, slugNameError, SLUG_NAME_MAX } from '$lib/utils';
	import { Trash2, Copy, Plus, X, Check, Loader2 } from 'lucide-svelte';
	import type { Endpoint } from '$lib/types';

	let endpoints = $state<Endpoint[]>([]);
	let loading = $state(true);
	let creating = $state(false);
	let copied = $state<string | null>(null);
	let deleteConfirm = $state<string | null>(null);
	let createError = $state<string | null>(null);
	let loadError = $state<string | null>(null);
	let deleteError = $state<string | null>(null);

	// Creation form state
	let newName = $state('');
	let newSlugName = $state('');
	let newMode = $state<'server' | 'browser'>('server');

	const modes = [
		{
			id: 'server',
			title: 'On the server',
			description: 'Always on. Requests are saved and forwarded even when this tab is closed.'
		},
		{
			id: 'browser',
			title: 'In this browser tab',
			description: 'Forward to localhost from this tab and let your app answer the sender, like a tunnel with nothing to install. Works while the tab is open.'
		}
	] as const;

	const steps = [
		{ title: 'Create an endpoint', text: 'You get a unique URL like', code: '/h/acid-fable-gully' },
		{ title: 'Send it a request', text: "Paste the URL into a service's webhook settings, or send a sample with one click." },
		{ title: 'Inspect, then go further', text: 'See headers and body, then add a transform, a forward target or a custom reply.' }
	];

	const newSlugNameError = $derived(slugNameError(newSlugName));
	const newSlugPreview = $derived(
		`/h/${newSlugName.trim() && !newSlugNameError ? newSlugName.trim().toLowerCase() + '-' : ''}three-random-words`
	);

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
		newSlugName = '';
		newMode = 'server';
	}

	async function handleCreate(e?: SubmitEvent) {
		e?.preventDefault();
		if (creating || newSlugNameError) return;
		creating = true;
		createError = null;
		try {
			const ep = await createEndpoint({
				name: newName || undefined,
				slug_name: newSlugName.trim() || undefined,
				mode: newMode
			});
			resetForm();
			goto(`/${ep.slug}`);
		} catch (err) {
			createError = err instanceof Error ? err.message : 'Failed to create endpoint. Please try again.';
		} finally {
			creating = false;
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

<svelte:head>
	<!-- Self-referencing, so a self-hosted copy points at itself, not at the demo. -->
	<link rel="canonical" href={page.url.origin + '/'} />
	<meta property="og:type" content="website" />
	<meta property="og:site_name" content="Testhooks" />
	<meta property="og:url" content={page.url.origin + '/'} />
	<meta property="og:title" content="Testhooks: free webhook tester, get a webhook URL in one click" />
	<meta property="og:description" content="Capture and inspect webhooks, test Stripe, GitHub, Shopify or Slack webhooks, forward them to localhost and script the reply. Open source, self-hostable." />
	<meta property="og:image" content={page.url.origin + '/og.jpg'} />
	<meta name="twitter:card" content="summary_large_image" />
	<meta name="twitter:title" content="Testhooks: free webhook tester" />
	<meta name="twitter:description" content="Get a webhook URL in one click. Inspect, transform, forward to localhost and script the reply." />
	<meta name="twitter:image" content={page.url.origin + '/og.jpg'} />
	{@html `<script type="application/ld+json">${JSON.stringify({
		'@context': 'https://schema.org',
		'@type': 'WebApplication',
		name: 'Testhooks',
		applicationCategory: 'DeveloperApplication',
		operatingSystem: 'Any',
		browserRequirements: 'Requires JavaScript',
		url: page.url.origin + '/',
		description: 'Free webhook tester: get a webhook URL in one click, inspect requests, transform and forward them, including to localhost, and script the reply.',
		offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
		sameAs: ['https://github.com/sarathsp06/testhooks', 'https://sarathsp06.github.io/testhooks/']
	})}</script>`}
</svelte:head>

{#snippet sectionHeading(text: string)}
	<h2 class="m-0 font-mono text-[13px] font-medium tracking-[0.04em] uppercase text-soft">{text}</h2>
{/snippet}

{#snippet errorBanner(message: string, onDismiss: () => void)}
	<div class="px-3.5 py-2.5 rounded-[10px] border border-danger/25 bg-danger/5 text-sm text-danger flex items-center justify-between gap-3">
		<span>{message}</span>
		<button type="button" onclick={onDismiss} class="p-1 rounded hover:bg-danger/10 transition-colors" aria-label="Dismiss">
			<X class="w-3.5 h-3.5" />
		</button>
	</div>
{/snippet}

<div class="flex-1 flex flex-col bg-page">
	<div class="flex-1 w-full max-w-[1240px] mx-auto px-4 sm:px-6 pt-12 sm:pt-[72px] pb-16 flex flex-col gap-12 sm:gap-14">
		<!-- ===== CREATE ===== -->
		<section class="flex flex-col gap-7 max-w-[760px]">
			<div class="flex flex-col gap-3.5">
				<h1 class="m-0 text-[clamp(34px,5vw,52px)] leading-[1.05] tracking-[-0.035em] font-semibold text-ink">
					Get a webhook URL in one click.
				</h1>
				<p class="m-0 text-lg leading-[1.55] text-body">
					Point any service at it and watch requests arrive here as they happen. Then transform them, forward them, or script the reply.
				</p>
				<a href="https://www.producthunt.com/products/testhooks?embed=true&utm_source=badge-featured&utm_medium=badge&utm_campaign=badge-testhooks" target="_blank" rel="noopener noreferrer" class="self-start">
					<img alt="Testhooks - Webhooks tester, made for humans and for machines | Product Hunt" width="250" height="54" src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=1298324&t=1793626518707&theme=light" class="dark:hidden" />
					<img alt="Testhooks - Webhooks tester, made for humans and for machines | Product Hunt" width="250" height="54" src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=1298324&t=1793626518707&theme=dark" class="hidden dark:block" />
				</a>
			</div>

			<form onsubmit={handleCreate} class="flex flex-col gap-[18px] bg-surface border border-line rounded-[14px] p-5 sm:p-6">
				<div class="flex gap-3 flex-wrap items-end">
					<label class="flex-[1_1_280px] flex flex-col gap-1.5 text-[13px] font-medium text-soft">
						<span>Name <span class="font-normal text-faint">(optional)</span></span>
						<input
							type="text"
							bind:value={newName}
							placeholder="stripe-payments"
							class="font-sans text-[15px] text-ink px-3.5 py-3 border border-line-strong rounded-[10px] bg-page min-h-[46px] placeholder:text-faint/70 focus:outline-none focus:border-accent focus:ring-2 focus:ring-accent/20 transition-colors"
						/>
					</label>
					<button
						type="submit"
						disabled={creating}
						class="flex-[1_1_auto] sm:flex-none justify-center text-[15px] font-medium text-on-ink bg-ink border-0 rounded-[10px] px-[22px] min-h-[46px] cursor-pointer flex items-center gap-2 hover:opacity-90 disabled:opacity-60 transition-opacity"
					>
						{#if creating}
							<Loader2 class="w-4 h-4 animate-spin" />
							Creating…
						{:else}
							<Plus class="w-4 h-4" strokeWidth={2.2} />
							Create endpoint
						{/if}
					</button>
				</div>

				<div class="flex flex-col gap-1.5">
					<label for="slug-name" class="text-[13px] font-medium text-soft">
						Start the URL with <span class="font-normal text-faint">(optional)</span>
					</label>
					<input
						id="slug-name"
						type="text"
						bind:value={newSlugName}
						placeholder="stripe"
						maxlength={SLUG_NAME_MAX + 10}
						autocapitalize="off"
						autocomplete="off"
						spellcheck="false"
						aria-invalid={!!newSlugNameError}
						aria-describedby="slug-name-hint"
						class="font-mono text-sm text-ink px-3 py-2.5 border rounded-[10px] bg-page max-w-[320px] placeholder:text-faint/70 focus:outline-none focus:border-accent transition-colors {newSlugNameError ? 'border-danger/60' : 'border-line-strong'}"
					/>
					{#if newSlugNameError}
						<p id="slug-name-hint" class="m-0 text-xs text-danger">{newSlugNameError}</p>
					{:else}
						<p id="slug-name-hint" class="m-0 text-[13px] text-faint">
							Your URL will be <span class="font-mono text-soft break-all">{newSlugPreview}</span>
						</p>
					{/if}
				</div>

				<fieldset class="m-0 p-0 border-0 flex flex-col gap-2.5 min-w-0">
					<legend class="p-0 mb-2.5 text-[13px] font-medium text-soft">Where should requests be handled?</legend>
					<div class="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-2.5">
						{#each modes as m (m.id)}
							<label
								class="flex gap-3 items-start rounded-xl px-4 py-3.5 bg-surface cursor-pointer transition-colors {newMode === m.id
									? 'border-[1.5px] border-ink'
									: 'border border-line hover:border-line-strong'}"
							>
								<input type="radio" name="mode" value={m.id} bind:group={newMode} class="mt-[3px] w-4 h-4 accent-accent shrink-0" />
								<span class="flex flex-col gap-1">
									<span class="text-[15px] font-semibold text-ink">{m.title}</span>
									<span class="text-sm leading-normal text-body">{m.description}</span>
								</span>
							</label>
						{/each}
					</div>
					<p class="m-0 text-[13px] text-faint">Not sure? Start on the server. You can switch any time.</p>
				</fieldset>

				{#if createError}
					{@render errorBanner(createError, () => (createError = null))}
				{/if}
			</form>
		</section>

		<!-- ===== HOW IT WORKS ===== -->
		<section class="flex flex-col gap-4">
			{@render sectionHeading('How it works')}
			<ol class="m-0 p-0 list-none grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
				{#each steps as step, i}
					<li class="flex gap-3.5 items-start bg-surface border border-line rounded-xl p-[18px]">
						<span class="flex-none w-7 h-7 rounded-full bg-ink text-on-ink text-[13px] font-mono flex items-center justify-center">{i + 1}</span>
						<span class="flex flex-col gap-1">
							<span class="text-[15px] font-semibold text-ink">{step.title}</span>
							<span class="text-sm leading-normal text-body">
								{step.text}{#if step.code}&nbsp;<span class="font-mono text-[13px] text-ink">{step.code}</span>.{/if}
							</span>
						</span>
					</li>
				{/each}
			</ol>
		</section>

		<!-- ===== ENDPOINTS ===== -->
		<section class="flex flex-col gap-4">
			<div class="flex items-baseline justify-between gap-3 flex-wrap">
				{@render sectionHeading(endpoints.length > 0 ? `Your endpoints (${endpoints.length})` : 'Your endpoints')}
				<span class="text-[13px] text-faint">Saved in this browser</span>
			</div>

			{#if deleteError}
				{@render errorBanner(deleteError, () => (deleteError = null))}
			{/if}

			{#if loading}
				<div class="border border-dashed border-line-dashed rounded-xl px-6 py-8 text-center text-sm text-faint">Loading…</div>
			{:else if loadError}
				<div class="px-3.5 py-2.5 rounded-[10px] border border-danger/25 bg-danger/5 text-sm text-danger flex items-center justify-between gap-3">
					<span>{loadError}</span>
					<button type="button" onclick={loadEndpoints} class="text-xs underline hover:no-underline">Retry</button>
				</div>
			{:else if endpoints.length === 0}
				<div class="border border-dashed border-line-dashed rounded-xl px-6 py-8 text-center flex flex-col gap-1.5 items-center">
					<span class="text-[15px] font-medium text-ink">No endpoints yet</span>
					<span class="text-sm text-body">The ones you create show up here, so you can come back to them.</span>
				</div>
			{:else}
				<ul class="m-0 p-0 list-none flex flex-col gap-2">
					{#each endpoints as ep (ep.id)}
						<li class="bg-surface border border-line rounded-xl hover:border-line-strong transition-colors flex items-center gap-2 pr-2">
							<a href="/{ep.slug}" class="flex-1 min-w-0 px-4 py-3.5 flex flex-col gap-1 no-underline">
								<span class="flex items-center gap-2 flex-wrap">
									<span class="text-[15px] font-semibold text-ink truncate">{ep.name || ep.slug}</span>
									<span class="text-xs font-medium text-soft bg-chip rounded-md px-2 py-0.5">{ep.mode === 'server' ? 'Server' : 'Browser'}</span>
									{#if ep.config?.wasm_script}
										<span class="text-xs font-medium text-soft bg-chip rounded-md px-2 py-0.5">Transform</span>
									{/if}
									{#if ep.config?.forward_url}
										<span class="text-xs font-medium text-soft bg-chip rounded-md px-2 py-0.5">Forward</span>
									{/if}
								</span>
								<span class="text-[13px] text-faint flex gap-x-2 flex-wrap min-w-0">
									<span class="font-mono truncate max-w-full">/h/{ep.slug}</span>
									<span>· Created {formatTime(ep.created_at)}</span>
								</span>
							</a>

							<div class="flex items-center gap-1 shrink-0">
								{#if deleteConfirm === ep.id}
									<button
										type="button"
										onclick={() => handleDelete(ep.id)}
										class="text-[13px] font-medium px-3 min-h-9 rounded-lg bg-danger/10 text-danger hover:bg-danger/15 transition-colors"
									>
										Delete
									</button>
									<button
										type="button"
										onclick={() => (deleteConfirm = null)}
										class="text-[13px] px-3 min-h-9 rounded-lg text-soft hover:bg-chip transition-colors"
									>
										Cancel
									</button>
								{:else}
									<button
										type="button"
										onclick={() => handleCopy(ep.slug)}
										class="w-10 h-10 rounded-lg flex items-center justify-center text-faint hover:text-ink hover:bg-chip transition-colors"
										title="Copy webhook URL"
										aria-label="Copy webhook URL"
									>
										{#if copied === ep.slug}
											<Check class="w-4 h-4 text-accent" />
										{:else}
											<Copy class="w-4 h-4" />
										{/if}
									</button>
									<button
										type="button"
										onclick={() => (deleteConfirm = ep.id)}
										class="w-10 h-10 rounded-lg flex items-center justify-center text-faint hover:text-danger hover:bg-danger/10 transition-colors"
										title="Delete endpoint"
										aria-label="Delete endpoint"
									>
										<Trash2 class="w-4 h-4" />
									</button>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>

	<footer class="border-t border-line">
		<div class="max-w-[1240px] mx-auto px-4 sm:px-6 py-5 flex justify-between gap-3 flex-wrap text-[13px] text-faint">
			<p class="m-0 max-w-[760px] leading-relaxed">
				<span class="text-soft">testhooks</span> is a free, open-source webhook tester: a webhook URL to capture and inspect webhooks from Stripe, GitHub, Shopify, Slack or any service, verify Standard Webhooks signatures, transform payloads, forward webhooks to localhost from this tab, and script the reply the sender gets. Self-host it with one binary.
			</p>
			<span class="flex gap-4">
				<a href="https://sarathsp06.github.io/testhooks/" target="_blank" rel="noopener noreferrer" class="underline hover:text-accent transition-colors">Docs</a>
				<a href="https://github.com/sarathsp06/testhooks" target="_blank" rel="noopener noreferrer" class="underline hover:text-accent transition-colors">GitHub</a>
			</span>
		</div>
	</footer>
</div>
