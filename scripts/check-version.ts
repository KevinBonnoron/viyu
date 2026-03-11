// The VERSION file is the single version source: release-please bumps it in its release pull request, the Android
// build derives versionName/versionCode from it (apps/android/app/build.gradle) and CI labels the Docker image with
// it. This script checks that it is a plain MAJOR.MINOR.PATCH version and, in a release run (RELEASE_TAG, set by
// release.yml), that the tag matches it.
const version = (await Bun.file(new URL('../VERSION', import.meta.url)).text()).trim();
const errors: string[] = [];

if (!/^\d+\.\d+\.\d+$/.test(version)) {
  errors.push(`VERSION '${version}' is not MAJOR.MINOR.PATCH`);
}

const tag = process.env.RELEASE_TAG;
if (tag && tag !== `v${version}`) {
  errors.push(`release tag ${tag} does not match VERSION v${version}`);
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(error);
  }
  process.exit(1);
}
console.log(`version ${version}`);
