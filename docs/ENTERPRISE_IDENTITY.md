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

## SAML requirements and supported integration boundary

SAML browser login terminates at an identity broker (for example Keycloak),
which issues an application access token. Mailhub is the token-consuming resource
server; it does not expose a SAML assertion-consumer endpoint or interpret SAML
as an SMTP/IMAP password. This reconciles the historical SAML milestone with the
platform's API-only interface. A native browser UI/SAML service provider is a
separate product feature, not an implied capability of this integration.

Configure the broker's SAML identity provider with trusted IdP metadata and
signature validation enabled. Require the intended issuer/entity ID, destination,
audience, response correlation, assertion expiry and replay protection. Use
SHA-256 or stronger signatures, rotate certificates through broker metadata,
and restrict browser redirect URLs to the intended client. Do not accept arbitrary
email attributes as proof of account ownership. Map an administrator-approved,
stable subject to the provisioned Mailhub username; account linking must require
proof of control. Configure MFA and session lifetime at the IdP/broker.

For JMAP, configure:

```yaml
jmap:
  enabled: true
  jwt_public_key_path: /etc/mailhub/broker-public.pem
  jwt_issuer: https://identity.example.net/realms/mail
  jwt_audience: mailhub-jmap
```

The broker must issue RSA/ECDSA-signed access tokens with exact `iss`, an `aud`
containing `mailhub-jmap`, nonempty `sub` equal to the provisioned username, and
future `exp`. Configure an explicit audience: tokens for another application must
not be accepted. Mailhub rejects disabled/missing accounts independently of token
validity. Existing JWT deployments may omit audience for compatibility; new
federated deployments must set both issuer and audience. Public-key replacement
requires restart. Short access-token lifetimes limit logout latency; IdP logout
alone does not instantly revoke a self-contained JWT.

For administrative REST use `api.oauth` introspection with a distinct audience
and least-privilege scopes; never grant mailbox users administrative scopes by
default. SMTP/IMAP use local or LDAP credentials over TLS, not browser SAML tokens.

Acceptance evidence must include signed success, tampered/unsigned assertions,
wrong destination/audience/issuer, replay, expired assertions/tokens, logout,
certificate rollover, local disablement and token subject mapping. Broker/IdP
assertion tests require that actual deployment; local tests cover Mailhub's JWT
issuer/audience/expiry boundary. Do not claim native SAML endpoint support.

Broker reference: [Keycloak SAML identity-provider configuration](https://github.com/keycloak/keycloak/blob/main/docs/documentation/server_admin/topics/identity-broker/saml.adoc).
