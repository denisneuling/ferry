# Changelog

What changed in each release of ferry, written by semantic-release from the
Conventional Commits since the one before. Until 1.0, a minor version may
change the API, the settings or the chart's values; such changes are listed
under BREAKING CHANGES.

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
