# Admin users management

## Choice: no `users.enabled` column

The users table has no `enabled` column. To avoid a risky migration on live SQLite,
admin user management focuses on **role** and **quota_total** first:

- `PATCH /api/users/{id}` accepts optional `{ role, quota_total, note }`
- `note` is accepted for API compatibility but **not persisted** (no column)
- Disable-user is skipped until an explicit migration adds `enabled INTEGER DEFAULT 1`

Self-lockout / last-admin guards:

- Cannot demote your own admin role
- Cannot demote the last remaining admin
