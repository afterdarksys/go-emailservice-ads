# Enterprise identity

## LDAP and Active Directory

The active SMTP, IMAP, and JMAP password paths share LDAP verification. Provision
accounts first (mailbox API or SCIM); LDAP never silently creates mailboxes or
assigns relay/admin privileges. Disablement in the local account store always
wins, including the legacy external-directory SSO path.

```yaml
auth:
  ldap:
    enabled: true
    url: ldaps://ad.example.net:636
    base_dn: DC=example,DC=net
    bind_dn: CN=mail-reader,OU=Services,DC=example,DC=net
    password_file: /run/secrets/ldap-bind-password
    ca_file: /etc/mailhub/directory-ca.pem
    user_filter: '(&(objectClass=user)(userPrincipalName={username})(!(userAccountControl:1.2.840.113556.1.4.803:=2)))'
    domains: [example.net]
```

For OpenLDAP use an appropriate filter such as
`(&(objectClass=inetOrgPerson)(mail={username}))`. The username substitution is
LDAP-filter escaped. Search must return exactly one DN; that DN is bound using
the supplied nonempty password. Service credentials require only search/read
access. Referrals are not followed. Connections require certificate-verified
LDAPS with TLS 1.2 or newer; there is no plaintext/password fallback. Dial and
operation timeouts are five seconds. Rotate the password file atomically: it is
read for each login. Other LDAP configuration changes require reload/restart as
specified by the reload endpoint.

On failure check certificate hostname/CA, service bind credentials, search base,
filter uniqueness, AD account status and local provisioning/enabled state. Never
turn off certificate verification to diagnose an outage. Repeated failures use
the existing account/IP lockout policy. Provider failures return generic login
failure to clients.

Qualification requires a real directory: valid/invalid/empty password, disabled
AD account, duplicate matches, missing account, escaped filter input, TLS failure,
provider outage, secret rotation, and account disablement across all mail protocols.
Unit tests verify local disablement and fail-closed behavior; they do not certify
a particular AD forest or LDAP deployment.

Reference: [go-ldap authentication and TLS API](https://pkg.go.dev/github.com/go-ldap/ldap/v3).
