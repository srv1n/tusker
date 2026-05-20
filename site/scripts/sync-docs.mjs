import { promises as fs } from 'node:fs';
import path from 'node:path';

console.error(
  '[tusker] site/scripts/sync-docs.mjs is legacy. Use `tusker docs export --site ./site` or `npm --prefix site run export`.'
);
process.exit(1);

const siteRoot = process.cwd();
const repoRoot = path.resolve(siteRoot, '..');
const docsRoot = path.join(siteRoot, 'src', 'content', 'docs');

const manualMappings = [
  { source: 'docs/specs/README.md', target: 'developer/specs/index.md', section: 'Developer Specs' },
  { source: 'docs/specs/00-product-modes.md', target: 'developer/specs/00-product-modes.md', section: 'Developer Specs' },
  { source: 'docs/specs/01-vault-tracker.md', target: 'developer/specs/01-vault-tracker.md', section: 'Developer Specs' },
  { source: 'docs/specs/02-workflow-contract.md', target: 'developer/specs/02-workflow-contract.md', section: 'Developer Specs' },
  { source: 'docs/specs/03-daemon-and-registry.md', target: 'developer/specs/03-daemon-and-registry.md', section: 'Developer Specs' },
  { source: 'docs/specs/04-workspace-manager.md', target: 'developer/specs/04-workspace-manager.md', section: 'Developer Specs' },
  { source: 'docs/specs/05-runner-and-session-protocol.md', target: 'developer/specs/05-runner-and-session-protocol.md', section: 'Developer Specs' },
  { source: 'docs/specs/06-review-rework-retry.md', target: 'developer/specs/06-review-rework-retry.md', section: 'Developer Specs' },
  { source: 'docs/specs/07-documentation-site-and-publication.md', target: 'developer/specs/07-documentation-site-and-publication.md', section: 'Developer Specs' },
  { source: 'docs/specs/08-symphony-alignment-and-orchestration-roadmap.md', target: 'developer/specs/08-symphony-alignment-and-orchestration-roadmap.md', section: 'Developer Specs' },
  {
    source: 'README.md',
    target: 'developer/repository/repository-overview.md',
    section: 'Developer Repository',
    title: 'Repository Overview',
  },
  { source: 'skill/docs/DISPATCHER_PSEUDOCODE.md', target: 'developer/internals/dispatcher-pseudocode.md', section: 'Developer Internals' },
  { source: 'skill/docs/FAILURE_CLASSES.md', target: 'developer/internals/failure-classes.md', section: 'Developer Internals' },
  { source: 'skill/docs/OPERATOR_INTERVENTION.md', target: 'developer/internals/operator-intervention.md', section: 'Developer Internals' },
  {
    source: 'skill/README.md',
    target: 'user/start-here/skill-bundle.md',
    section: 'User Docs',
    title: 'Skill Bundle',
  },
  {
    source: 'skill/SKILL.md',
    target: 'user/start-here/agent-workflow.md',
    section: 'User Docs',
    title: 'Agent Workflow',
  },
];

const generatedMappings = [
  {
    dir: 'skill/references',
    targetDir: 'user/reference',
    section: 'User Reference',
    order: (name) => name,
  },
];

function normalizeLineEndings(text) {
  return text.replace(/\r\n/g, '\n');
}

function stripFrontmatter(text) {
  const normalized = normalizeLineEndings(text);
  if (!normalized.startsWith('---\n')) {
    return { data: {}, body: normalized };
  }
  const end = normalized.indexOf('\n---\n', 4);
  if (end === -1) {
    return { data: {}, body: normalized };
  }
  const raw = normalized.slice(4, end);
  const body = normalized.slice(end + 5);
  const data = {};
  for (const line of raw.split('\n')) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (!match) continue;
    let [, key, value] = match;
    value = value.trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    data[key] = value;
  }
  return { data, body };
}

function firstHeading(body) {
  const match = body.match(/^#\s+(.+)$/m);
  return match ? match[1].trim() : '';
}

function firstParagraph(body) {
  const cleaned = body
    .replace(/^#.*$/gm, '')
    .replace(/```[\s\S]*?```/g, '')
    .replace(/!\[[^\]]*\]\([^)]+\)/g, '')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
  for (const chunk of cleaned.split('\n\n')) {
    const paragraph = chunk
      .split('\n')
      .map((line) => line.trim())
      .join(' ')
      .trim();
    if (!paragraph) continue;
    if (paragraph.startsWith('Status:')) continue;
    if (paragraph.startsWith('Scope:')) continue;
    return paragraph.replace(/\s+/g, ' ');
  }
  return '';
}

function yamlEscape(value) {
  return `"${String(value).replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
}

function toPosix(filePath) {
  return filePath.split(path.sep).join('/');
}

function buildMappings() {
  return Promise.all(
    generatedMappings.map(async (group) => {
      const dir = path.join(repoRoot, group.dir);
      const names = (await fs.readdir(dir)).filter((name) => name.endsWith('.md')).sort();
      return names.map((name) => ({
        source: path.join(group.dir, name),
        target: path.join(group.targetDir, group.order(name)),
        section: group.section,
      }));
    })
  ).then((parts) => [...manualMappings, ...parts.flat()]);
}

function rewriteLinks(body, sourceAbs, targetAbs, routeMap) {
  return body.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (full, label, href) => {
    if (
      href.startsWith('http://') ||
      href.startsWith('https://') ||
      href.startsWith('#') ||
      href.startsWith('mailto:')
    ) {
      return full;
    }

    const cleanHref = href.split('#')[0];
    if (!cleanHref.endsWith('.md') && !cleanHref.endsWith('.mdx')) {
      return full;
    }

    const resolvedSource = path.resolve(path.dirname(sourceAbs), cleanHref);
    const mappedTarget = routeMap.get(resolvedSource);
    if (!mappedTarget) {
      return full;
    }

    const relativePath = toPosix(path.relative(path.dirname(targetAbs), mappedTarget));
    const suffix = href.includes('#') ? `#${href.split('#').slice(1).join('#')}` : '';
    return `[${label}](${relativePath}${suffix})`;
  });
}

async function ensureDir(filePath) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
}

async function cleanGeneratedDirs() {
  const generated = [
    path.join(docsRoot, 'developer', 'specs'),
    path.join(docsRoot, 'developer', 'repository'),
    path.join(docsRoot, 'developer', 'internals'),
    path.join(docsRoot, 'user', 'start-here'),
    path.join(docsRoot, 'user', 'reference'),
  ];
  await Promise.all(generated.map((dir) => fs.rm(dir, { recursive: true, force: true })));
}

async function main() {
  const mappings = await buildMappings();
  const routeMap = new Map(
    mappings.map((entry) => [path.join(repoRoot, entry.source), path.join(docsRoot, entry.target)])
  );

  await cleanGeneratedDirs();

  await Promise.all(
    mappings.map(async (entry) => {
      const sourceAbs = path.join(repoRoot, entry.source);
      const targetAbs = path.join(docsRoot, entry.target);
      const raw = await fs.readFile(sourceAbs, 'utf8');
      const { data, body } = stripFrontmatter(raw);
      const title =
        entry.title || data.title || data.name || firstHeading(body) || path.basename(entry.source, '.md');
      const description =
        data.description ||
        firstParagraph(body) ||
        `${entry.section} synced from ${entry.source}.`;
      const rewritten = rewriteLinks(body.trim(), sourceAbs, targetAbs, routeMap);
      const sourceLine = `_Synced from \`${entry.source}\`._`;
      const output = `---\ntitle: ${yamlEscape(title)}\ndescription: ${yamlEscape(description)}\n---\n\n${sourceLine}\n\n${rewritten.trim()}\n`;

      await ensureDir(targetAbs);
      await fs.writeFile(targetAbs, output);
    })
  );

  process.stdout.write(`Synced ${mappings.length} docs.\n`);
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
