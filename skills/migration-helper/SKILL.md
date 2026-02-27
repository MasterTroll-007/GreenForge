# Migration Helper

Skill for database migration management.

## Trigger
User asks to create/review database migrations, update schema, or check migration status.

## Steps
1. Detect migration tool (Liquibase/Flyway)
2. Show current migration history
3. Check for pending migrations
4. Help create new migration scripts
5. Validate migration against current schema
6. Check for backwards compatibility

## Migration Guidelines
- Always add columns as nullable first
- Create indexes concurrently when possible
- Test rollback scripts
- Version naming convention: V{number}__{description}
- Liquibase: use changesets with author + id

## Tools Used
- `database`: db_schema, db_migrations
- `file`: file_read, file_write
- `git`: git_status (check if migration is committed)
- `build`: run_tests (integration tests)
