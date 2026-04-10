# Contributing to BubbleFish Nexus

Thanks for your interest. Before you open a PR, there are a few things to know.

---

## CLA requirement

BubbleFish Nexus is dual-licensed: AGPL-3.0 for community use, commercial license for enterprise. To keep that model legally sound, all contributors must sign a Contributor License Agreement before their first commit is merged.

The CLA grants BubbleFish Technologies, Inc. the right to relicense your contributions under the commercial license. You retain copyright ownership of your contributions — the CLA is a license grant, not a copyright assignment. Your work is always available under AGPL-3.0.

The CLA bot will comment on your first PR with a link to sign. It takes about two minutes.

If you have questions about the CLA before contributing, open an issue tagged `legal` or email contribute@bubblefish.sh.

---

## What to work on

Check the issue tracker. Issues labeled `good first issue` are well-scoped and have enough context to get started without needing deep architecture knowledge.

Issues labeled `help wanted` are things we want done but don't have bandwidth for right now. These may require more context — read the issue thread carefully before starting.

If you want to work on something not in the tracker, open an issue first and describe what you're planning. This prevents duplicate work and makes sure the direction fits the project.

---

## Development setup

**Requirements:** Go 1.26+, Git

```bash
git clone https://github.com/bubblefish-tech/nexus.git
cd nexus

# Build
go build -o bubblefish ./cmd/bubblefish/

# Run tests
go test ./...

# Run tests with race detection (requires GCC)
CGO_ENABLED=1 go test ./... -race

# Run with local config
bubblefish install --mode simple
bubblefish start
```

For destination testing:
- SQLite works out of the box (bundled, zero config)
- PostgreSQL (pgvector): configure connection string in destination TOML
- Supabase: configure URL and service key in destination TOML

---

## Before you submit a PR

1. **Run the tests.** `go test ./...` should pass with no failures. All 31 packages must be green.
2. **Run with race detection.** `CGO_ENABLED=1 go test ./... -race` must pass with zero races.
3. **Run the linter.** `go vet ./...` at minimum.
4. **Keep PRs focused.** One thing per PR. If you're fixing a bug and adding a feature, those are two PRs.
5. **Write a clear description.** What problem does this solve? How did you test it?
6. **Sign off your commits.** We use the Developer Certificate of Origin: `git commit -s -m "your message"`.

---

## What we won't merge

- Changes that add external dependencies without prior discussion
- Reformatting PRs or mass refactors without a clear reason
- Features that belong in the Enterprise tier
- Changes to core security paths without maintainer involvement from the start

If you're unsure whether something fits, ask first.

---

## Reporting security issues

Do not open a public issue for security vulnerabilities. Email security@bubblefish.sh with details. We'll respond within 48 hours and work with you on disclosure timing.

---

## License

By contributing, you agree that your contributions will be licensed under AGPL-3.0 for community use, with relicensing rights granted to BubbleFish Technologies, Inc. per the CLA you signed. You retain copyright ownership of your contributions.
