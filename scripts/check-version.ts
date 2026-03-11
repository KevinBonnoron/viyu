// The VERSION file is the single version source: release-please bumps it in its release pull request, the Android
// build derives versionName/versionCode from it (apps/android/app/build.gradle) and CI labels the Docker image with
// it. This script checks that it is a plain MAJOR.MINOR.PATCH version; release.yml checks a release tag against
// it before building.
const version = (await Bun.file(new URL('../VERSION', import.meta.url)).text()).trim();

if (!/^\d+\.\d+\.\d+$/.test(version)) {
  console.error(`VERSION '${version}' is not MAJOR.MINOR.PATCH`);
  process.exit(1);
}
console.log(`version ${version}`);
