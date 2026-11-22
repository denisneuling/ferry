# Contributing

Thanks for taking the time.

## How this repository works

ferry is developed in a private Forgejo instance and push-mirrored here.
Issues and pull requests opened on GitHub are carried into Forgejo by
[forgejo-github-bridge](https://github.com/metapatrol/forgejo-github-bridge), where the maintainers work, and everything
they say comes back here. From your side it is an ordinary GitHub project; a
bot account posts the maintainers' replies, naming who wrote them.

## Issues

Open an issue for bugs, questions and ideas. Please include the version, the
SMB server you use (software and version, not its address), what you
expected, what happened, and the relevant output — with user names, host
names and paths removed where they are private. ferry never logs passwords;
if you ever see one in its output, that is a security problem: see
[SECURITY.md](SECURITY.md).

## Pull requests

- Target **`develop`**. Pull requests against other branches are not picked up.
- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:` and
  `fix:` make a release, `chore:`, `ci:`, `docs:` and `build:` do not.
  Versions and the changelog are derived from them.
- Run the checks before pushing: the README's *Quick start* has the commands.
- Explain the why in the commit body when the diff does not.
- Keep to the ground rules in [CLAUDE.md](CLAUDE.md) — above all: Samba
  decides every access, nothing is persisted but the optional cache of
  derived data, and credentials never reach a
  log, trace, metric or error.
- Use fictional data in tests, examples and screenshots (`alex`,
  `nas.example.lan`), never a real server or person.
- If you change CI configuration (`.forgejo/`, `.github/`), say why in the
  description. Such pull requests are read with particular care before any
  workflow of theirs runs.

### What happens after you open one

1. Your branch is imported into the maintainers' Forgejo. CI runs there once a
   maintainer has read your changes and approved the run.
2. Review comments appear on your pull request, posted by the bot and
   attributed to the maintainer.
3. When it is merged, the bot says so; your authorship is kept.

The brand assets in `.metapatrol/` are generated: `npx @metapatrol/brand-kit init`
rewrites them from `metapatrol.json`. Edit `metapatrol.json`, not the files.
