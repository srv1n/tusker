// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { resolveSidebarNavigation } from './src/utils/navigation.ts';

// https://astro.build/config
export default defineConfig({
	integrations: [
		starlight({
			title: 'Tusker Docs',
			description: 'Developer architecture docs and user-facing operating guidance for Tusker, the markdown-first agent work tracker.',
			logo: {
				src: './src/assets/tusker-mark.svg',
				alt: 'Tusker',
			},
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/srv1n/tusker' }],
			customCss: [
				'@fontsource-variable/manrope',
				'@fontsource-variable/newsreader',
				'@fontsource/ibm-plex-mono',
				'./src/styles/custom.css',
			],
			lastUpdated: true,
			sidebar: resolveSidebarNavigation(new URL('./src/generated/navigation.json', import.meta.url)),
		}),
	],
});
