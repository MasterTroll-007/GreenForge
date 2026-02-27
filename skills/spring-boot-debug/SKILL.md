# Spring Boot Debug

Skill for debugging Spring Boot application issues.

## Trigger
User asks to debug a Spring Boot application, investigate startup failures,
bean injection errors, or configuration issues.

## Steps
1. Check application logs for ERROR/WARN entries
2. Analyze Spring context loading (bean definitions, profiles)
3. Check configuration properties per active profile
4. Identify dependency injection failures
5. Suggest fix based on error patterns

## Tools Used
- `spring_analyzer`: list_beans, analyze_config
- `logs`: log_search, log_analyze
- `file`: file_read, file_search
- `build`: run_tests
