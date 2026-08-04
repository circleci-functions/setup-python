# setup-python implementation goals

Checkpoints for implementing `setup-python` per `cmd/setup-python/porting.md`.
Intended for an autonomous worker/evaluator loop: the worker implements a
goal, then an evaluator re-derives pass/fail from the "Verify" commands
before checking it off. Don't check a box on the worker's own say-so — only
after the listed verification actually passes.

Goals are ordered by dependency; don't start G*n* until every goal it depends
on is checked off.

## G1 — Package manager detection

Depends on: none

- [x] `cmd/setup-python/python/packagemanager.go` implements a `PackageManager`
      type with auto-detection priority order: `pip-dist` (requirements.txt +
      setup.py/pyproject.toml) → `pip` (requirements.txt alone) → `pipenv`
      (Pipfile) → `uv` (uv.lock) → `poetry` (pyproject.toml alone).
- [x] `Lockfile()` returns the correct filename per manager (`requirements.txt`,
      `Pipfile.lock`, `uv.lock`, `poetry.lock`; empty/N-A for pip-dist's own
      lockfile handling per porting.md).
- [x] Install command/args builder per manager matches porting.md's `dist`/
      `install-packages` command tables (pip `-r <file>`, pip-dist `-e <path-args>`,
      pipenv `install <args>`, poetry `install --no-ansi <args>`, uv `sync <args>`).

**Verify:**
```
go test ./cmd/setup-python/python/... -run TestDetect -v
go vet ./cmd/setup-python/...
```
Test must cover all 5 detection branches plus the "no manager detected" error case.

## G2 — Cache path & key logic

Depends on: G1

- [x] `cmd/setup-python/python/cache.go` resolves venv/pypi paths per manager
      per the porting.md table, including `PIPENV_VENV_IN_PROJECT`,
      `WORKON_HOME`, and `CIRCLE_WORKING_DIRECTORY` (uv) branches.
- [x] `--venv-path` override replaces the detected venv path(s) for any manager.
- [x] Cache key builder produces
      `<cache-version>-cci_pycache-[<branch>-]-[<python-version>-]-<sha256(lockfile)>`,
      with each bracketed component gated by its own flag, and
      `--cache-key-override` bypassing the whole computation.

**Verify:**
```
go test ./cmd/setup-python/python/... -run TestCache -v
```
Tests use `pkg/sdk/sdktest`, no network. Cover: each include-branch/include-python
toggle combination, override wins outright, correct paths per manager.

## G3 — Python/pip binary resolution + `bashenv.AddAlias` parity

Depends on: G1

- [ ] Resolver prefers `python`/`pip` on `PATH`, falls back to `python3`/`pip3`.
- [ ] The function's own subprocess calls always use the resolved binary directly.
- [ ] When falling back, calls `bashenv.AddAlias(map[string]string{"python": "python3", "pip": "pip3"})`
      (already implemented in `bashenv/bashenv.go`) so later steps in the same
      job get the alias too.
- [ ] When `python` is already on `PATH`, `AddAlias` is *not* called and
      `$BASH_ENV` is untouched.

**Verify:**
```
go test ./cmd/setup-python/python/... -run TestPythonBinary -v
go test ./cmd/setup-python/... -run TestAlias -v
```

## G4 — `install-packages` subcommand

Depends on: G1, G2, G3

- [ ] `main.go` wires `installPackagesCmd` with every flag from porting.md's
      `install-packages` flag-mapping table: `--app-dir`, `--args`,
      `--cache-key-override`, `--cache-version`, `--include-branch-in-cache-key`,
      `--include-python-in-cache-key`, `--path-args`, `--pip-dependency-file`,
      `--pkg-manager` (enum), `--cache`, `--pypi-cache`, `--venv-cache`, `--venv-path`.
      No flag for `cache-folder-prefix`, `no_output_timeout`, or `pre-install-steps`
      (dropped per porting.md).
- [ ] Flow: detect manager → (if `--cache`) restore → install → (if `--cache`) save.
- [ ] `--no-cache` skips both restore and save calls entirely (not just one).

**Verify:**
```
./setup-python install-packages --help   # every flag from the table present, correct default/negatable
go test ./cmd/setup-python/... -run TestInstallPackages -v
```
Test (via `sdktest`) must prove: a full restore→install→save cycle for `pip`
and one venv-based manager (e.g. `poetry`); `--no-cache` results in zero
`SaveCache`/`RestoreCache` calls to the fake server.

## G5 — `dist` subcommand

Depends on: G1

- [ ] `main.go` wires `distCmd` with every flag from porting.md's `dist` table:
      `--app-dir`, `--build-tool` (enum wheel/build), `--no-isolation`,
      `--out-dir`, `--sdist-separate`, `--skip-dependency-check`, `--wheel-separate`.
- [ ] `build-tool=wheel`: runs `pip install wheel` then
      `python setup.py sdist bdist_wheel --dist-dir <out-dir>`.
- [ ] `build-tool=build`: runs `pip install build` then `python -m build`
      with `--outdir`, and conditionally `--sdist`/`--wheel`/`--skip-dependency-check`/
      `--no-isolation` per the corresponding flags.

**Verify:**
```
go test ./cmd/setup-python/... -run TestDist -v
```
Test asserts the exact subprocess argv for each bool-flag combination.

## G6 — Acceptance tests

Depends on: G4, G5

- [ ] `acceptance/setup-python/` compiles the real binary in `TestMain` (mirroring
      `acceptance/setup-node`) and runs it as a subprocess.
- [ ] Covers `install-packages` for each of the 5 package managers (cache hit
      and cache miss) and both `dist` build-tools.
- [ ] `testdata/` fixtures are generated by actually running `pip`/`poetry`/
      `pipenv`/`uv` (per `CLAUDE.md`'s acceptance-fixture convention), not
      hand- or AI-written.

**Verify:**
```
go test ./acceptance/setup-python/... -v
```
Must pass hermetically — no network access.

## G7 — README + top-level docs

Depends on: G4, G5, G6

- [ ] `cmd/setup-python/README.md` has flags tables for both subcommands, a
      cache/caching section, a "Documented deviations" section (pre-install-steps
      unsupported + `--no-cache` workaround, `--venv-path` as a repeatable flag),
      and example invocations.
- [ ] Top-level `README.md` functions table gets a `setup-python` row.

**Verify:**
```
grep -c '^##' cmd/setup-python/README.md   # sanity: has multiple sections
grep 'setup-python' README.md              # row present
```

## G8 — Full CI gate (terminal condition)

Depends on: G1–G7

- [ ] Everything above is checked off.

**Verify:**
```
task ci
```
This is the loop's overall stop condition — the evaluator confirms the
objective satisfied only when `task ci` exits 0 *and* every box above is
checked (a green `task ci` alone isn't sufficient if, say, G7's README was
skipped but nothing fails a test for it).
