# Changelog

## Unreleased

### Fixed

* Keep video-grid skeletons until local watch history, reactions and display settings are ready; retain checked cards while subsequent pages load.
* Wait for the subscription resume shelf before displaying the timeline, so resumable videos do not briefly appear in both lists.
* Hide playback controls and their keyboard handlers while end-of-video suggestions or the next-episode screen are displayed; restore them on replay.
* Keep subscription video and series views mounted across tab switches, preserve hidden virtual-grid measurements, and retain explicit `tab=videos` / `tab=series` URLs.
* Show a retryable error when display preferences fail to load instead of leaving video grids on permanent skeletons.
* Continue empty-feed pagination after a status-query error only when known filters exclude every candidate; keep pending or unchecked videos withheld.
* Reset active seek gestures when the end screen appears so replay remains seekable even without pointer-capture cleanup.

## [0.2.0](https://github.com/KevinBonnoron/viyu/compare/v0.1.0...v0.2.0) (2026-11-20)


### Features

* offer the unfinished videos first in the feed and subscriptions ([9ecad17](https://github.com/KevinBonnoron/viyu/commit/9ecad171fb99829e8aefefa84fcaf0c7c82ed089))
* release the server and desktop for Linux, macOS and Windows ([9c762d2](https://github.com/KevinBonnoron/viyu/commit/9c762d2b9b040ed2d9f64f1a2ae709cb6ed1fedc))


### Bug Fixes

* align the sidebar collapse toggle with the other footer buttons ([cb8daa2](https://github.com/KevinBonnoron/viyu/commit/cb8daa264144226d36b88e1266ea2e3b176ce6a4))
* handle video grid failures and reset ended seek gestures ([7c56569](https://github.com/KevinBonnoron/viyu/commit/7c565691c2949f7cfc527ce85fa56773caeaa120))
* keep playback awake and preserve pasted-link navigation ([cb984b3](https://github.com/KevinBonnoron/viyu/commit/cb984b334be5c903d06676c61d29ff81b616b2f9))
* prevent subscription flicker and tab reloads ([f802171](https://github.com/KevinBonnoron/viyu/commit/f8021712a6718ee06c5465d88108e9f23a7f9bc5))

## [0.1.0](https://github.com/KevinBonnoron/viyu/compare/v0.0.1...v0.1.0) (2026-11-15)


### Features

* initial commit ([b01d591](https://github.com/KevinBonnoron/viyu/commit/b01d591de14a38c05d2455f0e132c3eb4baa94fd))
