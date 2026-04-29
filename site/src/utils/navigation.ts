import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

type SidebarLink = {
	label: string;
	link: string;
};

type SidebarAutoGroup = {
	label: string;
	autogenerate: {
		directory: string;
	};
};

type SidebarGroup = {
	label: string;
	items: SidebarItem[];
	collapsed?: boolean;
};

export type SidebarItem = string | SidebarLink | SidebarAutoGroup | SidebarGroup;

type SidebarLane = {
	slug: string;
	label: string;
	items: SidebarItem[];
};

type GeneratedNavigationItem = {
	title?: unknown;
	route?: unknown;
	order?: unknown;
};

type GeneratedNavigationSection = {
	slug?: unknown;
	label?: unknown;
	items?: unknown;
	sections?: unknown;
};

type GeneratedNavigationLane = GeneratedNavigationSection & {
	slug?: unknown;
	label?: unknown;
};

type GeneratedNavigationManifest = {
	lanes?: unknown;
};

const fallbackSidebarLanes: SidebarLane[] = [
	{
		slug: 'user',
		label: 'User Docs',
		items: [
			'user',
			{
				label: 'Start Here',
				autogenerate: { directory: 'user/start-here' },
			},
			{
				label: 'Reference',
				autogenerate: { directory: 'user/reference' },
			},
		],
	},
	{
		slug: 'developer',
		label: 'Developer Docs',
		items: [
			'developer',
			{
				label: 'Specs',
				autogenerate: { directory: 'developer/specs' },
			},
			{
				label: 'Repository',
				autogenerate: { directory: 'developer/repository' },
			},
			{
				label: 'Internals',
				autogenerate: { directory: 'developer/internals' },
			},
		],
	},
];

export const fallbackSidebar = fallbackSidebarLanes.map(({ label, items }) => ({ label, items }));

function normalizeManifestPath(manifestLocation: string | URL) {
	return manifestLocation instanceof URL ? fileURLToPath(manifestLocation) : manifestLocation;
}

function toTitleCase(value: string) {
	return value
		.split(/[-_/]+/g)
		.filter(Boolean)
		.map((segment) => segment.charAt(0).toUpperCase() + segment.slice(1))
		.join(' ');
}

function normalizeLabel(value: unknown, fallback: string) {
	if (typeof value !== 'string') {
		return fallback;
	}

	const trimmed = value.trim();
	return trimmed || fallback;
}

function normalizeRoute(value: unknown) {
	if (typeof value !== 'string') {
		return null;
	}

	const trimmed = value.trim();
	if (!trimmed) {
		return null;
	}

	return trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
}

function normalizeOrder(value: unknown) {
	return Number.isInteger(value) ? (value as number) : Number.POSITIVE_INFINITY;
}

function buildLinkItems(items: unknown): SidebarLink[] {
	if (!Array.isArray(items)) {
		return [];
	}

	return items
		.map((item) => {
			if (!item || typeof item !== 'object') {
				return null;
			}

			const title = normalizeLabel(
				(item as GeneratedNavigationItem).title,
				normalizeRoute((item as GeneratedNavigationItem).route) ?? ''
			);
			const link = normalizeRoute((item as GeneratedNavigationItem).route);
			if (!title || !link) {
				return null;
			}

			return {
				label: title,
				link,
				order: normalizeOrder((item as GeneratedNavigationItem).order),
			};
		})
		.filter((item): item is SidebarLink & { order: number } => item !== null)
		.sort((left, right) => {
			if (left.order !== right.order) {
				return left.order - right.order;
			}

			return left.label.localeCompare(right.label, undefined, { sensitivity: 'base' });
		})
		.map(({ order: _order, ...link }) => link);
}

function buildGroup(node: GeneratedNavigationSection): SidebarGroup | null {
	const slug = typeof node.slug === 'string' ? node.slug.trim() : '';
	const label = normalizeLabel(node.label, slug ? toTitleCase(slug) : 'Untitled');
	const items = [
		...buildLinkItems(node.items),
		...buildGroups(node.sections),
	];

	if (items.length === 0) {
		return null;
	}

	return { label, items };
}

function buildGroups(sections: unknown): SidebarGroup[] {
	if (!Array.isArray(sections)) {
		return [];
	}

	return sections
		.map((section) => {
			if (!section || typeof section !== 'object') {
				return null;
			}

			return buildGroup(section as GeneratedNavigationSection);
		})
		.filter((section): section is SidebarGroup => section !== null);
}

function loadGeneratedNavigation(manifestLocation: string | URL): GeneratedNavigationLane[] {
	const manifestPath = normalizeManifestPath(manifestLocation);
	if (!existsSync(manifestPath)) {
		return [];
	}

	const rawManifest = readFileSync(manifestPath, 'utf8').trim();
	if (!rawManifest) {
		return [];
	}

	try {
		const parsed = JSON.parse(rawManifest) as GeneratedNavigationManifest;
		return Array.isArray(parsed.lanes)
			? parsed.lanes.filter((lane): lane is GeneratedNavigationLane => !!lane && typeof lane === 'object')
			: [];
	} catch (error) {
		console.warn(
			`[tusker] Failed to parse ${manifestPath}; using static sidebar instead.`,
			error
		);
		return [];
	}
}

function buildLane(lane: GeneratedNavigationLane, includeLandingPage: boolean): SidebarLane | null {
	const slug = typeof lane.slug === 'string' ? lane.slug.trim() : '';
	if (!slug) {
		return null;
	}

	const generatedItems = [
		...buildLinkItems(lane.items),
		...buildGroups(lane.sections),
	];
	if (generatedItems.length === 0) {
		return null;
	}

	return {
		slug,
		label: normalizeLabel(lane.label, `${toTitleCase(slug)} Docs`),
		items: includeLandingPage ? [slug, ...generatedItems] : generatedItems,
	};
}

export function resolveSidebarNavigation(manifestLocation: string | URL) {
	const generatedLanes = new Map(
		loadGeneratedNavigation(manifestLocation)
			.map((lane) =>
				buildLane(
					lane,
					fallbackSidebarLanes.some((fallbackLane) => fallbackLane.slug === lane.slug)
				)
			)
			.filter((lane): lane is SidebarLane => lane !== null)
			.map((lane) => [lane.slug, lane] as const)
	);

	if (generatedLanes.size === 0) {
		return fallbackSidebar;
	}

	const mergedSidebar = fallbackSidebarLanes.map((fallbackLane) => {
		const generatedLane = generatedLanes.get(fallbackLane.slug);
		return generatedLane
			? { label: generatedLane.label, items: generatedLane.items }
			: { label: fallbackLane.label, items: fallbackLane.items };
	});

	for (const [slug, lane] of generatedLanes) {
		if (fallbackSidebarLanes.some((fallbackLane) => fallbackLane.slug === slug)) {
			continue;
		}

		mergedSidebar.push({ label: lane.label, items: lane.items });
	}

	return mergedSidebar;
}
