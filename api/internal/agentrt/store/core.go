package store

// Core work (Owner 2026-10-19 「核心字段 A」: a plan may mark a task core; the
// chief of staff judges which by the rules; routing takes core tasks as it
// does). Core work is the runtime's own core, as design v5 names it (§0 item
// 6, the Owner 10-13): the data layer, the Redis data forms, the kernel's
// interfaces, agentd, the bus and stopping; in a repository, a change under
// one of its Core directories (repos.go). Routing gives it to the holder of
// only author_core and its review to the holder of only review_core (the
// Owner's capability facts: Claude writes it, Codex reviews it read-only);
// agentd's deterministic check verifies it as any work (route.go). Work
// beside it is not core even under those directories: test boilerplate,
// documentation, a page filled in to an agreed interface.
