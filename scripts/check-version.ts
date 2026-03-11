// The root package.json `version` is the single version source. The Android build
// derives versionName/versionCode from it (apps/android/app/build.gradle) and the Docker
// image label is set from it in CI; this script guards the remaining copy
// (apps/client/package.json) and, on release tags, that the tag matches.
const readVersion = async (path: string): Promise<string> => {
  const manifest: { version?: string } = await Bun.file(new URL(path, import.meta.url)).json();
  if (!manifest.version) {
    throw new Error(`${path} has no version`);
  }
  return manifest.version;
};

const root = await readVersion('../package.json');
const client = await readVersion('../apps/client/package.json');
const errors: string[] = [];

if (client !== root) {
  errors.push(`apps/client/package.json version ${client} differs from root package.json ${root}`);
}

const tag = process.env.GITHUB_REF_TYPE === 'tag' ? process.env.GITHUB_REF_NAME : undefined;
if (tag && tag !== `v${root}`) {
  errors.push(`tag ${tag} does not match root package.json version v${root}`);
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(error);
  }
  process.exit(1);
}
console.log(`version ${root}`);
