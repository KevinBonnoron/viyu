# Changelog

## Unreleased

### Fixed

* Keep video-grid skeletons until local watch history, reactions and display settings are ready; retain checked cards while subsequent pages load.
* Wait for the subscription resume shelf before displaying the timeline, so resumable videos do not briefly appear in both lists.
* Hide playback controls and their keyboard handlers while end-of-video suggestions or the next-episode screen are displayed; restore them on replay.
* Keep subscription video and series views mounted across tab switches, preserve hidden virtual-grid measurements, and retain explicit `tab=videos` / `tab=series` URLs.

## [0.1.0](https://github.com/KevinBonnoron/viyu/compare/v0.0.1...v0.1.0) (2026-11-15)


### Features

* initial commit ([b01d591](https://github.com/KevinBonnoron/viyu/commit/b01d591de14a38c05d2455f0e132c3eb4baa94fd))
