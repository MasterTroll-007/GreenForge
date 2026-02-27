# JVM Code Review

Skill for reviewing Java/Kotlin code with JVM best practices.

## Trigger
User asks for code review or PR review.

## Steps
1. Get the diff (git diff or PR diff)
2. Check for common Java/Kotlin anti-patterns
3. Verify Spring Boot conventions
4. Check for security vulnerabilities (OWASP)
5. Verify test coverage for changed code
6. Provide structured feedback

## Review Checklist
- [ ] Null safety (Optional, Kotlin null types)
- [ ] Exception handling (no catch-all, proper logging)
- [ ] Thread safety (if concurrent access possible)
- [ ] Resource management (try-with-resources, Closeable)
- [ ] Spring conventions (@Transactional, @Service separation)
- [ ] Kafka consumer idempotency
- [ ] SQL injection prevention (parameterized queries)
- [ ] Test coverage for new/changed code

## Tools Used
- `git`: git_diff
- `code_review`: review_diff, check_idioms
- `spring_analyzer`: list_beans (for architecture compliance)
- `build`: run_tests
