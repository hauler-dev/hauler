# Contributing to Hauler UI

Thank you for your interest in contributing to Hauler UI! This guide will help you get started.

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help others learn and grow
- Follow project guidelines

## Getting Started

### 1. Fork and Clone

```bash
git clone <your-fork-url>
cd hauler-ui
```

### 2. Set Up Development Environment

```bash
# Start the development environment
docker compose up -d

# Or run backend locally
cd backend
go run main.go
```

### 3. Create a Branch

```bash
git checkout -b feature/your-feature-name
```

## Development Workflow

### Making Changes

1. **Backend (Go)**
   - Edit `backend/main.go`
   - Follow existing patterns
   - Add error handling
   - Test with `go run main.go`

2. **Frontend (JavaScript)**
   - Edit `frontend/app.js`
   - Maintain existing structure
   - Test in browser
   - Check console for errors

3. **Documentation**
   - Update README.md if needed
   - Add wiki pages for new features
   - Update API reference

### Testing

```bash
# Run all tests
./tests/run_all_tests.sh

# Run specific test suite
./tests/comprehensive_test_suite.sh

# Security scan
./tests/security_scan.sh
```

### Code Style

**Go:**
- Use `gofmt` for formatting
- Follow standard Go conventions
- Add comments for exported functions

**JavaScript:**
- Use consistent indentation (2 spaces)
- Use async/await for promises
- Add JSDoc comments for complex functions

**Python:**
- Follow PEP 8
- Use type hints
- Add docstrings

## Submitting Changes

### 1. Commit Your Changes

```bash
git add .
git commit -m "feat: add new feature"
```

**Commit Message Format:**
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `refactor:` Code refactoring
- `test:` Test additions/changes
- `chore:` Maintenance tasks

### 2. Push to Your Fork

```bash
git push origin feature/your-feature-name
```

### 3. Create Merge Request

1. Go to GitLab repository
2. Click "New Merge Request"
3. Select your branch
4. Fill in description:
   - What changes were made
   - Why they were needed
   - How to test them
5. Submit for review

## Review Process

1. **Automated Checks**
   - CI/CD pipeline runs tests
   - Security scan executes
   - Build verification

2. **Code Review**
   - Maintainer reviews code
   - Feedback provided
   - Changes requested if needed

3. **Approval and Merge**
   - Once approved, maintainer merges
   - Changes deployed to staging

## Development Guidelines

### Adding New Features

1. **Plan First**
   - Discuss in issue tracker
   - Get feedback on approach
   - Consider impact on existing features

2. **Implement**
   - Follow existing patterns
   - Add comprehensive error handling
   - Include logging where appropriate

3. **Test**
   - Test happy path
   - Test error conditions
   - Test edge cases

4. **Document**
   - Update API reference
   - Add user documentation
   - Include code comments

### Bug Fixes

1. **Reproduce**
   - Confirm the bug exists
   - Document steps to reproduce
   - Identify root cause

2. **Fix**
   - Make minimal changes
   - Don't introduce new features
   - Maintain backward compatibility

3. **Verify**
   - Test the fix
   - Ensure no regressions
   - Update tests if needed

## Project Structure

```
hauler-ui/
├── backend/          # Go backend
├── frontend/         # JavaScript frontend
├── mcp_server/       # Python MCP server
├── docs/             # Documentation
├── tests/            # Test suites
└── data/             # Persistent data
```

## Getting Help

- **Questions**: Open a discussion
- **Bugs**: Create an issue
- **Features**: Propose in issue tracker
- **Security**: Email security@example.com

## Recognition

Contributors are recognized in:
- CONTRIBUTORS.md file
- Release notes
- Project documentation

Thank you for contributing to Hauler UI! 🎉
