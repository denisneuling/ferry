# Changelog

What changed in each release of ferry, written by semantic-release from the
Conventional Commits since the one before. Until 1.0, a minor version may
change the API, the settings or the chart's values; such changes are listed
under BREAKING CHANGES.

## [0.5.1](https://github.com/metapatrol/ferry/compare/v0.5.0...v0.5.1) (2026-11-22)

### Bug Fixes

* **release:** write the HEIF assets into the directory asked for ([3bf9a69](https://github.com/metapatrol/ferry/commit/3bf9a69b56a31dc997ef018735ccb0af1b95d9b9))

## [0.5.0](https://github.com/metapatrol/ferry/compare/v0.4.1...v0.5.0) (2026-11-22)

### Features

* show a PDF's first page as its thumbnail ([9a115b4](https://github.com/metapatrol/ferry/commit/9a115b426df23655927a34874b3cfc343ee69fb3))
* show HEIC photos as thumbnails and in the preview ([41f2436](https://github.com/metapatrol/ferry/commit/41f243627e66f8e75cf19202cd77e14732d59768))

## [0.4.1](https://github.com/metapatrol/ferry/compare/v0.4.0...v0.4.1) (2026-11-22)

### Bug Fixes

* **chart:** roll the pod with the cache on instead of recreating it ([83f0dc6](https://github.com/metapatrol/ferry/commit/83f0dc6793d78205f082c1645c859701ea740fcc))

## [0.4.0](https://github.com/metapatrol/ferry/compare/v0.3.0...v0.4.0) (2026-11-22)

### Features

* a cache of thumbnails and details in a directory ([ad45120](https://github.com/metapatrol/ferry/commit/ad451200f7be3e2faf200a8f955895febc4a3446))
* keep thumbnails and details in an optional cache on a volume ([cd8f6a4](https://github.com/metapatrol/ferry/commit/cd8f6a4a21d90907262c011bed376d86cc73b5f8))
* show the cover a recording carries as its thumbnail ([dfc8062](https://github.com/metapatrol/ferry/commit/dfc8062a6abafd9374598b64472bd653af546a45))

## [0.3.0](https://github.com/metapatrol/ferry/compare/v0.2.0...v0.3.0) (2026-11-22)

### Features

* a few SMB connections per login, one kept for browsing ([1cc0d38](https://github.com/metapatrol/ferry/commit/1cc0d386f6358485e605e90120697211b93e358b))
* download several entries or folders as one ZIP ([20daf8b](https://github.com/metapatrol/ferry/commit/20daf8b4843ee45026dbaf8e78a1d073f870e63d))
* keep thumbnails and details in the browser until the file changes ([31adac1](https://github.com/metapatrol/ferry/commit/31adac133a9af8d1ac7a6c7eefa4d076cf54652c))
* **ui:** load the next and the previous image ahead in the preview ([c112075](https://github.com/metapatrol/ferry/commit/c11207577e27ea2383780b2e2b6fcebccf8cecd0))
* **ui:** show a folder seen a moment ago at once when going back ([903a79d](https://github.com/metapatrol/ferry/commit/903a79df641387715a6b89093b64fc7e732dce89))

## [0.2.0](https://github.com/metapatrol/ferry/compare/v0.1.0...v0.2.0) (2026-11-22)

### Features

* show what a file says about itself in the details ([06a51d6](https://github.com/metapatrol/ferry/commit/06a51d6f625762832ac4d2fefc5c47dfa521c617))
* **ui:** show a share's free space, as the server reports it ([b93f30c](https://github.com/metapatrol/ferry/commit/b93f30c50d59d079f30103076b5005870ebd3bb6))
* **ui:** the free-space bar says what it shows ([39c7e2a](https://github.com/metapatrol/ferry/commit/39c7e2a7e08152a65af1020917cd2de0a1a2378f))

### Bug Fixes

* **deps:** regenerate the API code with oapi-codegen v2.9.0 ([e66ad0e](https://github.com/metapatrol/ferry/commit/e66ad0e88e5576eadddb175fb0f58bd5a9062d63))
* **deps:** update module github.com/oapi-codegen/oapi-codegen/v2 to v2.9.0 ([9291b17](https://github.com/metapatrol/ferry/commit/9291b170492869c7fe9345c8ffe1244b8529b309))
* **listing:** large folders open, streamed and rendered as they scroll ([47d7393](https://github.com/metapatrol/ferry/commit/47d73930e8439f5c1f218b8eb29f3387dd518e70))
* regenerate api/v1 after #22 and #25 crossed, and let the server test clean up ([4875753](https://github.com/metapatrol/ferry/commit/48757534f7349059f7556d63699539274e11d457))
* **release:** attach SHA256SUMS to the Forgejo release ([0ed4dc3](https://github.com/metapatrol/ferry/commit/0ed4dc36270bd24105a9f2891c4b4d8a255ce639))
* TestEndToEnd's cleanup signs in again ([fe7a111](https://github.com/metapatrol/ferry/commit/fe7a1111b2126129cb2d89bd95a7193014bcbf41))
* **ui:** actions on many entries say how many, and keep to the bounds ([b65e653](https://github.com/metapatrol/ferry/commit/b65e65371c50819ffd41f9c941106327fbb849b4))
* **ui:** ferry's own favicon and app icons ([3411a52](https://github.com/metapatrol/ferry/commit/3411a52d85f22b4290b06e10c744fea412125b16))
* **ui:** the ferry logo is a link to the shares ([339ec1a](https://github.com/metapatrol/ferry/commit/339ec1a60ded0eb630fbe6537e257e5c6ea3e164))

## [0.1.0](https://github.com/metapatrol/ferry/compare/v0.0.0...v0.1.0) (2026-11-21)

### Features

* first public release ([3c4f9b8](https://github.com/metapatrol/ferry/commit/3c4f9b8dbeda2a1986ae0b7b2f35b4a5db980dec))

### Bug Fixes

* **smb:** an abandoned request no longer ends the session ([5abb3f0](https://github.com/metapatrol/ferry/commit/5abb3f0b767f448b9be64d0f3ec18a90848eb579))
* **ui:** the filmstrip no longer cuts off its start on phones ([c64219e](https://github.com/metapatrol/ferry/commit/c64219e8fa94c7d0b80a430c2e453654bc788222))
* **ui:** the login's ferry stays clear of the text, the filmstrip shows its places ([0aa8268](https://github.com/metapatrol/ferry/commit/0aa826840300a60a5b4712268ea935471c109032))
