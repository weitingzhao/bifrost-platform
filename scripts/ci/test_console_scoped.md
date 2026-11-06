# Scoped Console checks

Run `make test-console-scoped BASE=<ancestor> HEAD=<commit>` or
`scripts/ci/test_console_scoped.sh <base> <head>` from a trusted checkout.
The script uses `go run ./cmd/testscope -console -module .` from api/.
The CLI also accepts `-stdin` for git name-status input (including renames).
Invalid input, unavailable history, scan failures and shared configuration changes
fall back to full checks.

The selector prints `console_scope=full|incremental|none`, `console_related`,
`console_tests`, `console_lint`, `console_build=true|false`, and `why`, one per
line. File lists are sorted, space-separated repository-relative paths; paths
requiring quoting cause full scope. Both sides of renames are inspected.
Tests under src/**/__tests__/ or named *.test.ts(x) are scanned for changed
external paths, paths relative to the test directory, and filenames.

Full scope runs type-check, cached eslint on src, all Vitest tests and the build.
Incremental scope runs type-check, eslint on changed existing TypeScript source,
Vitest related tests for changed src files, and explicit external-file readers.
It builds whenever a console/ path changed. None prints the selection and runs
no npm checks. Related and explicit tests may overlap and run twice.

CI inputs:

- `BASE`, `HEAD`: Make variables identifying the diff endpoints.
- `CONSOLE_SCOPE_REQUEST=full`: override selection and run everything.
- `CONSOLE_CACHE_DIR`: optional persistent cache directory, resolved relative to
  the caller's working directory. CI should restore/save it with a dependency
  and toolchain compatible cache key. Stores eslint/, vitest/, vite/, and
  type-check.tsbuildinfo. Without it, tools use their default caches (eslint
  uses console/node_modules/.cache/eslint/).

With a cache directory, type-check receives --incremental and --tsBuildInfoFile.
Temporary --config wrappers supply Vite and Vitest cacheDir from the environment,
without changing checked-in configs. The build still uses the package's build
script. The script returns nonzero on a failed check and cleans up wrappers.
