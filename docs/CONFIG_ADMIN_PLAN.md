# Configuration and administration tooling

1. Audit supported admin/queue commands and remove fabricated success paths.
2. Add gemsads-conf JSON/YAML inspection, validation, formatting and atomic,
   backed-up edits; share platform validation and default configuration.
3. Add SQLite inventory, schema/rows, integrity checks, consistent backup,
   schema-driven creation and offline transactional maintenance.
4. Add configuration diagnostics, verified TLS checks and RCPT-only relay probes.
5. Repair admin CLI queue mappings and expose a scoped generic API command.
6. Package tools, document Postfix equivalents and remaining limits, test and ship.

Database commands target SQLite files, not arbitrary proprietary .db formats.
Managed database mutations require the deployment's spool ownership lock and a
backup. No tool can automatically reconstruct arbitrary corrupted data or infer
safe changes to every database schema. Checks must report uncertainty honestly.
