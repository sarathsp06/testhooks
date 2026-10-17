<script lang="ts">
	import '../app.css';
	import { Webhook, Sun, Moon, Monitor, LogOut, BookOpen } from 'lucide-svelte';
	import { ModeWatcher, toggleMode, mode } from 'mode-watcher';
	import AuthPrompt from '$lib/components/AuthPrompt.svelte';
	import { auth } from '$lib/auth.svelte';

	function signOut() {
		auth.signOut();
		// Drop data loaded with the old token and let the next request prompt again.
		location.reload();
	}

	let { children } = $props();
</script>

<ModeWatcher />
<AuthPrompt />

<div class="min-h-screen flex flex-col">
	<header class="border-b border-[var(--border)] px-4 py-3 flex items-center justify-between">
		<a href="/" class="flex items-center gap-2 text-[var(--text)] hover:text-[var(--accent)] transition-colors">
			<Webhook class="w-5 h-5" />
			<span class="font-semibold text-lg">Testhooks</span>
		</a>
		<div class="flex items-center gap-2">
			<a
				href="https://sarathsp06.github.io/testhooks/"
				target="_blank"
				rel="noopener noreferrer"
				class="flex items-center gap-1.5 text-sm px-2.5 py-1.5 rounded-md text-[var(--text-muted)] hover:text-[var(--text)] hover:bg-[var(--bg-hover)] transition-colors"
			>
				<BookOpen class="w-4 h-4" />
				Docs
			</a>
			<a
				href="https://github.com/sarathsp06/testhooks"
				target="_blank"
				rel="noopener noreferrer"
				class="flex items-center gap-1.5 text-sm px-2.5 py-1.5 rounded-md border border-[var(--border)] text-[var(--text)] hover:bg-[var(--bg-hover)] transition-colors"
			>
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 19c-4.3 1.4-4.3-2.5-6-3m12 5v-3.5c0-1 .1-1.4-.5-2 2.8-.3 5.5-1.4 5.5-6a4.6 4.6 0 0 0-1.3-3.2 4.2 4.2 0 0 0-.1-3.2s-1.1-.3-3.5 1.3a12.3 12.3 0 0 0-6.2 0C6.5 2.8 5.4 3.1 5.4 3.1a4.2 4.2 0 0 0-.1 3.2A4.6 4.6 0 0 0 4 9.5c0 4.6 2.7 5.7 5.5 6-.6.6-.6 1.2-.5 2V21"></path></svg>
				GitHub
			</a>
			{#if auth.signedIn}
				<button
					onclick={signOut}
					class="p-1.5 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
					title="Sign out (forget the access token)"
					aria-label="Sign out"
				>
					<LogOut class="w-4 h-4" />
				</button>
			{/if}
			<button
				onclick={toggleMode}
				class="p-1.5 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
				title="Toggle theme"
			>
				{#if mode.current === 'dark'}
					<Sun class="w-4 h-4" />
				{:else if mode.current === 'light'}
					<Moon class="w-4 h-4" />
				{:else}
					<Monitor class="w-4 h-4" />
				{/if}
			</button>
		</div>
	</header>

	<main class="flex-1">
		{@render children()}
	</main>
</div>
