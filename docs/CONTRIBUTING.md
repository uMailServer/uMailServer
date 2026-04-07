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
3. Ensure CI passes
4. Request review from maintainers
5. Address review feedback

## Code of Conduct

Be respectful and constructive in all interactions.

## Questions?

Open an issue or join our discussions.
