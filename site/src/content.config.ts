import { defineCollection } from 'astro:content';
import { z } from 'astro/zod';
import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

const tuskerMetadataSchema = z
	.object({
		source_kind: z.string().min(1).optional(),
		id: z.string().min(1).optional(),
		audience: z.string().min(1).optional(),
		doc_intent: z.string().min(1).optional(),
		epic: z.string().optional(),
		story: z.string().optional(),
		source_path: z.string().min(1).optional(),
		publish_path: z.string().min(1).optional(),
		publish_section_title: z.string().min(1).optional(),
		publish_order: z.number().int().optional(),
		route: z.string().min(1).optional(),
		summary: z.string().min(1).optional(),
		published_at: z.string().optional(),
		created: z.string().optional(),
		updated: z.string().optional(),
		tags: z.array(z.string()).optional(),
	})
	.passthrough();

export const collections = {
	docs: defineCollection({
		loader: docsLoader(),
		schema: docsSchema({
			extend: z.object({
				tusker: tuskerMetadataSchema.optional(),
			}),
		}),
	}),
};
