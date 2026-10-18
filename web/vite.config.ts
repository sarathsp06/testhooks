import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		// The script prelude is shared with the Go server (internal/wasm/prelude.js).
		fs: { allow: ['..'] },
		proxy: {
			'/api': 'http://localhost:8080',
			'/h': 'http://localhost:8080',
			'/ws': {
				target: 'ws://localhost:8080',
				ws: true
			}
		}
	}
});
