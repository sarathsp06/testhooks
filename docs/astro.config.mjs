// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// Served from GitHub Pages at https://sarathsp06.github.io/testhooks/.
// Override with DOCS_SITE / DOCS_BASE when hosting elsewhere (e.g. a custom domain with base "/").
export default defineConfig({
	site: process.env.DOCS_SITE ?? 'https://sarathsp06.github.io',
	base: process.env.DOCS_BASE ?? '/testhooks',
	integrations: [
		starlight({
			title: 'testhooks',
			description:
				'Self-hostable webhook inspector: capture, inspect, transform, forward and mock webhooks in real time.',
			logo: { src: './src/assets/logo.svg', alt: 'Testhooks' },
			favicon: '/favicon.svg',
			head: [
				{ tag: 'link', attrs: { rel: 'preconnect', href: 'https://fonts.googleapis.com' } },
				{ tag: 'link', attrs: { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' } },
				{
					tag: 'link',
					attrs: {
						rel: 'stylesheet',
						href: 'https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600;700&family=Geist+Mono:wght@400;500&display=swap',
					},
				},
			],
			customCss: ['./src/styles/custom.css'],
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/sarathsp06/testhooks' }],
			editLink: { baseUrl: 'https://github.com/sarathsp06/testhooks/edit/main/docs/' },
			lastUpdated: true,
			sidebar: [
				{
					label: 'Getting started',
					items: [
						{ label: 'Quickstart', slug: 'getting-started/quickstart' },
						{ label: 'Installation', slug: 'getting-started/installation' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Server and browser mode', slug: 'guides/modes' },
						{ label: 'Webhooks to localhost', slug: 'guides/localhost' },
						{ label: 'Forwarding', slug: 'guides/forwarding' },
						{ label: 'Transforms and custom responses', slug: 'guides/scripts' },
						{ label: 'Verifying signed webhooks', slug: 'guides/signatures' },
					],
				},
				{
					label: 'Deployment',
					items: [
						{ label: 'Running in production', slug: 'deployment/production' },
						{ label: 'Deploy on Vercel', slug: 'deployment/vercel' },
						{ label: 'Releasing', slug: 'deployment/releasing' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'Configuration', slug: 'reference/configuration' },
						{ label: 'REST API', slug: 'reference/api' },
						{ label: 'WebSocket events', slug: 'reference/websocket' },
						{ label: 'Status codes', slug: 'reference/status-codes' },
					],
				},
			],
		}),
	],
});
