# Contributing to uMailServer

Thank you for your interest in contributing!

## License Notice

By contributing to this project, you agree that your contributions will be
dual-licensed under AGPL-3.0 and Commercial license terms.

## Development Setup

```bash
git clone https://github.com/umailserver/umailserver.git
cd umailserver
go mod download
```

## Running Tests

```bash
make test
```

## Code Style

- Follow standard Go conventions (`go fmt`)
- Run linter: `make lint`
- All tests must pass
- Maintain or improve test coverage

## Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass
6. Update documentation if needed
7. Submit PR with clear description

## Commit Messages

Use Conventional Commits format:

```
type(scope): description

[optional body]

[optional footer]
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

## Questions?

Open an issue or contact: dev@umailserver.com
