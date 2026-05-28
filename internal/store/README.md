# store

Persistence interfaces and implementations.

Planned split:

- `base`: shared query and tenant/user helpers.
- `pg`: primary PostgreSQL implementation.
- `sqlite`: optional lightweight local fallback if needed later.
- `migrations`: schema ownership references.
