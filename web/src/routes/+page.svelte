<script lang="ts">
	import { goto } from '$app/navigation';
	import { createEndpoint, listEndpoints, deleteEndpoint } from '$lib/api';
	import { getWebhookURL, formatTime, copyToClipboard } from '$lib/utils';
	import { SUPPORTED_LANGUAGES, DEFAULT_SCRIPTS, DEFAULT_RESPONSE_SCRIPTS, type TransformLanguage } from '$lib/wasm';
	import CodeEditor from '$lib/components/CodeEditor.svelte';
	import {
		Plus,
		Trash2,
		Copy,
		ExternalLink,
		Server,
		Monitor,
		ChevronDown,
		ChevronRight,
		Send,
		Code2,
		MessageSquare,
		Webhook,
		X,
		Zap,
		Shield,
		Eye,
		Globe,
		ArrowRight,
		Terminal,
		GitBranch,
		Unplug,
		Laptop,
		CircleCheck
	} from 'lucide-svelte';
	import type { Endpoint, EndpointConfig } from '$lib/types';

	let endpoints = $state<Endpoint[]>([]);
	let loading = $state(true);
	let creating = $state(false);
	let copied = $state<string | null>(null);
	let deleteConfirm = $state<string | null>(null);
	let createError = $state<string | null>(null);
	let loadError = $state<string | null>(null);
	let deleteError = $state<string | null>(null);
	let forwardUrlError = $state<string | null>(null);

	// Creation form state
	let newName = $state('');
	let newMode = $state<'server' | 'browser'>('server');
	let showAdvanced = $state(false);

	// Advanced: Forward URL (single URL + mode)
	let newForwardUrl = $state('');
	let newForwardMode = $state<'async' | 'sync'>('async');

	// Advanced: Transform
	let newTransformEnabled = $state(false);
	let newTransformLanguage = $state<TransformLanguage>('javascript');
	let newTransformScript = $state('');

	// Advanced: Custom Response (script-based)
	let newCustomResponseEnabled = $state(false);
	let newCustomResponseLanguage = $state<TransformLanguage>('javascript');
	let newCustomResponseScript = $state('');

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
		newMode = 'server';
		showAdvanced = false;
		newForwardUrl = '';
		newForwardMode = 'async';
		newTransformEnabled = false;
		newTransformLanguage = 'javascript';
		newTransformScript = '';
		newCustomResponseEnabled = false;
		newCustomResponseLanguage = 'javascript';
		newCustomResponseScript = '';
	}

	async function handleCreate() {
		creating = true;
		createError = null;
		try {
			const config: EndpointConfig = {};

			if (newForwardUrl.trim()) {
				config.forward_url = newForwardUrl.trim();
				config.forward_mode = newForwardMode;
			}
			if (newTransformEnabled && newTransformScript.trim()) {
				config.wasm_script = newTransformScript;
				config.transform_language = newTransformLanguage;
			}
			if (newCustomResponseEnabled && newCustomResponseScript.trim()) {
				config.custom_response = {
					enabled: true,
					script: newCustomResponseScript,
					language: newCustomResponseLanguage
				};
			}

			const hasConfig = Object.keys(config).length > 0;

			const ep = await createEndpoint({
				name: newName || undefined,
				mode: newMode,
				config: hasConfig ? config : undefined
			});
			resetForm();
			goto(`/${ep.slug}`);
		} catch (err) {
			createError = err instanceof Error ? err.message : 'Failed to create endpoint. Please try again.';
		} finally {
			creating = false;
		}
	}

	function validateForwardUrl(): boolean {
		const url = newForwardUrl.trim();
		if (!url) return true;
		try {
			new URL(url);
			forwardUrlError = null;
			return true;
		} catch {
			forwardUrlError = 'Invalid URL. Must include protocol (e.g. https://example.com)';
			return false;
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

	function scrollToCreate() {
		document.getElementById('create-section')?.scrollIntoView({ behavior: 'smooth' });
	}

	$effect(() => {
		loadEndpoints();
	});

	// Pipeline steps for the animated diagram
	const pipelineSteps = [
		{ label: 'Capture', icon: 'webhook', color: 'blue', desc: 'Receive any HTTP request' },
		{ label: 'Store', icon: 'database', color: 'purple', desc: 'Persist to PostgreSQL' },
		{ label: 'Transform', icon: 'code', color: 'orange', desc: 'JS/Lua/Jsonnet scripts' },
		{ label: 'Forward', icon: 'send', color: 'green', desc: 'Relay to your services' },
		{ label: 'Respond', icon: 'reply', color: 'teal', desc: 'Custom HTTP response' }
	];

	const features = [
		{
			icon: Globe,
			title: 'Localhost Friendly',
			description: 'Browser mode forwards via fetch() — reach localhost, Docker, anything your machine can see. No CLI, no daemon.'
		},
		{
			icon: Shield,
			title: 'Privacy-first Mode',
			description: 'Browser mode: zero server storage. Payloads never touch disk. GDPR-friendly, ideal for sensitive data.'
		},
		{
			icon: Zap,
			title: 'Real-time Streaming',
			description: 'WebSocket-powered live view. See every request the instant it arrives — headers, body, timing.'
		},
		{
			icon: Code2,
			title: 'Script Transforms',
			description: 'JavaScript, Lua, or Jsonnet scripts transform payloads before forwarding. Runs in WASM — sandboxed and fast.'
		},
		{
			icon: Send,
			title: 'Smart Forwarding',
			description: 'Forward to any URL with sync or async mode. Sync mode captures the response for your handler scripts.'
		},
		{
			icon: Eye,
			title: 'Full Inspection',
			description: 'Headers, query params, body with JSON/XML syntax highlighting. Copy as cURL, replay, export as JSON/CSV.'
		}
	];

	const techStack = [
		{ name: 'Go', color: '#00ADD8' },
		{ name: 'Svelte 5', color: '#FF3E00' },
		{ name: 'WebSocket', color: '#8B5CF6' },
		{ name: 'WASM', color: '#F97316' },
		{ name: 'Postgres', color: '#336791' }
	];
</script>

<div class="min-h-screen">
	<!-- ===== HERO SECTION ===== -->
	<section class="relative min-h-[85vh] flex items-center justify-center overflow-hidden">
		<!-- Dark gradient background matching logo aesthetic -->
		<div class="absolute inset-0 bg-gradient-to-br from-[#0d1117] via-[#0d1117] to-[#161b22]"></div>
		<!-- Subtle glow behind hero -->
		<div class="absolute top-1/3 left-1/2 -translate-x-1/2 w-[500px] h-[500px] rounded-full blur-[120px] bg-[#00ADD8]/8"></div>

		<div class="max-w-[1200px] mx-auto px-6 relative z-10">
			<div class="flex flex-col items-center text-center gap-8">
				<!-- Badge -->
				<div class="inline-flex items-center gap-2 px-4 py-2 rounded-full text-sm bg-[#00ADD8]/10 border border-[#00ADD8]/20">
					<Zap class="w-4 h-4 text-[#00ADD8]" />
					<span class="text-[#00ADD8] font-medium">Self-hostable Webhook Inspector</span>
				</div>

				<!-- Hook Logomark -->
				<div class="relative">
					<!-- Tunnel glow -->
					<div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-32 h-32 rounded-full bg-[#00ADD8]/10 blur-2xl"></div>
					<svg class="w-28 h-28 relative" viewBox="0 0 120 120" fill="none" xmlns="http://www.w3.org/2000/svg">
						<defs>
							<linearGradient id="heroHookGrad" x1="0%" y1="0%" x2="100%" y2="100%">
								<stop offset="0%" stop-color="#00ADD8"/>
								<stop offset="100%" stop-color="#0086A8"/>
							</linearGradient>
							<style>
								@keyframes heroFlowDot1 { 0%,100% { opacity: 0.3; } 50% { opacity: 1; } }
								@keyframes heroFlowDot2 { 0%,100% { opacity: 1; } 50% { opacity: 0.3; } }
								@keyframes heroFlowDot3 { 0%,100% { opacity: 0.5; } 33% { opacity: 1; } 66% { opacity: 0.3; } }
								.hero-dot1 { animation: heroFlowDot1 2s ease-in-out infinite; }
								.hero-dot2 { animation: heroFlowDot2 2s ease-in-out infinite; }
								.hero-dot3 { animation: heroFlowDot3 2s ease-in-out infinite; }
							</style>
						</defs>
						<!-- The Hook -->
						<path d="M 8 38
							 L 48 38
							 C 80 38, 105 50, 105 78
							 C 105 108, 82 120, 60 120
							 C 40 120, 26 108, 26 92
							 C 26 78, 36 70, 48 70
							 C 58 70, 64 78, 64 86
							 C 64 92, 60 96, 54 96"
							stroke="url(#heroHookGrad)" stroke-width="7" stroke-linecap="round" fill="none"/>
						<!-- Arrow tip -->
						<path d="M 58 88 L 54 96 L 48 90" stroke="url(#heroHookGrad)" stroke-width="5" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
						<!-- Signal dots -->
						<circle cx="-18" cy="38" r="3.5" fill="#F97316" class="hero-dot1"/>
						<circle cx="-34" cy="38" r="2.8" fill="#F97316" class="hero-dot2" opacity="0.6"/>
						<circle cx="-48" cy="38" r="2"   fill="#F97316" class="hero-dot3" opacity="0.3"/>
						<!-- Tunnel entrance -->
						<line x1="2" y1="30" x2="2" y2="46" stroke="#00ADD8" stroke-width="2.5" stroke-linecap="round" opacity="0.5"/>
						<!-- Broadcast arcs -->
						<path d="M 76 120 Q 88 130, 78 142" stroke="#F97316" stroke-width="2" stroke-linecap="round" fill="none" opacity="0.5" class="hero-dot1"/>
						<path d="M 82 116 Q 98 130, 84 148" stroke="#F97316" stroke-width="1.5" stroke-linecap="round" fill="none" opacity="0.3" class="hero-dot2"/>
					</svg>
				</div>

				<!-- Wordmark — "TEST" muted + "HOOKS" bold, matching logo -->
				<h1 class="font-fira tracking-wider">
					<span class="text-4xl md:text-5xl lg:text-6xl font-normal text-[#8b949e]">TEST</span>
					<span class="text-4xl md:text-5xl lg:text-6xl font-bold text-[#e6edf3]">HOOKS</span>
				</h1>

				<!-- Tagline — matching logo exactly -->
				<p class="text-base md:text-lg leading-relaxed max-w-2xl text-[#8b949e]">
					Capture, inspect, transform, and forward webhooks in real time.
					<br />
					<span class="text-sm text-[#8b949e]/70">
						A lightweight <a href="https://webhook.site" target="_blank" class="underline underline-offset-2 hover:text-[#00ADD8] transition-colors">webhook.site</a> replacement you own. Browser mode replaces ngrok with zero setup.
					</span>
				</p>

				<!-- CTA buttons -->
				<div class="flex flex-col sm:flex-row gap-4">
					<button
						onclick={scrollToCreate}
						class="inline-flex items-center justify-center gap-2 px-7 py-3 text-base font-medium rounded-xl bg-[#00ADD8] text-white hover:bg-[#0086A8] transition-all shadow-lg shadow-[#00ADD8]/20"
					>
						<Webhook class="w-5 h-5" />
						Create Endpoint
					</button>
					<a
						href="https://github.com/sarathsp06/testhooks"
						target="_blank"
						class="inline-flex items-center justify-center gap-2 px-7 py-3 text-base font-medium rounded-xl border border-[#30363d] text-[#e6edf3] hover:bg-[#161b22] transition-all"
					>
						<GitBranch class="w-5 h-5" />
						View on GitHub
					</a>
				</div>

				<!-- Architecture mini-diagram — matching logo -->
				<div class="pt-6 border-t border-[#30363d] w-full max-w-2xl">
					<div class="flex items-center justify-center gap-1 font-fira text-xs md:text-sm flex-wrap">
						<span class="text-[#484f58]">webhook</span>
						<span class="text-[#6e7681]">--&gt;</span>
						<span class="text-[#00ADD8]">/h/:slug</span>
						<span class="text-[#6e7681]">--&gt;</span>
						<span class="text-[#8B5CF6]">WS</span>
						<span class="text-[#6e7681]">--&gt;</span>
						<span class="text-[#e6edf3]">browser</span>
						<span class="text-[#6e7681]">--&gt;</span>
						<span class="text-[#F97316]">localhost</span>
					</div>
				</div>

				<!-- Tech stack pills — matching logo colors -->
				<div class="flex flex-wrap justify-center gap-2">
					{#each techStack as tech}
						<span
							class="px-3 py-1.5 text-xs font-semibold rounded-full border"
							style="color: {tech.color}; background-color: {tech.color}12; border-color: {tech.color}4D;"
						>
							{tech.name}
						</span>
					{/each}
				</div>
			</div>
		</div>
	</section>

	<!-- ===== PIPELINE DIAGRAM ===== -->
	<section class="py-20 bg-[var(--bg)]">
		<div class="max-w-[1200px] mx-auto px-6">
			<div class="text-center mb-14">
				<div class="inline-flex items-center gap-2 px-4 py-2 rounded-full text-sm mb-6 bg-blue-50 dark:bg-blue-500/10 border border-blue-200/60 dark:border-blue-500/20">
					<span class="text-[var(--accent)] font-medium">How It Works</span>
				</div>
				<h2 class="text-3xl md:text-4xl font-bold font-fira mb-4">
					The <span class="bg-gradient-to-r from-[var(--accent)] to-purple-500 bg-clip-text text-transparent">Pipeline</span>
				</h2>
				<p class="text-lg max-w-3xl mx-auto text-[var(--text-muted)]">
					Every webhook flows through a composable pipeline. Enable only what you need.
				</p>
			</div>

			<!-- Pipeline visual -->
			<div class="relative max-w-4xl mx-auto">
				<!-- Connection line (desktop) -->
				<div class="hidden md:block absolute left-0 right-0 h-0.5 opacity-20" style="top: 36px; background: linear-gradient(to right, #60a5fa, #a78bfa, #fb923c, #4ade80, #2dd4bf);"></div>

				<div class="grid grid-cols-1 md:grid-cols-5 gap-4 md:gap-3">
					{#each pipelineSteps as step, i}
						<div class="flex flex-col items-center text-center group">
							<!-- Step circle -->
							<div class="relative w-[72px] h-[72px] rounded-2xl flex items-center justify-center mb-4 transition-all duration-300 group-hover:-translate-y-1 group-hover:shadow-lg
								{step.color === 'blue' ? 'bg-blue-50 dark:bg-blue-500/10 border border-blue-200 dark:border-blue-500/30 group-hover:shadow-blue-500/10' : ''}
								{step.color === 'purple' ? 'bg-purple-50 dark:bg-purple-500/10 border border-purple-200 dark:border-purple-500/30 group-hover:shadow-purple-500/10' : ''}
								{step.color === 'orange' ? 'bg-orange-50 dark:bg-orange-500/10 border border-orange-200 dark:border-orange-500/30 group-hover:shadow-orange-500/10' : ''}
								{step.color === 'green' ? 'bg-green-50 dark:bg-green-500/10 border border-green-200 dark:border-green-500/30 group-hover:shadow-green-500/10' : ''}
								{step.color === 'teal' ? 'bg-teal-50 dark:bg-teal-500/10 border border-teal-200 dark:border-teal-500/30 group-hover:shadow-teal-500/10' : ''}
							">
								{#if step.icon === 'webhook'}
									<Webhook class="w-6 h-6 text-blue-500" />
								{:else if step.icon === 'database'}
									<svg class="w-6 h-6 text-purple-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
										<ellipse cx="12" cy="5" rx="9" ry="3" />
										<path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3" />
										<path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
									</svg>
								{:else if step.icon === 'code'}
									<Code2 class="w-6 h-6 text-orange-500" />
								{:else if step.icon === 'send'}
									<Send class="w-6 h-6 text-green-500" />
								{:else if step.icon === 'reply'}
									<MessageSquare class="w-6 h-6 text-teal-500" />
								{/if}

								<!-- Arrow connector (mobile: vertical, desktop: hidden since line) -->
								{#if i < pipelineSteps.length - 1}
									<div class="md:hidden absolute -bottom-5 left-1/2 -translate-x-1/2 text-[var(--text-muted)]">
										<ArrowRight class="w-4 h-4 rotate-90" />
									</div>
								{/if}
							</div>

							<!-- Label -->
							<h3 class="text-sm font-semibold font-fira mb-1 text-[var(--text)]">{step.label}</h3>
							<p class="text-xs text-[var(--text-muted)] leading-relaxed">{step.desc}</p>
						</div>
					{/each}
				</div>

				<!-- Example flow card -->
				<div class="mt-12 p-6 rounded-xl border border-[var(--border)] bg-[var(--bg-card)]">
					<div class="flex items-center gap-2 mb-4">
						<Terminal class="w-4 h-4 text-[var(--text-muted)]" />
						<span class="text-xs font-fira font-medium text-[var(--text-muted)] uppercase tracking-wider">Example Pipeline</span>
					</div>
					<div class="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm">
						<div class="p-4 rounded-lg bg-[var(--bg)] border border-[var(--border)]">
							<div class="flex items-center gap-2 mb-2">
								<span class="text-xs font-fira font-bold text-blue-500">1. CAPTURE</span>
							</div>
							<code class="text-xs text-[var(--text-muted)] font-fira block">
								POST /h/abc123<br />
								{`{"event": "order.created"}`}
							</code>
						</div>
						<div class="p-4 rounded-lg bg-[var(--bg)] border border-[var(--border)]">
							<div class="flex items-center gap-2 mb-2">
								<span class="text-xs font-fira font-bold text-orange-500">2. TRANSFORM</span>
							</div>
							<code class="text-xs text-[var(--text-muted)] font-fira block">
								function transform(req) {"{"}<br />
								&nbsp;&nbsp;req.body.ts = Date.now();<br />
								&nbsp;&nbsp;return req;<br />
								{"}"}
							</code>
						</div>
						<div class="p-4 rounded-lg bg-[var(--bg)] border border-[var(--border)]">
							<div class="flex items-center gap-2 mb-2">
								<span class="text-xs font-fira font-bold text-green-500">3. FORWARD</span>
							</div>
							<code class="text-xs text-[var(--text-muted)] font-fira block">
								POST https://api.yourapp.com<br />
								{`{"event": "order.created",`}<br />
								{` "ts": 1711234567890}`}
							</code>
						</div>
					</div>
				</div>
			</div>
		</div>
	</section>

	<!-- ===== FEATURES GRID ===== -->
	<section class="py-20 bg-[var(--bg-card)]">
		<div class="max-w-[1200px] mx-auto px-6">
			<div class="text-center mb-14">
				<div class="inline-flex items-center gap-2 px-4 py-2 rounded-full text-sm mb-6 bg-blue-50 dark:bg-blue-500/10 border border-blue-200/60 dark:border-blue-500/20">
					<span class="text-[var(--accent)] font-medium">Core Features</span>
				</div>
				<h2 class="text-3xl md:text-4xl font-bold font-fira mb-4">
					Everything You Need for <span class="bg-gradient-to-r from-[var(--accent)] to-purple-500 bg-clip-text text-transparent">Webhook Development</span>
				</h2>
				<p class="text-lg max-w-3xl mx-auto text-[var(--text-muted)]">
					From live inspection to programmable transforms — one tool for the full webhook lifecycle.
				</p>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
				{#each features as feature}
					<div class="group p-8 rounded-xl border border-[var(--border)] bg-[var(--bg)] transition-all duration-300 hover:border-blue-300/60 dark:hover:border-blue-500/30 hover:shadow-lg hover:shadow-blue-500/5 hover:-translate-y-1">
						<div class="w-12 h-12 flex items-center justify-center rounded-xl mb-6 bg-blue-50 dark:bg-blue-500/10">
							<feature.icon class="w-5 h-5 text-[var(--accent)]" />
						</div>
						<h3 class="text-xl font-semibold mb-3 font-fira text-[var(--text)]">{feature.title}</h3>
						<p class="leading-relaxed text-[var(--text-muted)] text-[0.9375rem]">{feature.description}</p>
					</div>
				{/each}
			</div>
		</div>
	</section>

	<!-- ===== TWO MODES SECTION ===== -->
	<section class="py-20 bg-[var(--bg)]">
		<div class="max-w-[1200px] mx-auto px-6">
			<div class="text-center mb-14">
				<div class="inline-flex items-center gap-2 px-4 py-2 rounded-full text-sm mb-6 bg-blue-50 dark:bg-blue-500/10 border border-blue-200/60 dark:border-blue-500/20">
					<span class="text-[var(--accent)] font-medium">Two Modes</span>
				</div>
				<h2 class="text-3xl md:text-4xl font-bold font-fira mb-4">
					Choose Your <span class="bg-gradient-to-r from-[var(--accent)] to-purple-500 bg-clip-text text-transparent">Trade-off</span>
				</h2>
				<p class="text-lg max-w-3xl mx-auto text-[var(--text-muted)]">
					Server mode for production reliability, Browser mode for privacy and local testing.
				</p>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl mx-auto">
				<!-- Server Mode -->
				<div class="relative p-8 rounded-xl border-2 border-blue-200 dark:border-blue-500/30 bg-blue-50/30 dark:bg-blue-500/5 overflow-hidden">
					<div class="absolute top-4 right-4 text-[10px] px-2 py-1 rounded bg-blue-500/10 text-[var(--accent)] font-medium">DEFAULT</div>
					<div class="flex items-center gap-3 mb-4">
						<div class="w-10 h-10 flex items-center justify-center rounded-lg bg-blue-100 dark:bg-blue-500/20">
							<Server class="w-5 h-5 text-[var(--accent)]" />
						</div>
						<h3 class="text-xl font-semibold font-fira text-[var(--text)]">Server Mode</h3>
					</div>
					<p class="text-[var(--text-muted)] text-[0.9375rem] mb-5 leading-relaxed">
						Requests are stored in PostgreSQL. Transforms and forwarding run server-side, always-on even without a browser. Ideal for production webhooks.
					</p>
					<div class="flex flex-wrap gap-2">
						{#each ['Persistent storage', 'Always-on', 'Server-side WASM', 'Retry with backoff'] as tag}
							<span class="px-2.5 py-1 text-xs font-fira rounded bg-blue-100 dark:bg-blue-500/10 text-[var(--accent)]">{tag}</span>
						{/each}
					</div>
				</div>

				<!-- Browser Mode -->
				<div class="relative p-8 rounded-xl border-2 border-purple-200 dark:border-purple-500/30 bg-purple-50/30 dark:bg-purple-500/5 overflow-hidden">
					<div class="absolute top-4 right-4 text-[10px] px-2 py-1 rounded bg-purple-500/10 text-[var(--purple)] font-medium">TUNNEL KILLER</div>
					<div class="flex items-center gap-3 mb-4">
						<div class="w-10 h-10 flex items-center justify-center rounded-lg bg-purple-100 dark:bg-purple-500/20">
							<Monitor class="w-5 h-5 text-[var(--purple)]" />
						</div>
						<h3 class="text-xl font-semibold font-fira text-[var(--text)]">Browser Mode</h3>
					</div>
					<p class="text-[var(--text-muted)] text-[0.9375rem] mb-5 leading-relaxed">
						Your tunnel replacement for local webhook testing. Webhooks hit Testhooks, stream to your browser via WebSocket, and <code class="text-[var(--purple)] bg-purple-500/5 px-1 rounded">fetch()</code> delivers them to localhost. No CLI, no daemon, no port forwarding. Zero server storage.
					</p>
					<div class="flex flex-wrap gap-2">
						{#each ['Replaces ngrok', 'Reach localhost', 'Zero storage', 'No CLI needed'] as tag}
							<span class="px-2.5 py-1 text-xs font-fira rounded bg-purple-100 dark:bg-purple-500/10 text-[var(--purple)]">{tag}</span>
						{/each}
					</div>
				</div>
			</div>
		</div>
	</section>

	<!-- ===== REPLACE YOUR HTTP TUNNEL SECTION ===== -->
	<section class="py-20 bg-[var(--bg-card)]">
		<div class="max-w-[1200px] mx-auto px-6">
			<div class="text-center mb-14">
				<div class="inline-flex items-center gap-2 px-4 py-2 rounded-full text-sm mb-6 bg-purple-50 dark:bg-purple-500/10 border border-purple-200/60 dark:border-purple-500/20">
					<Unplug class="w-4 h-4 text-[var(--purple)]" />
					<span class="text-[var(--purple)] font-medium">No CLI, No Daemon</span>
				</div>
				<h2 class="text-3xl md:text-4xl font-bold font-fira mb-4">
					Replace <span class="bg-gradient-to-r from-purple-500 to-[var(--accent)] bg-clip-text text-transparent">ngrok</span> for Webhook Testing
				</h2>
				<p class="text-lg max-w-3xl mx-auto text-[var(--text-muted)]">
					Browser mode turns Testhooks into a webhook tunnel — without installing anything.
					External services hit your public Testhooks URL, the payload streams over WebSocket to your browser,
					and <code class="text-[var(--purple)] bg-purple-500/5 px-1.5 py-0.5 rounded text-base">fetch()</code> delivers it straight to localhost.
				</p>
			</div>

			<!-- Flow diagram -->
			<div class="max-w-5xl mx-auto mb-14">
				<div class="flex flex-col md:flex-row items-center justify-center gap-3 md:gap-0">
					<!-- Step 1: External Service -->
					<div class="flex flex-col items-center text-center md:flex-1">
						<div class="w-16 h-16 rounded-2xl flex items-center justify-center mb-3 bg-orange-50 dark:bg-orange-500/10 border border-orange-200 dark:border-orange-500/30">
							<Globe class="w-7 h-7 text-orange-500" />
						</div>
						<h4 class="text-sm font-semibold font-fira text-[var(--text)] mb-1">Stripe / GitHub / Slack</h4>
						<p class="text-[11px] text-[var(--text-muted)] leading-relaxed">Sends webhook to<br />your Testhooks URL</p>
					</div>

					<!-- Arrow -->
					<div class="hidden md:flex items-center justify-center md:w-16 shrink-0">
						<div class="flex items-center gap-1 w-full">
							<div class="h-0.5 flex-1 bg-gradient-to-r from-orange-300 to-purple-300 dark:from-orange-500/40 dark:to-purple-500/40"></div>
							<ArrowRight class="w-4 h-4 text-[var(--text-muted)] shrink-0" />
						</div>
					</div>
					<div class="md:hidden flex justify-center">
						<ArrowRight class="w-4 h-4 text-[var(--text-muted)] rotate-90" />
					</div>

					<!-- Step 2: Your Browser -->
					<div class="flex flex-col items-center text-center md:flex-1">
						<div class="w-16 h-16 rounded-2xl flex items-center justify-center mb-3 bg-purple-50 dark:bg-purple-500/10 border border-purple-200 dark:border-purple-500/30">
							<Laptop class="w-7 h-7 text-purple-500" />
						</div>
						<h4 class="text-sm font-semibold font-fira text-[var(--text)] mb-1">Your Browser</h4>
						<p class="text-[11px] text-[var(--text-muted)] leading-relaxed">Receives via WebSocket,<br />inspects, transforms, forwards</p>
					</div>

					<!-- Arrow -->
					<div class="hidden md:flex items-center justify-center md:w-16 shrink-0">
						<div class="flex items-center gap-1 w-full">
							<div class="h-0.5 flex-1 bg-gradient-to-r from-purple-300 to-green-300 dark:from-purple-500/40 dark:to-green-500/40"></div>
							<ArrowRight class="w-4 h-4 text-[var(--text-muted)] shrink-0" />
						</div>
					</div>
					<div class="md:hidden flex justify-center">
						<ArrowRight class="w-4 h-4 text-[var(--text-muted)] rotate-90" />
					</div>

					<!-- Step 3: Localhost -->
					<div class="flex flex-col items-center text-center md:flex-1">
						<div class="w-16 h-16 rounded-2xl flex items-center justify-center mb-3 bg-green-50 dark:bg-green-500/10 border border-green-200 dark:border-green-500/30">
							<Terminal class="w-7 h-7 text-green-500" />
						</div>
						<h4 class="text-sm font-semibold font-fira text-[var(--text)] mb-1">localhost:3000</h4>
						<p class="text-[11px] text-[var(--text-muted)] leading-relaxed">Your app receives<br />the webhook via fetch()</p>
					</div>
				</div>

				<!-- Annotation bar -->
				<div class="hidden md:flex justify-between max-w-3xl mx-auto mt-6 px-8">
					<span class="text-[10px] font-fira text-orange-500/70 dark:text-orange-400/50">POST /h/abc123</span>
					<span class="text-[10px] font-fira text-purple-500/70 dark:text-purple-400/50">WebSocket push</span>
					<span class="text-[10px] font-fira text-green-500/70 dark:text-green-400/50">fetch("http://localhost:3000")</span>
				</div>
			</div>

			<!-- Comparison table -->
			<div class="max-w-4xl mx-auto">
				<div class="rounded-xl border border-[var(--border)] overflow-hidden">
					<!-- Header -->
					<div class="grid grid-cols-3 bg-[var(--bg)] border-b border-[var(--border)]">
						<div class="px-5 py-3"></div>
						<div class="px-5 py-3 text-center border-l border-[var(--border)]">
							<span class="text-xs font-fira font-semibold text-[var(--text-muted)] uppercase tracking-wider">ngrok / localtunnel</span>
						</div>
						<div class="px-5 py-3 text-center border-l border-purple-200 dark:border-purple-500/30 bg-purple-50/50 dark:bg-purple-500/5">
							<span class="text-xs font-fira font-semibold text-[var(--purple)] uppercase tracking-wider">Testhooks Browser Mode</span>
						</div>
					</div>
					<!-- Rows -->
					{#each [
						{ label: 'Setup', tunnel: 'Install CLI, authenticate, run daemon', testhooks: 'Open a browser tab' },
						{ label: 'Inspect payloads', tunnel: 'Separate logging or dashboard', testhooks: 'Built-in live view with syntax highlighting' },
						{ label: 'Transform before delivery', tunnel: 'Not possible', testhooks: 'JS, Lua, or Jsonnet in sandboxed WASM' },
						{ label: 'Custom responses', tunnel: 'Whatever your local server returns', testhooks: 'Script-defined — test error handling without changing your app' },
						{ label: 'Data privacy', tunnel: 'Traffic routes through third-party servers', testhooks: 'Payloads never stored — browser-only, zero disk' }
					] as row}
						<div class="grid grid-cols-3 border-b border-[var(--border)] last:border-b-0">
							<div class="px-5 py-3 flex items-center">
								<span class="text-xs font-semibold text-[var(--text)]">{row.label}</span>
							</div>
							<div class="px-5 py-3 border-l border-[var(--border)] flex items-center">
								<span class="text-xs text-[var(--text-muted)]">{row.tunnel}</span>
							</div>
							<div class="px-5 py-3 border-l border-purple-200 dark:border-purple-500/30 bg-purple-50/30 dark:bg-purple-500/5 flex items-center gap-2">
								<CircleCheck class="w-3.5 h-3.5 text-[var(--green)] shrink-0" />
								<span class="text-xs text-[var(--text)]">{row.testhooks}</span>
							</div>
						</div>
					{/each}
				</div>

				<!-- Provider callout -->
				<p class="text-center text-sm text-[var(--text-muted)] mt-8">
					Works with any webhook provider — Stripe, GitHub, Slack, Shopify, Twilio, Linear, and more.
					<br />
					<span class="text-xs">Just point their webhook URL at your Testhooks endpoint.</span>
				</p>
			</div>
		</div>
	</section>

	<!-- ===== CREATE ENDPOINT SECTION ===== -->
	<section id="create-section" class="py-20 bg-[var(--bg)]">
		<div class="max-w-3xl mx-auto px-6">
			<div class="bg-[var(--bg-card)] border border-[var(--border)] rounded-xl overflow-hidden shadow-lg shadow-blue-500/5">
				<div class="px-6 pt-6 pb-4">
					<div class="flex items-center gap-3 mb-1">
						<Webhook class="w-5 h-5 text-[var(--accent)]" />
						<h2 class="text-lg font-semibold font-fira">Create New Endpoint</h2>
					</div>
					<p class="text-xs text-[var(--text-muted)] mb-4 ml-8">Get a unique URL to capture, inspect, and forward webhooks in real time.</p>

					<!-- Row 1: Name + Mode -->
					<div class="flex gap-3 mb-3">
						<input
							bind:value={newName}
							placeholder="Endpoint name (optional)"
							class="flex-1 bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-2 text-sm text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)] transition-colors"
						/>
						<div class="flex gap-1 bg-[var(--bg)] border border-[var(--border)] rounded-lg p-1">
							<button
								class="px-3 py-1.5 rounded-md text-xs font-medium transition-colors flex items-center gap-1.5 {newMode === 'server' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
								onclick={() => (newMode = 'server')}
								title="Stores requests in database, always-on transforms & forwarding"
							>
								<Server class="w-3.5 h-3.5" />Server
							</button>
							<button
								class="px-3 py-1.5 rounded-md text-xs font-medium transition-colors flex items-center gap-1.5 {newMode === 'browser' ? 'bg-[var(--accent)] text-white' : 'text-[var(--text-muted)] hover:text-[var(--text)]'}"
								onclick={() => (newMode = 'browser')}
								title="No data stored on server, all processing in browser"
							>
								<Monitor class="w-3.5 h-3.5" />Browser
							</button>
						</div>
					</div>

					<!-- Mode description -->
					<div class="text-[11px] text-[var(--text-muted)] mb-4 px-1">
						{#if newMode === 'server'}
							Requests are stored in the database. Transforms and forwarding run server-side, always-on even without a browser.
						{:else}
							Privacy-first: payloads never touch the server's disk. Transforms and forwarding run in your browser. Ideal for localhost targets.
						{/if}
					</div>
				</div>

				<!-- Advanced Configuration toggle -->
				<button
					class="w-full px-6 py-2.5 border-t border-[var(--border)] flex items-center gap-2 cursor-pointer select-none hover:bg-[var(--bg-hover)] transition-colors"
					onclick={() => (showAdvanced = !showAdvanced)}
				>
					{#if showAdvanced}
						<ChevronDown class="w-3.5 h-3.5 text-[var(--text-muted)]" />
					{:else}
						<ChevronRight class="w-3.5 h-3.5 text-[var(--text-muted)]" />
					{/if}
					<span class="text-xs font-medium text-[var(--text-muted)]">Advanced Configuration</span>
					{#if newForwardUrl.trim() || newTransformEnabled || newCustomResponseEnabled}
						<span class="text-[10px] px-1.5 py-0.5 rounded bg-[var(--accent)]/10 text-[var(--accent)]">
							{[
								newForwardUrl.trim() ? 'forwarding' : '',
								newTransformEnabled ? 'transform' : '',
								newCustomResponseEnabled ? 'custom response' : ''
							].filter(Boolean).join(', ')}
						</span>
					{/if}
				</button>

				{#if showAdvanced}
					<div class="px-6 py-4 border-t border-[var(--border)] flex flex-col gap-5">
						<!-- Forward URL (single) -->
						<div>
							<div class="flex items-center gap-2 mb-2">
								<Send class="w-3.5 h-3.5 text-[var(--text-muted)]" />
								<span class="text-xs font-medium text-[var(--text)]">Forward URL</span>
								<span class="text-[10px] text-[var(--text-muted)]">
									{newMode === 'browser' ? '(browser-side fetch - localhost works)' : '(server-side HTTP - public URLs only)'}
								</span>
							</div>
							<div class="flex flex-col gap-2">
								<input
									bind:value={newForwardUrl}
									placeholder="https://example.com/webhook or http://localhost:3000/hook"
									oninput={() => (forwardUrlError = null)}
									onblur={validateForwardUrl}
									class="bg-[var(--bg)] border border-[var(--border)] rounded-lg px-3 py-1.5 text-sm text-[var(--text)] placeholder:text-[var(--text-muted)] focus:outline-none focus:border-[var(--accent)] {forwardUrlError ? 'border-red-500/50' : ''} transition-colors"
								/>
								{#if forwardUrlError}
									<p class="text-[11px] text-[var(--red)] px-1">{forwardUrlError}</p>
								{/if}
								{#if newForwardUrl.trim()}
									<div class="flex items-center gap-3">
										<span class="text-[11px] text-[var(--text-muted)]">Mode:</span>
										<div class="flex items-center border border-[var(--border)] rounded-lg overflow-hidden">
											<button
												onclick={() => (newForwardMode = 'async')}
												class="text-[11px] px-3 py-1 transition-colors {newForwardMode === 'async'
													? 'bg-[var(--accent)] text-white'
													: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
											>
												Async
											</button>
											<button
												onclick={() => (newForwardMode = 'sync')}
												class="text-[11px] px-3 py-1 transition-colors {newForwardMode === 'sync'
													? 'bg-[var(--accent)] text-white'
													: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
											>
												Sync
											</button>
										</div>
										<span class="text-[10px] text-[var(--text-muted)]">
											{newForwardMode === 'async' ? 'Fire-and-forget, does not block response' : 'Waits for target response, usable in handler'}
										</span>
									</div>
								{/if}
							</div>
						</div>

						<!-- Transform Script -->
						<div>
							<div class="flex items-center justify-between mb-2">
								<div class="flex items-center gap-2">
									<Code2 class="w-3.5 h-3.5 text-[var(--text-muted)]" />
									<span class="text-xs font-medium text-[var(--text)]">Transform Script</span>
								</div>
								<label class="flex items-center gap-2 cursor-pointer">
									<span class="text-[10px] text-[var(--text-muted)]">{newTransformEnabled ? 'Enabled' : 'Disabled'}</span>
									<input
										type="checkbox"
										bind:checked={newTransformEnabled}
										class="w-3.5 h-3.5 rounded border-[var(--border)] accent-[var(--accent)]"
									/>
								</label>
							</div>

							{#if newTransformEnabled}
								<div class="flex flex-col gap-2">
									<div class="flex items-center gap-2">
										<span class="text-[11px] text-[var(--text-muted)]">Language:</span>
										<div class="flex items-center border border-[var(--border)] rounded-lg overflow-hidden">
											{#each SUPPORTED_LANGUAGES as lang}
												<button
													onclick={() => { newTransformLanguage = lang.id; newTransformScript = ''; }}
													class="text-[11px] px-2.5 py-1 transition-colors {newTransformLanguage === lang.id
														? 'bg-[var(--accent)] text-white'
														: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
												>
													{lang.label}
												</button>
											{/each}
										</div>
										{#if !newTransformScript}
											<button
												onclick={() => (newTransformScript = DEFAULT_SCRIPTS[newTransformLanguage])}
												class="text-[11px] px-2 py-1 rounded-lg border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
											>
												Insert template
											</button>
										{/if}
									</div>
									<CodeEditor
										value={newTransformScript}
										onchange={(v) => (newTransformScript = v)}
										placeholder={DEFAULT_SCRIPTS[newTransformLanguage].split('\n')[0]}
										minHeight="140px"
										language={newTransformLanguage}
									/>
								</div>
							{/if}
						</div>

						<!-- Custom Response (script-based) -->
						<div>
							<div class="flex items-center justify-between mb-2">
								<div class="flex items-center gap-2">
									<MessageSquare class="w-3.5 h-3.5 text-[var(--text-muted)]" />
									<span class="text-xs font-medium text-[var(--text)]">Custom Response</span>
									<span class="text-[10px] text-[var(--text-muted)]">(what webhook senders receive back)</span>
								</div>
								<label class="flex items-center gap-2 cursor-pointer">
									<span class="text-[10px] text-[var(--text-muted)]">{newCustomResponseEnabled ? 'Enabled' : 'Disabled'}</span>
									<input
										type="checkbox"
										bind:checked={newCustomResponseEnabled}
										class="w-3.5 h-3.5 rounded border-[var(--border)] accent-[var(--accent)]"
									/>
								</label>
							</div>

							{#if newCustomResponseEnabled}
								<div class="flex flex-col gap-2">
									<div class="flex items-center gap-2">
										<span class="text-[11px] text-[var(--text-muted)]">Language:</span>
										<div class="flex items-center border border-[var(--border)] rounded-lg overflow-hidden">
											{#each SUPPORTED_LANGUAGES as lang}
												<button
													onclick={() => { newCustomResponseLanguage = lang.id; newCustomResponseScript = ''; }}
													class="text-[11px] px-2.5 py-1 transition-colors {newCustomResponseLanguage === lang.id
														? 'bg-[var(--accent)] text-white'
														: 'text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)]'}"
												>
													{lang.label}
												</button>
											{/each}
										</div>
										{#if !newCustomResponseScript}
											<button
												onclick={() => (newCustomResponseScript = DEFAULT_RESPONSE_SCRIPTS[newCustomResponseLanguage])}
												class="text-[11px] px-2 py-1 rounded-lg border border-[var(--border)] hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
											>
												Insert template
											</button>
										{/if}
									</div>
									{#if newMode === 'server' && newCustomResponseLanguage !== 'javascript'}
										<p class="text-[10px] text-[var(--yellow)]">Server mode only supports JavaScript. {newCustomResponseLanguage === 'lua' ? 'Lua' : 'Jsonnet'} will only run when the browser tab is open.</p>
									{/if}
									<CodeEditor
										value={newCustomResponseScript}
										onchange={(v) => (newCustomResponseScript = v)}
										placeholder={DEFAULT_RESPONSE_SCRIPTS[newCustomResponseLanguage].split('\n')[0]}
										minHeight="140px"
										language={newCustomResponseLanguage}
									/>
								</div>
							{/if}
						</div>
					</div>
				{/if}

				<!-- Error message -->
				{#if createError}
					<div class="mx-6 mt-4 px-3 py-2 rounded bg-red-500/10 border border-red-500/20 text-sm text-[var(--red)] flex items-center justify-between">
						<span>{createError}</span>
						<button onclick={() => (createError = null)} class="ml-2 p-0.5 rounded hover:bg-red-500/20 transition-colors">
							<X class="w-3.5 h-3.5" />
						</button>
					</div>
				{/if}

				<!-- Create button -->
				<div class="px-6 py-4 border-t border-[var(--border)]">
					<button
						onclick={handleCreate}
						disabled={creating}
						class="w-full bg-[var(--accent)] hover:bg-[var(--accent-hover)] text-white rounded-xl px-4 py-2.5 text-sm font-medium transition-colors disabled:opacity-50 flex items-center justify-center gap-2 shadow-lg shadow-blue-500/20"
					>
						<Webhook class="w-4 h-4" />
						{creating ? 'Creating...' : 'Create Endpoint'}
					</button>
				</div>
			</div>
		</div>
	</section>

	<!-- ===== ENDPOINTS LIST ===== -->
	<section class="py-16 bg-[var(--bg-card)]">
		<div class="max-w-3xl mx-auto px-6">
			{#if deleteError}
				<div class="mb-3 px-3 py-2 rounded bg-red-500/10 border border-red-500/20 text-sm text-[var(--red)] flex items-center justify-between">
					<span>{deleteError}</span>
					<button onclick={() => (deleteError = null)} class="ml-2 p-0.5 rounded hover:bg-red-500/20 transition-colors">
						<X class="w-3.5 h-3.5" />
					</button>
				</div>
			{/if}
			<h2 class="text-lg font-semibold font-fira mb-4">
				Your Endpoints
				{#if endpoints.length > 0}
					<span class="text-sm font-normal text-[var(--text-muted)] ml-2">({endpoints.length})</span>
				{/if}
			</h2>

			{#if loading}
				<div class="text-center py-12 text-[var(--text-muted)]">Loading...</div>
			{:else if loadError}
				<div class="px-4 py-3 rounded bg-red-500/10 border border-red-500/20 text-sm text-[var(--red)] flex items-center justify-between">
					<span>{loadError}</span>
					<button onclick={loadEndpoints} class="ml-2 text-xs underline hover:no-underline">Retry</button>
				</div>
			{:else if endpoints.length === 0}
				<div class="text-center py-12 text-[var(--text-muted)]">
					<p class="mb-2">No endpoints yet.</p>
					<p class="text-sm">Create one above to get started.</p>
				</div>
			{:else}
				<div class="flex flex-col gap-2">
					{#each endpoints as ep (ep.id)}
						<div class="bg-[var(--bg)] border border-[var(--border)] rounded-lg p-4 hover:border-[var(--accent)]/30 transition-colors group">
							<div class="flex items-center justify-between">
								<a href="/{ep.slug}" class="flex-1 min-w-0">
									<div class="flex items-center gap-3">
										<span class="text-sm font-mono text-[var(--accent)]">{ep.slug}</span>
										{#if ep.name}
											<span class="text-sm text-[var(--text)] truncate">{ep.name}</span>
										{/if}
										<span class="text-[10px] px-1.5 py-0.5 rounded {ep.mode === 'server' ? 'bg-blue-500/10 text-blue-400' : 'bg-purple-500/10 text-purple-400'} flex items-center gap-1">
											{#if ep.mode === 'server'}<Server class="w-2.5 h-2.5" />{:else}<Monitor class="w-2.5 h-2.5" />{/if}
											{ep.mode}
										</span>
										<!-- Config badges -->
										{#if ep.config?.forward_url}
											<span class="text-[10px] px-1.5 py-0.5 rounded bg-green-500/10 text-[var(--green)]">
												forward
											</span>
										{/if}
										{#if ep.config?.wasm_script}
											<span class="text-[10px] px-1.5 py-0.5 rounded bg-orange-500/10 text-[var(--orange)]">
												{ep.config.transform_language ?? 'js'} transform
											</span>
										{/if}
									</div>
									<div class="text-xs text-[var(--text-muted)] mt-1">
										Created {formatTime(ep.created_at)} &middot; <code class="text-[10px]">{getWebhookURL(ep.slug)}</code>
									</div>
								</a>

								<div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
									<button
										onclick={() => handleCopy(ep.slug)}
										class="p-2 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
										title="Copy webhook URL"
									>
										{#if copied === ep.slug}
											<span class="text-[10px] text-[var(--green)]">Copied!</span>
										{:else}
											<Copy class="w-4 h-4" />
										{/if}
									</button>
									<a
										href="/{ep.slug}"
										class="p-2 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] hover:text-[var(--text)] transition-colors"
										title="Open"
									>
										<ExternalLink class="w-4 h-4" />
									</a>
									{#if deleteConfirm === ep.id}
										<button
											onclick={() => handleDelete(ep.id)}
											class="text-[10px] px-2 py-1 rounded bg-red-500/10 text-[var(--red)] hover:bg-red-500/20 transition-colors"
										>
											Confirm
										</button>
										<button
											onclick={() => (deleteConfirm = null)}
											class="text-[10px] px-2 py-1 rounded hover:bg-[var(--bg-hover)] text-[var(--text-muted)] transition-colors"
										>
											Cancel
										</button>
									{:else}
										<button
											onclick={() => (deleteConfirm = ep.id)}
											class="p-2 rounded hover:bg-red-500/10 text-[var(--text-muted)] hover:text-[var(--red)] transition-colors"
											title="Delete endpoint"
										>
											<Trash2 class="w-4 h-4" />
										</button>
									{/if}
								</div>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	</section>

	<!-- ===== FOOTER CTA ===== -->
	<section class="py-20 bg-gradient-to-b from-[var(--bg-card)] to-blue-50/30 dark:to-blue-950/10">
		<div class="max-w-3xl mx-auto px-6 text-center">
			<h2 class="text-3xl md:text-4xl font-bold font-fira mb-6">
				Ready to <span class="bg-gradient-to-r from-[var(--accent)] to-purple-500 bg-clip-text text-transparent">Own Your Webhooks</span>?
			</h2>
			<p class="text-lg mb-8 text-[var(--text-muted)]">
				Open source, self-hostable, no sign-up required. One binary, one database — and browser mode reaches localhost too.
			</p>
			<div class="flex flex-col sm:flex-row items-center justify-center gap-4">
				<button
					onclick={scrollToCreate}
					class="inline-flex items-center gap-2 px-7 py-3 text-base font-medium rounded-xl bg-[var(--accent)] text-white hover:bg-[var(--accent-hover)] transition-all shadow-lg shadow-blue-500/20"
				>
					Get Started
					<ArrowRight class="w-4 h-4" />
				</button>
				<a
					href="https://github.com/sarathsp06/testhooks"
					target="_blank"
					class="inline-flex items-center gap-2 px-7 py-3 text-base font-medium rounded-xl border border-[var(--border)] text-[var(--text)] hover:bg-[var(--bg-hover)] transition-all"
				>
					<svg class="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
						<path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
					</svg>
					Star on GitHub
				</a>
			</div>
		</div>
	</section>

	<!-- ===== FOOTER ===== -->
	<footer class="py-8 border-t border-[var(--border)]">
		<div class="max-w-[1200px] mx-auto px-6">
			<div class="flex flex-col md:flex-row items-center justify-between gap-4">
				<div class="flex items-center gap-2">
					<Webhook class="w-4 h-4 text-[var(--text-muted)]" />
					<span class="font-fira text-sm"><span class="font-normal text-[var(--text-muted)]">TEST</span><span class="font-bold text-[var(--text)]">HOOKS</span></span>
				</div>
				<p class="text-xs text-[var(--text-muted)]">Self-hostable webhook inspector. Open source.</p>
				<a
					href="https://github.com/sarathsp06/testhooks"
					target="_blank"
					class="text-xs text-[var(--text-muted)] hover:text-[var(--accent)] transition-colors"
				>
					GitHub
				</a>
			</div>
		</div>
	</footer>
</div>
