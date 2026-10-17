<script lang="ts">
	import { KeyRound, AlertCircle, LoaderCircle } from 'lucide-svelte';
	import { auth } from '$lib/auth.svelte';
	import { verifyAuthToken } from '$lib/api';

	let token = $state('');
	let checking = $state(false);
	let localError = $state('');
	let input: HTMLInputElement | undefined = $state();

	const message = $derived(localError || auth.error);

	$effect(() => {
		if (auth.prompting) {
			token = '';
			localError = '';
			queueMicrotask(() => input?.focus());
		}
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		const candidate = token.trim();
		if (!candidate || checking) return;

		checking = true;
		localError = '';
		try {
			if (await verifyAuthToken(candidate)) {
				auth.save(candidate);
			} else {
				localError = 'That token is not valid for this server.';
				input?.select();
			}
		} catch (err) {
			localError = err instanceof Error ? err.message : 'Could not reach the server.';
		} finally {
			checking = false;
		}
	}
</script>

{#if auth.prompting}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4" role="presentation">
		<div
			role="dialog"
			aria-modal="true"
			aria-labelledby="auth-prompt-title"
			class="w-full max-w-sm rounded-lg border border-[var(--border)] bg-[var(--bg-card)] p-5 shadow-xl"
		>
			<form onsubmit={submit} class="flex flex-col gap-4">
				<div class="flex items-center gap-2">
					<KeyRound class="w-4 h-4 text-[var(--accent)]" />
					<h2 id="auth-prompt-title" class="text-sm font-semibold text-[var(--text)]">Sign in to Testhooks</h2>
				</div>

				<p class="text-xs text-[var(--text-muted)]">
					This server requires an access token. Enter the value of its <code class="text-[11px]">AUTH_TOKEN</code> setting.
					It is stored in this browser only.
				</p>

				<label class="flex flex-col gap-1">
					<span class="text-xs text-[var(--text-muted)]">Access token</span>
					<input
						bind:this={input}
						bind:value={token}
						type="password"
						autocomplete="current-password"
						spellcheck="false"
						class="w-full bg-[var(--bg)] border rounded px-3 py-1.5 text-sm font-mono text-[var(--text)] focus:outline-none focus:border-[var(--accent)] {message ? 'border-[var(--red)]' : 'border-[var(--border)]'}"
					/>
				</label>

				{#if message}
					<p class="text-xs text-[var(--red)] flex items-start gap-1.5" role="alert">
						<AlertCircle class="w-3.5 h-3.5 flex-shrink-0 mt-px" />
						{message}
					</p>
				{/if}

				<button
					type="submit"
					disabled={!token.trim() || checking}
					class="text-xs px-4 py-2 rounded bg-[var(--accent)] hover:bg-[var(--accent-hover)] text-white transition-colors flex items-center justify-center gap-1.5 disabled:opacity-50 disabled:cursor-not-allowed"
				>
					{#if checking}
						<LoaderCircle class="w-3.5 h-3.5 animate-spin" />
						Checking…
					{:else}
						Sign in
					{/if}
				</button>
			</form>
		</div>
	</div>
{/if}
