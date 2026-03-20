<script lang="ts">
	import '../app.css';
	import { Webhook, Sun, Moon, Monitor } from 'lucide-svelte';
	import { ModeWatcher, toggleMode, mode } from 'mode-watcher';

	let { children } = $props();
</script>

<ModeWatcher />

<div class="min-h-screen flex flex-col">
	<header class="border-b border-[var(--border)] px-4 py-3 flex items-center justify-between">
		<a href="/" class="flex items-center gap-2 text-[var(--text)] hover:text-[var(--accent)] transition-colors">
			<Webhook class="w-5 h-5" />
			<span class="font-semibold text-lg">Testhooks</span>
		</a>
		<div class="flex items-center gap-3">
			<span class="text-xs text-[var(--text-muted)]">webhook inspector</span>
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
