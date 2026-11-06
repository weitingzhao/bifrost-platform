# Scoped API tests

Run from the repository root with `make test-api-scoped BASE=<base> HEAD=<head>`. The CI checkout must supply the trusted base version of the script and command. `API_SCOPE_REQUEST=full` bypasses selection. `API_SCOPE_RESULT` and `FAILED_TESTS_RESULT` optionally name result files; relative paths are relative to the caller.

The selector accepts `go -C api run ./cmd/testscope -module . BASE HEAD`, or `-stdin` with tab-separated `git diff --name-status -M` lines. Unknown history, graph, parse, or selection errors choose the full suite. Guards conservatively include every package with a test-file filepath walk, including fixture walks. Store references are syntactic and conservatively include identifier matches; an empty selection or missing removed store file runs all store tests.

## Last five commits

These are the exact command outputs using each commit’s first parent as base, with the module graph from the working checkout.

### 785fd9e1

```text
scope=incremental
packages=github.com/weitingzhao/bifrost-platform/api/cmd/agentd github.com/weitingzhao/bifrost-platform/api/cmd/platform-api github.com/weitingzhao/bifrost-platform/api/cmd/s5check github.com/weitingzhao/bifrost-platform/api/cmd/stg-release-probe github.com/weitingzhao/bifrost-platform/api/internal/actions github.com/weitingzhao/bifrost-platform/api/internal/agentd github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller github.com/weitingzhao/bifrost-platform/api/internal/agentrt/exam github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards github.com/weitingzhao/bifrost-platform/api/internal/agentrt/relay github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgpreflight github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store github.com/weitingzhao/bifrost-platform/api/internal/approvals github.com/weitingzhao/bifrost-platform/api/internal/cluster github.com/weitingzhao/bifrost-platform/api/internal/delivery github.com/weitingzhao/bifrost-platform/api/internal/lineage github.com/weitingzhao/bifrost-platform/api/internal/questions github.com/weitingzhao/bifrost-platform/api/internal/safego github.com/weitingzhao/bifrost-platform/api/internal/server github.com/weitingzhao/bifrost-platform/api/internal/storedurability github.com/weitingzhao/bifrost-platform/api/internal/tradevocab
store_run=
why=changed packages, test dependents, and guards
```

### 28f6b91f

```text
scope=incremental
packages=github.com/weitingzhao/bifrost-platform/api/cmd/agentd github.com/weitingzhao/bifrost-platform/api/cmd/platform-api github.com/weitingzhao/bifrost-platform/api/cmd/s5check github.com/weitingzhao/bifrost-platform/api/cmd/stg-release-probe github.com/weitingzhao/bifrost-platform/api/internal/actions github.com/weitingzhao/bifrost-platform/api/internal/agentd github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller github.com/weitingzhao/bifrost-platform/api/internal/agentrt/exam github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questioncards github.com/weitingzhao/bifrost-platform/api/internal/agentrt/relay github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgpreflight github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store github.com/weitingzhao/bifrost-platform/api/internal/approvals github.com/weitingzhao/bifrost-platform/api/internal/cluster github.com/weitingzhao/bifrost-platform/api/internal/delivery github.com/weitingzhao/bifrost-platform/api/internal/lineage github.com/weitingzhao/bifrost-platform/api/internal/questions github.com/weitingzhao/bifrost-platform/api/internal/safego github.com/weitingzhao/bifrost-platform/api/internal/server github.com/weitingzhao/bifrost-platform/api/internal/storedurability github.com/weitingzhao/bifrost-platform/api/internal/tradevocab
store_run=^(TestReview751QuotaRecoveryBeforeCIKick)$
why=changed packages, test dependents, and guards
```

### 41509315

```text
scope=incremental
packages=github.com/weitingzhao/bifrost-platform/api/cmd/platform-api github.com/weitingzhao/bifrost-platform/api/cmd/s5check github.com/weitingzhao/bifrost-platform/api/internal/actions github.com/weitingzhao/bifrost-platform/api/internal/agentd github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store github.com/weitingzhao/bifrost-platform/api/internal/approvals github.com/weitingzhao/bifrost-platform/api/internal/cluster github.com/weitingzhao/bifrost-platform/api/internal/delivery github.com/weitingzhao/bifrost-platform/api/internal/questions github.com/weitingzhao/bifrost-platform/api/internal/safego github.com/weitingzhao/bifrost-platform/api/internal/server github.com/weitingzhao/bifrost-platform/api/internal/storedurability github.com/weitingzhao/bifrost-platform/api/internal/tradevocab
store_run=
why=changed packages, test dependents, and guards
```

### 7fcce288

```text
scope=incremental
packages=github.com/weitingzhao/bifrost-platform/api/cmd/platform-api github.com/weitingzhao/bifrost-platform/api/cmd/s5check github.com/weitingzhao/bifrost-platform/api/internal/actions github.com/weitingzhao/bifrost-platform/api/internal/agentd github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store github.com/weitingzhao/bifrost-platform/api/internal/approvals github.com/weitingzhao/bifrost-platform/api/internal/cluster github.com/weitingzhao/bifrost-platform/api/internal/delivery github.com/weitingzhao/bifrost-platform/api/internal/questions github.com/weitingzhao/bifrost-platform/api/internal/safego github.com/weitingzhao/bifrost-platform/api/internal/server github.com/weitingzhao/bifrost-platform/api/internal/storedurability github.com/weitingzhao/bifrost-platform/api/internal/tradevocab
store_run=
why=changed packages, test dependents, and guards
```

### 2f9ddad7

```text
scope=full
packages=
store_run=
why=testdata
```
