<script lang="ts">
	import { HelpCircle } from 'lucide-svelte';

	let { text }: { text: string } = $props();
</script>

<span class="relative inline-flex group ml-1 align-middle">
	<HelpCircle class="w-3 h-3 text-[var(--text-muted)] opacity-50 group-hover:opacity-100 cursor-help transition-opacity" />
	<span
		class="absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 px-2.5 py-1.5 text-[10px] leading-snug text-[var(--text)] bg-[var(--bg-card)] border border-[var(--border)] rounded shadow-lg whitespace-normal w-56 opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-50 pointer-events-none"
	>
		{text}
	</span>
</span>
