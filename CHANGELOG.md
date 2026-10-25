# Changelog

All notable changes to setup-python will be documented in this file. Each release's section is added
by its release PR (see [RELEASE.md](RELEASE.md)) and becomes its GitHub release notes.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] - 2026-10-25

### What's Changed
* Stop passing a GitHub token to CI to download the toolkit by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/27
* Keep pip-installed commands on PATH after a cache hit by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/29
* Install a missing uv, Poetry or pipenv before install-packages runs it by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/30


**Full Changelog**: https://github.com/circleci-functions/setup-python/compare/v0.4.0...v0.5.0

## [0.4.0] - 2026-10-22

### What's Changed
* Update module golang.org/x/mod to v0.40.0 [SECURITY] by @renovate[bot] in https://github.com/circleci-functions/setup-python/pull/14
* Update toolkit to v0.3.0 by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/25
* Let install-packages carry on when linking or caching can't by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/23
* Lead the README with the functions block and fill its gaps by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/24

### New Contributors
* @renovate[bot] made their first contribution in https://github.com/circleci-functions/setup-python/pull/14

**Full Changelog**: https://github.com/circleci-functions/setup-python/compare/v0.3.0...v0.4.0

## [0.3.0] - 2026-10-13

### What's Changed
* Release by merging a release PR by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/11
* Set the code owner team by @stiyyagura0901 in https://github.com/circleci-functions/setup-python/pull/13


**Full Changelog**: https://github.com/circleci-functions/setup-python/compare/v0.2.0...v0.3.0

## [0.2.0] - 2026-10-07

The last release made by pushing a tag, published as `v0.2.0-d057988`. Earlier releases are in
the repo's [tags](https://github.com/circleci-functions/setup-python/tags).
