# Contributing to uMailServer

Thank you for your interest in contributing to uMailServer! We welcome contributions from the community.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/umailserver.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Run tests: `make test`
6. Commit with clear messages: `git commit -m "feat: add new feature"`
7. Push and submit a PR

## Development Setup

```bash
# Clone and setup
git clone https://github.com/umailserver/umailserver.git
cd umailserver
make setup

# Run in development mode
make dev
```

## Code Style

- Go: Follow standard Go conventions (`gofmt`, `go vet`)
- TypeScript/React: Use ESLint and Prettier configurations in the project
- Commit messages: Use Conventional Commits format

## Testing

- Write tests for new features
- Ensure all tests pass: `make test`
- Run with race detection: `make test-race`
- Check coverage: `make coverage`

## Pull Request Process

1. Update documentation if needed
2. Add tests for new functionality
3. Run the checks locally (CI does not run on pull requests, see below)
4. Request review from maintainers
5. Address review feedback

## CI and releases

GitHub Actions does not run on every push or pull request. Run the checks
locally before opening a pull request:

```bash
go build ./... && go vet ./... && go test ./... -count=1 -short
```

The workflows run only in these cases:

| Workflow | Runs on |
|---|---|
| `ci.yml` (lint, test, build, E2E) | a pushed `v*` tag, or a manual run |
| `docker.yml` (image build and push) | a pushed `v*` tag, or a manual run |
| `release.yml` (binaries and GitHub release) | a pushed `v*` tag |
| `fuzz.yml` | weekly schedule, or a manual run |
| `backup-restore.yml` | daily/weekly schedule, or a manual run |

To run CI by hand on a branch, use the Actions tab ("Run workflow") or:

```bash
gh workflow run ci.yml --ref <branch>
gh workflow run docker.yml --ref <branch>   # also pushes an image tagged with the branch name
```

To cut a release, tag the commit on `main` and push the tag. CI, the Docker
image and the release binaries are all built from that tag:

```bash
git tag v1.2.3
git push origin v1.2.3
```

Because CI does not run on pull requests, `main`'s branch protection does
not require any status check.

## Code of Conduct

Be respectful and constructive in all interactions.

## Questions?

Open an issue or join our discussions.
