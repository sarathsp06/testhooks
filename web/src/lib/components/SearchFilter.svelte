<script lang="ts">
	import { Search, X, Filter } from 'lucide-svelte';

	let {
		searchQuery = '',
		methodFilter = '',
		onSearchChange,
		onMethodFilterChange
	}: {
		searchQuery?: string;
		methodFilter?: string;
		onSearchChange: (query: string) => void;
		onMethodFilterChange: (method: string) => void;
	} = $props();

	let localQuery = $state(searchQuery);
	let showFilters = $state(false);

	const methods = ['', 'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];

	function handleInput(e: Event) {
		const target = e.target as HTMLInputElement;
		localQuery = target.value;
		onSearchChange(localQuery);
	}

	function clearSearch() {
		localQuery = '';
		onSearchChange('');
	}
</script>

<div class="flex flex-col gap-1.5">
	<div class="flex items-center gap-1.5 px-3 py-1.5">
		<Search class="w-3.5 h-3.5 text-[var(--text-muted)] flex-shrink-0" />
		<input
			type="text"
			value={localQuery}
			oninput={handleInput}
			placeholder="Search requests..."
			class="flex-1 bg-transparent text-xs text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none"
		/>
		{#if localQuery}
			<button
				onclick={clearSearch}
				class="p-0.5 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)]"
			>
				<X class="w-3 h-3" />
			</button>
		{/if}
		<button
			onclick={() => (showFilters = !showFilters)}
			class="p-0.5 rounded hover:bg-[var(--bg-hover)] {methodFilter ? 'text-[var(--accent)]' : 'text-[var(--text-muted)]'}"
			title="Filter by method"
		>
			<Filter class="w-3.5 h-3.5" />
		</button>
	</div>

	{#if showFilters}
		<div class="flex items-center gap-1 px-3 pb-1.5 flex-wrap">
			{#each methods as m}
				<button
					class="text-[10px] px-1.5 py-0.5 rounded transition-colors {methodFilter === m
						? 'bg-[var(--accent)] text-white'
						: 'bg-[var(--bg)] text-[var(--text-muted)] hover:text-[var(--text)]'}"
					onclick={() => onMethodFilterChange(m)}
				>
					{m || 'All'}
				</button>
			{/each}
		</div>
	{/if}
</div>
