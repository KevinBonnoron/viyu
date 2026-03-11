// The VERSION file is the single version source: the Android build derives versionName/versionCode from it
// (apps/android/app/build.gradle), the extension build writes it into its manifest and CI labels the Docker image with
// it. This script checks that it is a plain MAJOR.MINOR.PATCH version (browser extension manifests accept nothing else)
// and, on release tags, that the tag matches it.
const version = (await Bun.file(new URL('../VERSION', import.meta.url)).text()).trim();
const errors: string[] = [];

if (!/^\d+\.\d+\.\d+$/.test(version)) {
  errors.push(`VERSION '${version}' is not MAJOR.MINOR.PATCH`);
}

const tag = process.env.GITHUB_REF_TYPE === 'tag' ? process.env.GITHUB_REF_NAME : undefined;
if (tag && tag !== `v${version}`) {
  errors.push(`tag ${tag} does not match VERSION v${version}`);
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(error);
  }
  process.exit(1);
}
console.log(`version ${version}`);
