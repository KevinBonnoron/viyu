// Builds the unpacked extension into dist/, loadable as is in Chrome (chrome://extensions, "Load unpacked") and Firefox
// (about:debugging, "Load Temporary Add-on", pick dist/manifest.json). The scripts are bundled as IIFEs: neither the
// service worker nor the popup loads modules. The version comes from the repository's VERSION file, its single version
// source.
import { cp, mkdir, rm } from 'node:fs/promises';

const root = new URL('.', import.meta.url).pathname;
const dist = `${root}dist`;

await rm(dist, { recursive: true, force: true });
await mkdir(dist, { recursive: true });

const result = await Bun.build({
  entrypoints: [`${root}src/background.ts`, `${root}src/popup.ts`],
  outdir: dist,
  format: 'iife',
  target: 'browser',
});
if (!result.success) {
  for (const log of result.logs) {
    console.error(log);
  }
  process.exit(1);
}

await cp(`${root}static`, dist, { recursive: true });
const manifest = await Bun.file(`${root}static/manifest.json`).json();
const version = (await Bun.file(`${root}../../VERSION`).text()).trim();
await Bun.write(`${dist}/manifest.json`, `${JSON.stringify({ ...manifest, version }, null, 2)}\n`);
console.log(`extension ${version} built in ${dist}`);
