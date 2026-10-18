<script lang="ts">
	import '../app.css';
	import { Sun, Moon, LogOut } from 'lucide-svelte';
	import { ModeWatcher, toggleMode, mode } from 'mode-watcher';
	import AuthPrompt from '$lib/components/AuthPrompt.svelte';
	import { auth } from '$lib/auth.svelte';
	import { page } from '$app/state';

	function signOut() {
		auth.signOut();
		// Drop data loaded with the old token and let the next request prompt again.
		location.reload();
	}

	let { children } = $props();

	// Endpoint screens draw their own header (breadcrumb, URL, editor actions).
	const ownHeader = $derived(page.route.id === '/[slug]');
</script>

<ModeWatcher />
<AuthPrompt />

<div class="min-h-screen flex flex-col">
	<!-- 56px + 1px border: the endpoint page sizes itself to 100vh - 57px. -->
	{#if !ownHeader}
	<header class="border-b border-line bg-page">
		<div class="max-w-[1240px] mx-auto px-4 sm:px-6 h-14 flex items-center justify-between gap-3">
			<a href="/" class="flex items-center gap-2.5 text-ink no-underline">
				<span class="w-[26px] h-[26px] rounded-[7px] bg-[#111613] dark:bg-[#1B231F] flex items-center justify-center">
					<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="#5BD08F" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 12h5l2-5 3 10 2-5h4"></path></svg>
				</span>
				<span class="font-semibold text-base tracking-[-0.01em]">testhooks</span>
			</a>
			<nav class="flex items-center gap-1">
				<a
					href="https://sarathsp06.github.io/testhooks/"
					target="_blank"
					rel="noopener noreferrer"
					class="text-sm text-soft hover:text-accent px-2 sm:px-3 py-2.5 transition-colors"
				>
					Docs
				</a>
				<a
					href="https://github.com/sarathsp06/testhooks"
					target="_blank"
					rel="noopener noreferrer"
					aria-label="GitHub"
					class="text-sm font-medium text-ink hover:text-accent px-2.5 sm:px-3 py-2 border border-line-strong rounded-lg bg-surface flex items-center gap-2 transition-colors"
				>
					<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 19c-4.3 1.4-4.3-2.5-6-3m12 5v-3.5c0-1 .1-1.4-.5-2 2.8-.3 5.5-1.4 5.5-6a4.6 4.6 0 0 0-1.3-3.2 4.2 4.2 0 0 0-.1-3.2s-1.1-.3-3.5 1.3a12.3 12.3 0 0 0-6.2 0C6.5 2.8 5.4 3.1 5.4 3.1a4.2 4.2 0 0 0-.1 3.2A4.6 4.6 0 0 0 4 9.5c0 4.6 2.7 5.7 5.5 6-.6.6-.6 1.2-.5 2V21"></path></svg>
					<span class="hidden min-[400px]:inline">GitHub</span>
				</a>
				<button
					type="button"
					onclick={toggleMode}
					class="w-11 h-11 rounded-lg flex items-center justify-center text-soft hover:text-ink hover:bg-chip transition-colors"
					aria-label={mode.current === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
					title={mode.current === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
				>
					{#if mode.current === 'dark'}
						<Sun class="w-[17px] h-[17px]" />
					{:else}
						<Moon class="w-[17px] h-[17px]" />
					{/if}
				</button>
				{#if auth.signedIn}
					<button
						type="button"
						onclick={signOut}
						class="w-11 h-11 rounded-lg flex items-center justify-center text-soft hover:text-ink hover:bg-chip transition-colors"
						title="Sign out (forget the access token)"
						aria-label="Sign out"
					>
						<LogOut class="w-4 h-4" />
					</button>
				{/if}
			</nav>
		</div>
	</header>
	{/if}

	<main class="flex-1 flex flex-col">
		{@render children()}
	</main>
</div>
