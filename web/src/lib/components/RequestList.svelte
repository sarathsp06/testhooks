<script lang="ts">
	import type { CapturedRequest } from '$lib/types';
	import { methodColor, formatTime } from '$lib/utils';
	import SearchFilter from './SearchFilter.svelte';

	let {
		requests,
		selectedId,
		total,
		onSelect
	}: {
		requests: CapturedRequest[];
		selectedId: string | null;
		total: number;
		onSelect: (id: string) => void;
	} = $props();

	let searchQuery = $state('');
	let methodFilter = $state('');

	const filteredRequests = $derived.by(() => {
		let result = requests;
		if (methodFilter) {
			result = result.filter((r) => r.method === methodFilter);
		}
		if (searchQuery) {
			const q = searchQuery.toLowerCase();
			result = result.filter(
				(r) =>
					r.path.toLowerCase().includes(q) ||
					r.content_type?.toLowerCase().includes(q) ||
					r.ip?.toLowerCase().includes(q) ||
					(r.body && typeof r.body === 'string' && r.body.toLowerCase().includes(q))
			);
		}
		return result;
	});
</script>

<div class="flex flex-col h-full">
	<div class="px-3 py-2 border-b border-[var(--border)] flex items-center justify-between">
		<span class="text-xs font-medium text-[var(--text-muted)]">
			Requests
		</span>
		<span class="text-xs text-[var(--text-muted)]">
			{#if searchQuery || methodFilter}
				{filteredRequests.length} / {total}
			{:else}
				{total}
			{/if}
		</span>
	</div>

	<div class="border-b border-[var(--border)]">
		<SearchFilter
			{searchQuery}
			{methodFilter}
			onSearchChange={(q) => (searchQuery = q)}
			onMethodFilterChange={(m) => (methodFilter = m)}
		/>
	</div>

	{#if filteredRequests.length === 0}
		<div class="flex-1 flex items-center justify-center px-4">
			<p class="text-sm text-[var(--text-muted)] text-center">
				{#if requests.length === 0}
					Waiting for requests...
				{:else}
					No requests match your filter
				{/if}
			</p>
		</div>
	{:else}
		<div class="flex-1 overflow-y-auto">
			{#each filteredRequests as req (req.id)}
				<button
					class="w-full text-left px-3 py-2.5 border-b border-[var(--border)] hover:bg-[var(--bg-hover)] transition-colors {selectedId === req.id ? 'bg-[var(--bg-hover)] border-l-2 border-l-[var(--accent)]' : ''}"
					onclick={() => onSelect(req.id)}
				>
					<div class="flex items-center gap-2">
						<span class="text-xs font-mono font-semibold {methodColor(req.method)} w-12 flex-shrink-0">
							{req.method}
						</span>
						<span class="text-xs text-[var(--text)] truncate flex-1 font-mono">
							{req.path}
						</span>
						<span class="text-[10px] text-[var(--text-muted)] flex-shrink-0">
							{formatTime(req.created_at)}
						</span>
					</div>
					<div class="flex items-center gap-2 mt-1">
						{#if req.content_type}
							<span class="text-[10px] text-[var(--text-muted)] truncate">
								{req.content_type.split(';')[0]}
							</span>
						{/if}
						{#if req.size > 0}
							<span class="text-[10px] text-[var(--text-muted)]">
								{req.size}B
							</span>
						{/if}
						<span class="text-[10px] text-[var(--text-muted)]">
							{req.ip}
						</span>
					</div>
				</button>
			{/each}
		</div>
	{/if}
</div>
