# Mail Groups

Mail Groups provide a way to organize email addresses into logical collections for use in divert and screen rules.

## Overview

Groups allow you to:
- Define collections of email addresses
- Reference groups in routing rules
- Maintain centralized user lists
- Support dynamic membership (LDAP, database queries)

## Group Types

### 1. Static Groups

Manually maintained list of email addresses.

```yaml
executives:
  type: static
  members:
    - ceo@company.com
    - cfo@company.com
    - cto@company.com
  metadata:
    owner: admin@company.com
    purpose: "Executive team"
```

**Pros:**
- Simple and explicit
- No external dependencies
- Easy to audit

**Cons:**
- Manual maintenance required
- Must reload config to update
- Not suitable for large groups

**Best for:**
- Small, stable groups (< 50 members)
- VIP lists
- Special monitoring groups

### 2. LDAP Groups

Automatically synchronized with LDAP/Active Directory.

```yaml
all-employees:
  type: ldap
  ldap_query: "(&(ou=employees)(objectClass=user)(mail=*))"
  ldap_server: "ldap://ldap.company.com"
  metadata:
    purpose: "All company employees from LDAP"
```

**Pros:**
- Automatically updated
- Centrally managed
- Enterprise integration

**Cons:**
- Requires LDAP server
- Network dependency
- Slower than static (cached)

**Best for:**
- Department groups
- Location-based groups
- Large organizations with AD/LDAP

**Configuration Required:**

```go
// LDAP Provider Configuration
type LDAPConfig struct {
    Server   string
    Port     int
    BaseDN   string
    BindDN   string
    BindPW   string
    TLS      bool
}
```

### 3. Dynamic Groups

Based on database queries, updated in real-time.

```yaml
active-users:
  type: dynamic
  query: "SELECT email FROM users WHERE active=true AND email IS NOT NULL"
  database: "users_db"
  metadata:
    purpose: "Dynamically updated active users"
```

**Pros:**
- Real-time updates
- Flexible queries
- Can join multiple conditions

**Cons:**
- Requires database connection
- Query performance matters
- More complex to debug

**Best for:**
- User status-based groups (active, inactive)
- Role-based groups
- Complex filtering requirements

## Configuration: `groups.yaml`

### Basic Structure

```yaml
groups:
  group-name:
    type: static|ldap|dynamic
    # Type-specific fields
    metadata:
      owner: email@example.com
      purpose: "Description"
      created: "YYYY-MM-DD"
```

### Complete Example

```yaml
groups:
  # Static group
  executives:
    type: static
    members:
      - ceo@company.com
      - cfo@company.com
      - cto@company.com
    metadata:
      owner: admin@company.com
      purpose: "C-level executives"

  # LDAP group
  finance-dept:
    type: ldap
    ldap_query: "(&(ou=finance)(objectClass=user))"
    ldap_server: "ldap://ldap.company.com"
    metadata:
      purpose: "Finance department from LDAP"

  # Dynamic group
  premium-customers:
    type: dynamic
    query: "SELECT email FROM customers WHERE tier='premium' AND active=true"
    database: "crm_db"
    metadata:
      purpose: "Premium tier customers"
```

## Using Groups in Rules

### Divert Rules

```yaml
# In divert.yaml
divert_rules:
  - name: "Monitor Executives"
    match:
      type: group
      value: executives  # References group name
    action:
      divert_to: board-compliance@company.com
```

### Screen Rules

```yaml
# In screen.yaml
screen_rules:
  - name: "Finance Department Monitoring"
    match:
      type: group
      value: finance-dept
      direction: both
    action:
      screen_to:
        - cfo@company.com
```

## Group Expansion

Groups can be used as recipients (expands to all members):

```yaml
# Send to all group members
To: @executives

# Expands to:
# ceo@company.com, cfo@company.com, cto@company.com
```

**Syntax:** Prefix group name with `@`

## Group Management API

### List Groups

```go
manager := groups.NewManager("groups.yaml", logger)
groupNames := manager.ListGroups()
// Returns: ["executives", "finance-dept", "premium-customers"]
```

### Get Members

```go
members, err := manager.GetMembers(ctx, "executives")
// Returns: ["ceo@company.com", "cfo@company.com", "cto@company.com"]
```

### Check Membership

```go
isMember, err := manager.IsMember(ctx, "executives", "ceo@company.com")
// Returns: true
```

### Add Group

```go
group := &groups.Group{
    Type: groups.GroupTypeStatic,
    Members: []string{"user1@example.com", "user2@example.com"},
    Metadata: map[string]string{
        "owner": "admin@company.com",
        "purpose": "New team",
    },
}
err := manager.AddGroup("new-team", group)
```

### Remove Group

```go
err := manager.RemoveGroup("old-team")
```

### Reload Configuration

```go
err := manager.Reload()
```

## Caching

Groups are cached for performance:

- **Cache Duration:** 5 minutes (default)
- **Cache Invalidation:** Automatic after reload
- **Manual Invalidation:**

```go
// Invalidate specific group
manager.InvalidateCache("executives")

// Invalidate all groups
manager.InvalidateCache("")
```

## Nested Groups

Currently not supported. Use LDAP queries or database joins for complex hierarchies.

**Workaround:**

```yaml
# Instead of nesting, expand in query
all-management:
  type: static
  members:
    # Executives
    - ceo@company.com
    - cfo@company.com
    - cto@company.com
    # Directors
    - director1@company.com
    - director2@company.com
```

Or use LDAP:

```yaml
all-management:
  type: ldap
  ldap_query: "(|(ou=executives)(ou=directors))"
```

## Performance Considerations

### Static Groups

- **Lookup:** O(n) linear search
- **Recommended size:** < 100 members
- **Optimization:** Sorted and binary search for large groups

### LDAP Groups

- **Lookup:** Network call + LDAP query
- **Cache hit:** < 1ms
- **Cache miss:** 50-200ms
- **Optimization:** 5-minute cache

### Dynamic Groups

- **Lookup:** Database query
- **Cache hit:** < 1ms
- **Cache miss:** 10-100ms (depends on query)
- **Optimization:** Index email column

### Recommendations

| Group Size | Type | Cache Strategy |
|------------|------|----------------|
| < 50 | Static | None needed |
| 50-500 | LDAP/Dynamic | 5-minute cache |
| > 500 | Dynamic | 10-minute cache + indexed DB |

## Security

### Access Control

Restrict access to `groups.yaml`:

```bash
chmod 640 groups.yaml
chown mail:mail groups.yaml
```

### Audit Trail

Group changes are logged:

```
INFO  Group added  group=new-team type=static members=5
INFO  Group removed  group=old-team
INFO  Group members updated  group=executives old_count=3 new_count=4
```

### Sensitive Groups

Mark sensitive groups in metadata:

```yaml
executives:
  type: static
  members: [...]
  metadata:
    sensitivity: high
    access_level: restricted
    audit_all: true
```

## LDAP Configuration

### Connection Setup

Create `ldap-config.yaml`:

```yaml
ldap:
  servers:
    - ldap://ldap1.company.com:389
    - ldap://ldap2.company.com:389
  base_dn: "dc=company,dc=com"
  bind_dn: "cn=mail-service,ou=services,dc=company,dc=com"
  bind_password: "secret"
  use_tls: true
  tls_verify: true
  timeout: 10s
  page_size: 1000
```

### Common LDAP Queries

**All users:**
```
(&(objectClass=user)(mail=*))
```

**Department:**
```
(&(ou=finance)(objectClass=user)(mail=*))
```

**Active users only:**
```
(&(objectClass=user)(mail=*)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))
```

**Specific groups:**
```
(&(memberOf=CN=Executives,OU=Groups,DC=company,DC=com)(mail=*))
```

## Database Configuration

### Connection Setup

Create `db-config.yaml`:

```yaml
databases:
  users_db:
    type: postgresql
    host: db.company.com
    port: 5432
    database: users
    username: mail_service
    password: secret
    ssl_mode: require
    max_connections: 10
```

### Query Requirements

Queries must return email addresses:

```sql
-- Single column named 'email'
SELECT email FROM users WHERE active=true

-- Or use alias
SELECT email_address AS email FROM contacts WHERE status='active'
```

### Performance Optimization

**Add indexes:**
```sql
CREATE INDEX idx_users_email_active ON users(email, active);
CREATE INDEX idx_contacts_status ON contacts(status) WHERE status='active';
```

**Use prepared statements:**
```sql
PREPARE active_users AS
  SELECT email FROM users WHERE active=$1 AND email IS NOT NULL;

EXECUTE active_users(true);
```

## Troubleshooting

### Group not found

```
Error: group not found: executives
```

**Solution:**
1. Check group name in `groups.yaml`
2. Verify config is loaded
3. Check for typos

### LDAP connection failed

```
Error: failed to connect to LDAP server
```

**Solution:**
1. Verify LDAP server is reachable
2. Check credentials
3. Test with `ldapsearch`:
   ```bash
   ldapsearch -H ldap://ldap.company.com -D "cn=..." -W
   ```

### Dynamic query timeout

```
Error: database query timeout
```

**Solution:**
1. Optimize query (add indexes)
2. Reduce dataset
3. Increase timeout
4. Consider caching more aggressively

### Empty group members

```
Warning: group has no members: team-name
```

**Solution:**
1. For static: Add members to config
2. For LDAP: Verify LDAP query returns results
3. For dynamic: Check database query

## Best Practices

1. **Use static for VIPs**: Small, important groups
2. **Use LDAP for departments**: Automatic sync with HR systems
3. **Use dynamic for status**: Active/inactive, role-based
4. **Document purpose**: Always fill metadata
5. **Regular audits**: Review group membership quarterly
6. **Test before deploy**: Verify group expansion
7. **Monitor performance**: Watch query times
8. **Cache appropriately**: Balance freshness vs performance
9. **Access control**: Restrict who can modify groups
10. **Backup configs**: Version control `groups.yaml`

## Migration Guide

### From Static Lists to Groups

**Before:**
```yaml
# In divert.yaml
divert_rules:
  - name: "Monitor Bob"
    match:
      type: recipient
      value: bob@company.com
  - name: "Monitor Alice"
    match:
      type: recipient
      value: alice@company.com
```

**After:**
```yaml
# In groups.yaml
groups:
  monitored-users:
    type: static
    members:
      - bob@company.com
      - alice@company.com

# In divert.yaml
divert_rules:
  - name: "Monitor Special Users"
    match:
      type: group
      value: monitored-users
```

## See Also

- [Divert Proxy System](DIVERT_PROXY.md)
- [Screen Proxy System](SCREEN_PROXY.md)
- [Master Control](MASTER_CONTROL.md)
