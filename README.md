# setup-python

Installs your Python dependencies with the right package manager and caches
what it downloads, so later steps start from a warm cache.

```yaml
steps:
  - run:
      name: Install dependencies
      command: circleci run setup-python@<version> -- --app-dir ./service
  - run: pytest
```

setup-python detects the package manager from the files in `--app-dir`, restores
its virtualenv and pypi caches, runs the install, and saves the caches back. If
the executor only has `python3`/`pip3` (no bare `python`/`pip`), it writes a
`python`→`python3` alias to `$BASH_ENV` so later steps still resolve `python`.

## Choosing a package manager

By default setup-python detects the package manager from the files present in
`--app-dir`:

| Files | Package manager |
|-------|-----------------|
| `requirements.txt` + `setup.py` | pip-dist |
| `requirements.txt` alone | pip |
| `Pipfile` | pipenv |
| `uv.lock` | uv |
| `pyproject.toml` | poetry |

They are matched in that order (first match wins). Set `--package-manager` to pick
one explicitly instead:

```yaml
- run: circleci run setup-python@<version> -- --package-manager poetry
```

Each manager runs its standard install command; `--args` is appended verbatim to
any of them:

| Package manager | Command |
|-----------------|---------|
| pip | `pip install -r <pip-dependency-file>` |
| pip-dist | `pip install -e <path-args>` |
| pipenv | `pipenv install` |
| poetry | `poetry install --no-ansi` |
| uv | `uv sync` |

## Caching

Caching is on by default. setup-python restores two independent buckets per
manager before installing and saves them back afterwards — the virtualenv
(`--venv-cache`) and the pypi package/download cache (`--pypi-cache`):

| Package manager | venv path(s) | pypi path(s) | lockfile |
|-----------------|--------------|--------------|----------|
| pip / pip-dist | _(none — pip has no venv)_ | `~/.cache/pip`, `~/.pyenv/versions`, `~/.local/lib` | `--pip-dependency-file` |
| pipenv | `$WORKON_HOME`, else `~/.local/share/virtualenvs` (or `<app-dir>/.venvs` when `PIPENV_VENV_IN_PROJECT` is set) | `~/.cache/pip`, `~/.cache/pipenv` | `Pipfile.lock` |
| poetry | `~/.cache/pypoetry/virtualenvs` | `~/.cache/pip` | `poetry.lock` |
| uv | `<app-dir>/.venv` (or `$CIRCLE_WORKING_DIRECTORY/.venv`) | `~/.cache/uv` | `uv.lock` |

The cache key is computed from that manager's lockfile — so a dependency change
refreshes the cache on its own — plus `$CIRCLE_BRANCH` and the Python version by
default:

```
<cache-version>-cci_pycache-[<CIRCLE_BRANCH>-]-[<python --version>-]-<sha256(lockfile)>
```

Drop a component with `--include-branch-in-cache-key=false` /
`--include-python-in-cache-key=false`, or replace the whole key with
`--cache-key-override`.

Turn one bucket off with `--venv-cache=false` / `--pypi-cache=false`, or all
caching with `--cache=false`. Point `--venv-path` at a directory (repeatable) to
override the detected venv path(s) for any manager. Cache problems never fail the
job.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--app-dir` | `.` | Directory containing the Python project |
| `--package-manager` | `auto` | `auto`, `poetry`, `pipenv`, `pip`, `pip-dist` or `uv`. Auto-detected from project files when `auto`. |
| `--args` | (empty) | Extra arguments appended to the install command |
| `--pip-dependency-file` | `requirements.txt` | pip/pip-dist dependency file; also their cache-key lockfile |
| `--path-args` | `.` | Path passed to `pip install -e` for `--package-manager=pip-dist` |
| `--cache` | true | Set `--cache=false` to disable caching |
| `--venv-cache` | true | Include the virtualenv cache. No-op for pip/pip-dist. |
| `--pypi-cache` | true | Include the pypi package/download cache |
| `--venv-path` | (none) | Override the detected venv path(s) to cache. Repeatable. |
| `--cache-version` | `v1` | Leading component of the cache key; bump to force a miss |
| `--cache-key-override` | (empty) | Use this value verbatim as the cache key |
| `--include-branch-in-cache-key` | true | Include `$CIRCLE_BRANCH` in the cache key |
| `--include-python-in-cache-key` | true | Include the resolved `python --version` in the cache key |

## Platform support

Linux, macOS and Windows. Cache locations are resolved per platform following
each tool's own default — e.g. pip's cache is `~/.cache/pip` on Linux,
`~/Library/Caches/pip` on macOS and `%LocalAppData%\pip\Cache` on Windows (the
paths in the [Caching](#caching) table are the Linux ones).

On Windows the `python`/`pip` alias is written for a bash shell, so later steps
need Git Bash or MSYS2 to pick it up.
