# Changelog

## Unreleased

### Features

* Discover videos outside subscriptions through cached public searches based on each profile's likes and subscription videos, without platform cookies; retain relevance order, diversify channels and apply discovery visibility preferences before the feed limit.

### Fixed

* Keep ranked API positions for home-feed videos also returned by connected sources, retaining the connected session on each card and deduplicating paginated source results.
* Preserve eligible cached subscription videos during public-search outages, and filter hidden subscriptions before the bounded candidate limit while keeping unfiltered discovery seeds and live-history exceptions.
* Ground discovery topics in source video titles instead of raw tag repetition, so descriptive language tags on unrelated videos do not generate language-learning recommendations; preserve genuine language-learning interests.
* Keep video-grid skeletons until local watch history, reactions and display settings are ready; retain checked cards while subsequent pages load.
* Wait for the subscription resume shelf before displaying the timeline, so resumable videos do not briefly appear in both lists.
* Hide playback controls and their keyboard handlers while end-of-video suggestions or the next-episode screen are displayed; restore them on replay.
* Keep subscription video and series views mounted across tab switches, preserve hidden virtual-grid measurements, and retain explicit `tab=videos` / `tab=series` URLs.
* Show a retryable error when display preferences fail to load instead of leaving video grids on permanent skeletons.
* Continue empty-feed pagination after a status-query error only when known filters exclude every candidate; keep pending or unchecked videos withheld.
* Reset active seek gestures when the end screen appears so replay remains seekable even without pointer-capture cleanup.
* Fetch YouTube and PeerTube comment replies, retaining replies to the last included YouTube thread and bounding PeerTube reply requests.
* Enforce YouTube reply limits after extraction as well, so inline replies cannot exceed five per thread or fifty in total; orphan replies do not consume the budget.
* Collapse long comments and replies to four lines with show-more/show-less controls, while keeping short messages at their natural height.
* Refine comment metadata and reply threads with singular/plural labels, a stable text-and-chevron toggle, a subtly tinted reply group and no mobile indent.
* Keep the visible reply count as the toggle's accessible name in both states, so speech-input users can activate it by its label.
* Align quick-search tab labels and their keyboard shortcuts on the same text baseline.

## [0.4.0](https://github.com/KevinBonnoron/viyu/compare/v0.3.0...v0.4.0) (2026-11-28)


### Features

* **client:** preserve relevance order in the discovery feed ([e3814f9](https://github.com/KevinBonnoron/viyu/commit/e3814f93bdac938b38ede9e3d004a3361037dc8a))
* **feed:** discover public videos from grounded profile interests ([258199a](https://github.com/KevinBonnoron/viyu/commit/258199a2cd92c126db765cab32394bb29e7d6236))


### Bug Fixes

* **feed:** preserve API ranking for connected-source duplicates ([0dfb663](https://github.com/KevinBonnoron/viyu/commit/0dfb663ee76d6d8a7a18a68af430dae8b65af6ba))
* **feed:** select eligible cached candidates before discovery ([c1b8b79](https://github.com/KevinBonnoron/viyu/commit/c1b8b7917742805ab4efd49d1862e065cc03fcaa))

## [0.3.0](https://github.com/KevinBonnoron/viyu/compare/v0.2.0...v0.3.0) (2026-11-24)


### Features

* **catalog:** share chronological sorting across videos and series ([ea0c589](https://github.com/KevinBonnoron/viyu/commit/ea0c589f7d9a88c85d47bda7cdc9d2c7087494f5))
* **channel:** extract and display provider metadata ([483bd5d](https://github.com/KevinBonnoron/viyu/commit/483bd5d2bbf52f98d0d7e059cc34f215ad9846cf))
* **channel:** paginate catalogs and preserve tab content ([e0970f8](https://github.com/KevinBonnoron/viyu/commit/e0970f8fb2e793926680880f51012ad33bf07805))
* **player:** confirm sponsor skips during loading ([b5c0871](https://github.com/KevinBonnoron/viyu/commit/b5c0871bd8544780eda732daf8cd565c16cead83))
* **player:** play at double speed while holding ([d3f7471](https://github.com/KevinBonnoron/viyu/commit/d3f7471ab13144f741397c5ba75a87105ffe4aaf))
* **player:** show video title with fullscreen controls ([3ffae27](https://github.com/KevinBonnoron/viyu/commit/3ffae274bcd0ff1262b98ff41975eed9719da65d))
* **recommendations:** prioritize series continuation and publication context ([4a4e445](https://github.com/KevinBonnoron/viyu/commit/4a4e4458ce1e6a43190eb3fb0ddb64ca5c08b2e1))


### Bug Fixes

* **brand:** center and refine the play mark ([d0d78e4](https://github.com/KevinBonnoron/viyu/commit/d0d78e4df885b49a38ce5d7ca2f6bffd52d44cd8))
* **catalog:** reconcile pagination as indexed metadata changes ([b3b6053](https://github.com/KevinBonnoron/viyu/commit/b3b6053192380a01e35cfae66cd778a940853976))
* **channel:** keep mobile headers readable ([2301638](https://github.com/KevinBonnoron/viyu/commit/2301638fa810f10e83462edb6e76f29bfd76d03b))
* **channel:** match loading skeletons to the channel profile ([8de5e41](https://github.com/KevinBonnoron/viyu/commit/8de5e418dee2156580eb75b47355d2fc748dc52d))
* **channel:** retain loading state and filtered-page pagination ([7a4dd80](https://github.com/KevinBonnoron/viyu/commit/7a4dd807c0143549d541e17330c62d8769b37425))
* **media:** preserve resource identity across HLS recovery ([aaccd41](https://github.com/KevinBonnoron/viyu/commit/aaccd419cb83774c22f5a592a3e4786578a6b6fc))
* **media:** recover signed streams and retain long-playback resources ([833db39](https://github.com/KevinBonnoron/viyu/commit/833db39479cf5d8dddb11fd249e27fdc5123e0e2))
* **media:** renew refused intermediate playlists safely ([85867f8](https://github.com/KevinBonnoron/viyu/commit/85867f86634a47ff8cec821d3a5ec67949c470a6))
* **player:** adapt side panels to touch screens and available height ([6f5fb0a](https://github.com/KevinBonnoron/viyu/commit/6f5fb0a137bf790f400d590fd5f356bbff21c037))
* **player:** align channel metadata and actions in narrow layouts ([48c32c0](https://github.com/KevinBonnoron/viyu/commit/48c32c03435068b146f869e8692798cd009ac565))
* **player:** apply quality selections and display the active rendition ([438efc1](https://github.com/KevinBonnoron/viyu/commit/438efc1a05d220b1e86aeeafadbf5d8df67e97a6))
* **player:** keep panel placement stable while scrolling ([4ab78a8](https://github.com/KevinBonnoron/viyu/commit/4ab78a81def4638359fe59fad441248ead95054a))
* **player:** lower the side-panel width breakpoint ([fc41831](https://github.com/KevinBonnoron/viyu/commit/fc4183171ccab0528eed7941168dbf6a3405a11d))
* **player:** seek on double clicks and touch taps ([edc1e14](https://github.com/KevinBonnoron/viyu/commit/edc1e14f359c1463c9ae73ac480f3d524130a945))
* **player:** unify share and reaction button sizes ([81871af](https://github.com/KevinBonnoron/viyu/commit/81871af69e2bb7515c6ac82e038d7b04a09a3f1e))
* **recommendations:** exclude ineligible videos before candidate limits ([9d1a077](https://github.com/KevinBonnoron/viyu/commit/9d1a077ac111d684910456f081fd89aca7da355c))
* **search:** show publication dates in quick results ([9647b7d](https://github.com/KevinBonnoron/viyu/commit/9647b7d809793e38412199c149b2ea2fc8e17e51))
* **ui:** show one skeleton row while loading additional videos ([598dd29](https://github.com/KevinBonnoron/viyu/commit/598dd293b853d9aebe09afb7a34f8c23b34e12ac))

## [0.2.0](https://github.com/KevinBonnoron/viyu/compare/v0.1.0...v0.2.0) (2026-11-22)


### Features

* offer the unfinished videos first in the feed and subscriptions ([9ecad17](https://github.com/KevinBonnoron/viyu/commit/9ecad171fb99829e8aefefa84fcaf0c7c82ed089))
* release the server and desktop for Linux, macOS and Windows ([9c762d2](https://github.com/KevinBonnoron/viyu/commit/9c762d2b9b040ed2d9f64f1a2ae709cb6ed1fedc))


### Bug Fixes

* align the sidebar collapse toggle with the other footer buttons ([cb8daa2](https://github.com/KevinBonnoron/viyu/commit/cb8daa264144226d36b88e1266ea2e3b176ce6a4))
* **comments:** fetch YouTube and PeerTube replies ([4dcf480](https://github.com/KevinBonnoron/viyu/commit/4dcf48092e69c7266fdf9c9bffd974c1f25f71a0))
* **comments:** refine expandable messages and reply threads ([9a4caf1](https://github.com/KevinBonnoron/viyu/commit/9a4caf1f3850f415f6e212ca11aaf4f902317e87))
* handle video grid failures and reset ended seek gestures ([7c56569](https://github.com/KevinBonnoron/viyu/commit/7c565691c2949f7cfc527ce85fa56773caeaa120))
* keep playback awake and preserve pasted-link navigation ([cb984b3](https://github.com/KevinBonnoron/viyu/commit/cb984b334be5c903d06676c61d29ff81b616b2f9))
* prevent subscription flicker and tab reloads ([f802171](https://github.com/KevinBonnoron/viyu/commit/f8021712a6718ee06c5465d88108e9f23a7f9bc5))
* **search:** align tab labels and keyboard shortcuts ([1c539c3](https://github.com/KevinBonnoron/viyu/commit/1c539c39baf08fe6017cb2462eef065b05d2d1b6))

## [0.1.0](https://github.com/KevinBonnoron/viyu/compare/v0.0.1...v0.1.0) (2026-11-15)


### Features

* initial commit ([b01d591](https://github.com/KevinBonnoron/viyu/commit/b01d591de14a38c05d2455f0e132c3eb4baa94fd))
