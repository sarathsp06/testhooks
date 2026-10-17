// Expressive Code (code block) settings for the docs. Kept out of
// astro.config.mjs because it contains a function, which Starlight
// requires to live in this file.
import { defineEcConfig } from '@astrojs/starlight/expressive-code';

export default defineEcConfig({
	// Dark code panels in both colour modes, like the landing page's command box.
	themes: ['github-dark-default'],
	// Starlight scopes each theme to its colour mode; apply this one everywhere.
	themeCssSelector: () => ':root',
	useStarlightUiThemeColors: false,
	// Plain panels (no terminal title bar), like the landing command box.
	defaultProps: { frame: 'code' },
	styleOverrides: {
		borderRadius: '12px',
		borderColor: '#0e1210',
		codeBackground: '#0e1210',
		codeFontFamily: "'Geist Mono', ui-monospace, SFMono-Regular, Menlo, monospace",
		codeFontSize: '14px',
		uiFontFamily: "'Geist', system-ui, sans-serif",
		frames: {
			editorBackground: '#0e1210',
			editorTabBarBackground: '#161c19',
			editorActiveTabBackground: '#0e1210',
			terminalBackground: '#0e1210',
			terminalTitlebarBackground: '#161c19',
			terminalTitlebarBorderBottomColor: '#222a26',
			terminalTitlebarDotsForeground: '#3b4540',
			frameBoxShadowCssValue: '0 12px 32px -12px rgba(17, 22, 19, 0.28)',
		},
	},
});
