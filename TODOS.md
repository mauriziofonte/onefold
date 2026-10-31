# TODOS

Open work that needs a person (a decision, administrator rights, a desktop, or a tool install). Each item is self-contained: read it, read the files it names, do it. General project rules for agents: `LLMS.md`. Run `make test` before and after any change; it must stay green.

Common facts:

- onefold is a Go CLI + local GUI that dedups photo/video archives and other files and builds a canonical "golden" tree (`README.md`).
- Hard rules (`LLMS.md`): sources are read-only (only exceptions: `rearrange` duplicate deletion, `purge-sources`); the media naming and winner rules are frozen and pinned by `TestMediaSynthRegression` (`internal/plan/synth_test.go`) and `TestGenericPlanRegression`; external transport binaries (rsync, ssh) are never executed by onefold; dependencies are bumped on purpose only (Dependabot pull requests, merged by a person) and `govulncheck` is a blocking CI job.
- Windows-native is the primary target: changes touching paths, rename, time or console must also pass the Windows test binaries (`GOOS=windows go test -c ./<pkg>` run on Windows from the package directory).

No open items. Add one with an ID (`T6`, ...), a row in a table like this one, and a section with State, Why blocked, Steps and Done when:

| ID | Item | Blocked on | Effort once unblocked |
|---|---|---|---|
