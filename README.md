# setup-python

Install Python packages using the detected (or explicitly selected) package
manager, restoring/saving its venv and pypi cache directories to the
CircleCI job cache around the install. Also builds distribution packages via
`setup.py` or PEP 517 `python -m build`. A 1:1 port of the
[`circleci/python`](https://circleci.com/developer/orbs/orb/circleci/python)
orb's `install-packages` and `dist` commands.

Invoked via:

```
circleci run setup-python@<version> install-packages [flags]
circleci run setup-python@<version> dist [flags]
```

## install-packages

### What it does

1. Resolves the `python`/`pip` binaries to use: prefers `python`/`pip` on
   `PATH`, falls back to `python3`/`pip3`. If it falls back, writes a
   `python`/`pip` → `python3`/`pip3` alias to `$BASH_ENV` so bare `python`/
   `pip` calls in later steps of the same job still resolve.
2. Detects the package manager from files in `--app-dir` (or uses
   `--pkg-manager` if set to something other than `auto`):
   - `requirements.txt` + (`setup.py` present) → `pip-dist`
   - `requirements.txt` alone → `pip`
   - `Pipfile` → `pipenv`
   - `uv.lock` → `uv`
   - `pyproject.toml` → `poetry`
3. Unless `--no-cache`, computes a cache key and restores the manager's venv/
   pypi cache paths from the CircleCI job cache.
4. Runs the install command for the detected/selected package manager.
5. Unless `--no-cache`, saves the venv/pypi cache paths back to the CircleCI
   job cache under the same key.

### Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--app-dir` | string | `.` | Directory containing the Python project |
| `--args` | string | `""` | Extra arguments appended to the install command |
| `--cache` / `--no-cache` | bool | `true` | Restore the venv/pypi caches before install and save them afterwards. `--no-cache` skips both restore and save entirely |
| `--cache-key-override` | string | `""` | Use this value verbatim as the cache key instead of the computed one |
| `--cache-version` | string | `v1` | Leading component of the computed cache key; bump to force a cache miss |
| `--include-branch-in-cache-key` / `--no-include-branch-in-cache-key` | bool | `true` | Include `$CIRCLE_BRANCH` in the computed cache key |
| `--include-python-in-cache-key` / `--no-include-python-in-cache-key` | bool | `true` | Include the resolved `python --version` output in the computed cache key |
| `--path-args` | string | `.` | Path passed to `pip install -e` for `--pkg-manager=pip-dist` |
| `--pip-dependency-file` | string | `requirements.txt` | pip/pip-dist dependency file; also doubles as their cache-key lockfile |
| `--pkg-manager` | enum | `auto` | Package manager to use: `auto`, `poetry`, `pipenv`, `pip`, `pip-dist`, or `uv`. Auto-detected from project files when `auto` |
| `--pypi-cache` / `--no-pypi-cache` | bool | `true` | Include the pypi package/download cache in `--cache`'s restore/save, when `--cache` is true |
| `--venv-cache` / `--no-venv-cache` | bool | `true` | Include the venv cache in `--cache`'s restore/save, when `--cache` is true. No-op for `pip`/`pip-dist`, which have no venv |
| `--venv-path` | `[]string` (repeatable) | _(none)_ | Override the detected venv path(s) to cache. Pass multiple times for multiple paths |

### Install commands

Each package manager's install command, per the detected/selected manager:

| Package manager | Command |
|---|---|
| `pip` | `pip install -r <pip-dependency-file> <args>` |
| `pip-dist` | `pip install -e <path-args> <args>` |
| `pipenv` | `pipenv install <args>` |
| `poetry` | `poetry install --no-ansi <args>` |
| `uv` | `uv sync <args>` |

## dist

### What it does

Resolves the `python`/`pip` binaries (same alias behavior as
`install-packages`) and builds a distribution package either via
`setup.py sdist bdist_wheel` (`--build-tool=wheel`, the default) or via PEP
517 `python -m build` (`--build-tool=build`):

| Build tool | Commands run |
|---|---|
| `wheel` | `pip install wheel`, then `python setup.py sdist bdist_wheel --dist-dir <out-dir>` |
| `build` | `pip install build`, then `python -m build --outdir <out-dir> [--sdist] [--wheel] [--skip-dependency-check] [--no-isolation] .` |

### Flags

| Flag | Type | Default | Description |
|---|---|---|---|
| `--app-dir` | string | `.` | Directory containing the Python project |
| `--build-tool` | enum | `wheel` | Build backend to use: `wheel` or `build` |
| `--no-isolation` | bool | `false` | `--build-tool=build` only — passes `--no-isolation` to `python -m build` |
| `--out-dir` | string | `./dist` | Directory to write the built distribution(s) to |
| `--sdist-separate` | bool | `false` | `--build-tool=build` only — passes `--sdist` to `python -m build` |
| `--skip-dependency-check` | bool | `false` | `--build-tool=build` only — passes `--skip-dependency-check` to `python -m build` |
| `--wheel-separate` | bool | `false` | `--build-tool=build` only — passes `--wheel` to `python -m build` |

## Caching

`install-packages` caches two independent buckets per package manager — the
venv (`--venv-cache`) and the pypi package/download cache (`--pypi-cache`) —
both consulted only when `--cache` is true:

| Package manager | venv path(s) (`--venv-cache`) | pypi path(s) (`--pypi-cache`) | lockfile (cache key + `--pip-dependency-file`) |
|---|---|---|---|
| `pip` / `pip-dist` | _(none — pip has no venv concept)_ | `~/.cache/pip`, `~/.pyenv/versions`, `~/.local/lib` | `--pip-dependency-file` (default `requirements.txt`) |
| `pipenv` | `$WORKON_HOME`, else `~/.local/share/virtualenvs` (or `<app-dir>/.venvs` if `PIPENV_VENV_IN_PROJECT` is set) | `~/.cache/pip`, `~/.cache/pipenv` | `Pipfile.lock` |
| `poetry` | `~/.cache/pypoetry/virtualenvs` | `~/.cache/pip` | `poetry.lock` |
| `uv` | `<app-dir>/.venv` (or `$CIRCLE_WORKING_DIRECTORY/.venv` if set) | `~/.cache/uv` | `uv.lock` |

`--venv-path` overrides the detected venv path(s) for any manager (including
`pip`/`pip-dist`, which otherwise cache no venv at all).

Unless `--cache-key-override` is set (used verbatim instead), the cache key
is:

```
<cache-version>-cci_pycache-[<CIRCLE_BRANCH>-]-[<python --version output>-]-<sha256(lockfile)>
```

with each bracketed component included only when its corresponding flag
(`--include-branch-in-cache-key` / `--include-python-in-cache-key`) is set.
The lockfile hashed is the one from the table above — the pip/pip-dist row
reads `--pip-dependency-file` itself, since there's no dedicated lockfile
format for plain pip.

Cache errors — including running outside a CircleCI job, where they're
expected — are logged but never fail the command.

## Documented deviations

Relative to the `circleci/python` orb, ported 1:1 elsewhere:

1. **No way to run custom steps between cache restore and install** through
   `install-packages` alone. The orb's `pre-install-steps` parameter accepts
   a list of CircleCI steps to splice in between `restore_cache` and the
   install command; a function is a single process invocation, so there's no
   way to pause mid-run for caller-supplied steps, and `pre-install-steps` is
   not supported. Callers who need that ordering pass `--no-cache` (which
   skips both restore and save) and manage caching around their own steps
   with their own `save_cache`/`restore_cache` steps or their own
   `circleci run <cache-tool>` calls instead.
2. **`--venv-path` is a repeatable flag**, not the orb's JSON-array-in-a-string
   (e.g. `venv-path: '[ "p1", "p2" ]'`). Pass `--venv-path p1 --venv-path p2`
   for multiple paths instead of encoding a JSON array as a string value.

## Examples

```bash
# Auto-detect the package manager from files in the current directory
circleci run setup-python@<version> install-packages

# pip, explicit dependency file and app directory (monorepo)
circleci run setup-python@<version> install-packages --pkg-manager pip --app-dir ./service --pip-dependency-file requirements-dev.txt

# poetry, skip caching entirely (e.g. because custom steps run in between)
circleci run setup-python@<version> install-packages --pkg-manager poetry --no-cache

# uv, override the venv path(s) to cache
circleci run setup-python@<version> install-packages --pkg-manager uv --venv-path ./.venv --venv-path ./tools/.venv

# Build a wheel/sdist via setup.py (the default build tool)
circleci run setup-python@<version> dist

# Build via PEP 517, sdist and wheel as separate artifacts
circleci run setup-python@<version> dist --build-tool build --sdist-separate --wheel-separate --out-dir ./dist
```

## Platform support

Linux, darwin and Windows. `install-packages` and `dist` run on all three.

Cache locations are resolved per platform, following each tool's own
documented default — e.g. pip's cache is `~/.cache/pip` on Linux,
`~/Library/Caches/pip` on macOS and `%LocalAppData%\pip\Cache` on Windows.
The paths in the [Caching](#caching) table above are the Linux ones; the
macOS and Windows equivalents are used automatically when running there.

## Tests

Three layers, split by what they need rather than by what they cover. All three
run the CLI (or its packages) through the toolkit's `sdktest.Sandbox`, so
`$HOME`, `$BASH_ENV` and the cache are always temp locations — a test can never
install into your real home directory or reach a cache it did not set up.

- **Unit** (`python/`) — the package logic: manager detection, cache path/key
  computation, install-command construction. Run by `task test`.
- **CLI contract** (`clitest/`, no build tag) — the compiled binary's flag
  surface, error paths, and cache-driving contract, run against a fresh
  in-memory cache. Hermetic: each run's `PATH` holds only no-op `python`/`pip`/
  package-manager stubs, so `task test` needs no real tool and no network. Run
  by `task test`.
- **Acceptance** (`acceptance/`, `//go:build acceptance`) — real installs and
  dist builds against the real package managers, plus a real cache round trip.
  Run by `task acceptance`; safe to run locally (a manager missing from `PATH`
  skips its case).

```bash
task test        # unit + CLI contract — fast, hermetic, every commit
task acceptance  # real package managers (network); a live cache when in a job
```

`task acceptance` uses an in-memory cache locally. In CI the `acceptance` job
sets `TOOLKIT_CACHE_MODE=live` to exercise CircleCI's own cache — and fails
loudly rather than silently falling back if it can't reach one. The cache
round trip feeds the sandbox's per-run nonce to `--cache-version` so the
write-once cache key is unique to each run, making the save-then-restore exact
against the live cache. A small CI `smoke-test` job runs the shipped `./dist`
artifact end to end — the one thing the in-process suite can't prove: the
cross-compiled binary works and the `python`/`pip` alias it writes to
`$BASH_ENV` is sourced into a later job step by the real runner.
