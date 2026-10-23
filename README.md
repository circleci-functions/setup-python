# setup-python

Installs Python and puts `python` and `pip` on `PATH` for the rest of the job.
An optional `install-packages` command then installs your dependencies with
the right package manager, caching what it downloads.

```yaml
functions:
  setup-python: github.com/circleci-functions/setup-python@<version>

jobs:
  test:
    docker:
      - image: cimg/base:stable
    steps:
      - checkout
      - setup-python:
          with:
            python-version: "3.13"
      - setup-python/install-packages:
          with:
            app-dir: ./service
      - run: pytest
```

`- setup-python:` runs the default command, which installs Python, and
`- setup-python/install-packages:` runs `install-packages`. Each flag in the
tables below is a `with:` key, written without its leading `--`.

Where the `functions:` block isn't enabled, such as on CircleCI Server, run the
same commands with `circleci run` and the function's full name:

```yaml
- run: circleci run github.com/circleci-functions/setup-python@<version> -- --python-version 3.13
- run: circleci run github.com/circleci-functions/setup-python@<version> -- install-packages --app-dir ./service
```

## Installing Python

With no command, setup-python downloads a CPython build for the machine it
runs on, checks it against its SHA-256, and links `python`, `python3`,
`python3.X`, `pip`, `pip3` and `pip3.X` into a directory already on `PATH`.
Later steps run them by name, in any shell, without sourcing anything. If they
can't be linked, the step fails, since later steps could not run the Python it
installed.

The builds are [python-build-standalone](https://github.com/astral-sh/python-build-standalone)'s,
the relocatable CPython builds uv installs. They depend on nothing but the C
library, so the same build runs on any Linux distro, Alpine included.

### Choosing a version

`--python-version` takes any of:

| Spec | Installs |
|------|----------|
| `3.13.1`, `3.15.0rc2` | exactly that release |
| `3`, `3.x`, `3.13`, `3.13.x` | the newest matching stable release |
| `>=3.10 <3.14`, `^3.11`, `~3.12.1`, `3.11 \|\| 3.12` | the newest release in the range |
| `>=3.10,<3.14`, `~=3.11`, `==3.12.*` | the same, written as in `requires-python` |
| `3.14-dev` | the newest release of 3.14, prereleases included |
| `3.13t` | a free-threaded build, as `--freethreaded` asks for |

A range matches stable releases only. `--allow-prereleases` lets a `3.15`-style
spec fall back to prereleases while that version has no stable release yet.

Or read the version from a file with `--python-version-file`:

| File | Read from |
|------|-----------|
| `.python-version` | the first line that is not a comment |
| `.tool-versions` | the `python` line |
| `pyproject.toml` | `project.requires-python`, then `tool.poetry.dependencies.python` |
| `Pipfile` | `requires.python_full_version`, then `requires.python_version` |

```yaml
- setup-python:
    with:
      python-version-file: pyproject.toml
```

With neither flag, setup-python reads `.python-version` from the step's
working directory. With no `.python-version` either, it uses the Python 3
already on `PATH` and installs nothing, linking `python` to `python3` if only
`python3` resolves, and only warning if that link fails. With no Python on
`PATH` at all, it installs the newest stable release. `--python-version` wins
when both flags are set.

### pyenv

Where `python` is already on `PATH` through pyenv's shims, as on CircleCI's
machine, macOS and `cimg/python` images, setup-python registers the install
with pyenv instead of linking it: it is added to `$(pyenv root)/versions`
under its version (`3.13.1`, or `3.13.1t` for a free-threaded build), made
pyenv's global version, and the shims rehashed. pyenv then runs it, and
rehashes after `pip install`, so what pip installs resolves by name too.

A `.python-version` file or `$PYENV_VERSION` takes precedence over pyenv's
global version. Where either selects pyenv's own install of the version asked
for, as on an image that ships it, pyenv keeps running that one, and
`--pip-version` applies to it. If either selects any other Python where
setup-python runs, it fails rather than leave later steps running a Python
other than the one asked for.

### Where it links

setup-python links into the first of `~/.local/bin`, `~/bin` and
`/usr/local/bin` that is on `PATH` ahead of any Python already there and that
it can write to. Failing that, it links into the directory of the Python
already on `PATH`, replacing it there, or, with no Python on `PATH`, into the
first directory on `PATH` it can write to, then `/usr/local/bin`. Where
another `pip` comes ahead on `PATH` from a directory it can write to,
setup-python links its own `pip` there too. A binary it replaces is kept as
`<name>.pathlink-old` in the same directory.

Where that directory needs root, as for a preinstalled `/usr/bin/python3`,
setup-python links with `sudo -n`, so without passwordless sudo the step
fails. On macOS system directories sudo is skipped. Put `~/.local/bin` on
`PATH` ahead of that directory, or name a directory with `--bin-dir`:
setup-python links into exactly that one, without searching `PATH` or
registering with pyenv, so it has to be on `PATH` for later steps.

setup-python checks whether it can link before downloading, so a job that
could never use the installed Python fails immediately with what to change.

### Where the list of builds comes from

Each release of setup-python has the list of builds from
[uv's download metadata](https://github.com/astral-sh/uv/blob/main/crates/uv-python/download-metadata.json)
built in, so resolving a version needs no network. Only when no built-in build
matches — a Python release newer than your setup-python — does it fetch uv's
current list. A partial version such as `3.13` resolves to the newest release
in the built-in list; upgrade setup-python to pick up newer ones.

### pip installs

Executables from `pip install` land on `PATH` too, where they can: when the
directory setup-python linked `python` into is pip's user scripts directory
(`~/.local/bin` on Linux and macOS), setup-python tells the installed Python's
pip to install for the user, so `pip install poetry` puts `poetry` beside
`python`. Virtual environments made from that Python are unaffected.

Installing for the user rules out `pip install --target` and `--prefix`; pass
`--no-user` alongside them. Elsewhere, including on Windows and when
`~/.local/bin` is not on `PATH`, setup-python leaves pip alone and warns that
what pip installs will not be on `PATH`: run it with `python -m` instead.

`--pip-version` installs a specific pip into the installed Python.

### Caching the install

The unpacked install is saved to the job cache under a key made of the Python
version and platform, so later jobs restore it rather than downloading it
again. A cache problem never fails the job. Turn it off with `--cache=false`.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--python-version` | (see above) | Version spec to install |
| `--python-version-file` | (empty) | File to read the version spec from |
| `--allow-prereleases` | false | Let a `major.minor` spec match prereleases when there is no stable release |
| `--freethreaded` | false | Install a free-threaded build |
| `--pip-version` | (empty) | Version of pip to install into the installed Python |
| `--bin-dir` | (empty) | Link `python` and `pip` into this directory instead of discovering one on `PATH` |
| `--cache` | true | Set `--cache=false` to disable caching the install |

## Installing dependencies

`install-packages` detects the package manager from the files in `--app-dir`,
restores its virtualenv and pypi caches, runs the install, and saves the caches
back.

```yaml
- setup-python/install-packages:
    with:
      app-dir: ./service
```

It uses the `python` and `pip` on `PATH`; if the executor only has `python3`
and `pip3`, it links `python` and `pip` to them. If that link fails, it warns
and installs with `python3` and `pip3` anyway.

A package manager other than pip that isn't on `PATH` is installed with pip
first: the version `pyproject.toml` pins with `tool.uv.required-version` or
`tool.poetry.requires-poetry`, otherwise the latest. After setup-python's
`install`, pip puts it on `PATH` (see [pip installs](#pip-installs)). Where pip
installs it somewhere else, `install-packages` fails; install the package
manager in an earlier step instead.

### Choosing a package manager

By default `install-packages` detects the package manager from the files
present in `--app-dir`:

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
- setup-python/install-packages:
    with:
      package-manager: poetry
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

### Caching packages

Caching is on by default. `install-packages` restores two independent buckets
per manager before installing and saves them back afterwards — the virtualenv
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
<cache-version>-cci_pycache[-<CIRCLE_BRANCH>][-<python --version>]-<sha256(lockfile)>
```

The Python component is the whole output of `python --version`, space
included, as in `v1-cci_pycache-main-Python 3.13.1-<sha256>`. With
`$CIRCLE_BRANCH` unset, the branch component is empty, leaving
`v1-cci_pycache--Python 3.13.1-<sha256>`.

Drop a component with `--include-branch-in-cache-key=false` /
`--include-python-in-cache-key=false`, or replace the whole key with
`--cache-key-override`, which is used as it is, without reading the lockfile.

Turn one bucket off with `--venv-cache=false` / `--pypi-cache=false`, or all
caching with `--cache=false`. Point `--venv-path` at a directory (repeatable) to
override the detected venv path(s) for any manager. Cache problems never fail the
job: with no lockfile, or a `python --version` that fails, `install-packages`
installs without caching.

### Flags

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

Linux, macOS and Windows, on amd64 and arm64, plus Linux on 32-bit ARM,
ppc64le and s390x, the platforms setup-python itself is published for,
wherever python-build-standalone publishes a build for the release you ask
for. A version spec only matches releases with a build
for the machine it runs on.

- **Alpine and other musl distros** get python-build-standalone's musl builds,
  on amd64 and arm64.
- **Free-threaded builds** are available from 3.13.12 and 3.14.3.
- **Windows** gets `python`, `python3`, `pip` and `pip3` shims that work from
  cmd, PowerShell and Git Bash. Where a preinstalled Python is on `PATH`, they
  take its place, and its `Scripts` directory's `pip` too, which comes ahead of
  it.
- **PyPy and GraalPy** are not supported.

Cache locations for `install-packages` are resolved per platform following
each tool's own default — e.g. pip's cache is `~/.cache/pip` on Linux,
`~/Library/Caches/pip` on macOS and `%LocalAppData%\pip\Cache` on Windows (the
paths in the [Caching packages](#caching-packages) table are the Linux ones).
