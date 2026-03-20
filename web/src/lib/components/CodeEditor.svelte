<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { EditorView, keymap, placeholder as cmPlaceholder } from '@codemirror/view';
	import { EditorState, Compartment } from '@codemirror/state';
	import { javascript } from '@codemirror/lang-javascript';
	import { json } from '@codemirror/lang-json';
	import { StreamLanguage } from '@codemirror/language';
	import { lua } from '@codemirror/legacy-modes/mode/lua';
	import { oneDark } from '@codemirror/theme-one-dark';
	import { basicSetup } from 'codemirror';

	let {
		value = '',
		onchange,
		placeholder = '',
		readonly = false,
		minHeight = '120px',
		language = 'javascript'
	}: {
		value?: string;
		onchange?: (value: string) => void;
		placeholder?: string;
		readonly?: boolean;
		minHeight?: string;
		language?: 'javascript' | 'lua' | 'jsonnet' | 'json';
	} = $props();

	let container: HTMLDivElement;
	let view: EditorView | undefined;
	let skipNextUpdate = false;
	let languageCompartment = new Compartment();

	function getLanguageExtension(lang: string) {
		switch (lang) {
			case 'lua':
				return StreamLanguage.define(lua);
			case 'jsonnet':
			case 'json':
				// Jsonnet is a superset of JSON — JSON mode gives reasonable highlighting
				return json();
			case 'javascript':
			default:
				return javascript();
		}
	}

	onMount(() => {
		const extensions = [
			basicSetup,
			languageCompartment.of(getLanguageExtension(language)),
			oneDark,
			EditorView.lineWrapping,
			EditorView.theme({
				'&': { minHeight, fontSize: '13px' },
				'.cm-scroller': { overflow: 'auto' },
				'.cm-content': { fontFamily: "'JetBrains Mono', 'Fira Code', monospace" },
				'.cm-gutters': {
					backgroundColor: 'transparent',
					borderRight: '1px solid var(--border)'
				}
			}),
			EditorView.updateListener.of((update) => {
				if (update.docChanged && !skipNextUpdate) {
					const newValue = update.state.doc.toString();
					onchange?.(newValue);
				}
				skipNextUpdate = false;
			})
		];

		if (placeholder) {
			extensions.push(cmPlaceholder(placeholder));
		}

		if (readonly) {
			extensions.push(EditorState.readOnly.of(true));
		}

		view = new EditorView({
			state: EditorState.create({
				doc: value,
				extensions
			}),
			parent: container
		});
	});

	onDestroy(() => {
		view?.destroy();
	});

	// Sync external value changes into the editor
	$effect(() => {
		if (view && value !== view.state.doc.toString()) {
			skipNextUpdate = true;
			view.dispatch({
				changes: {
					from: 0,
					to: view.state.doc.length,
					insert: value
				}
			});
		}
	});

	// Reconfigure language when it changes
	$effect(() => {
		if (view) {
			view.dispatch({
				effects: languageCompartment.reconfigure(getLanguageExtension(language))
			});
		}
	});
</script>

<div bind:this={container} class="border border-[var(--border)] rounded overflow-hidden"></div>
