# Configuration Management Scripts

Two Python utilities for managing go-emailservice-ads configurations:

1. **config_bootstrap.py** - Generate new configurations from scratch
2. **config_import.py** - Import existing Postfix/Sendmail configurations

## Requirements

```bash
pip3 install pyyaml
```

## config_bootstrap.py

Bootstrap a new configuration with command-line arguments.

### Basic Usage

```bash
# Minimal setup
./config_bootstrap.py --domain msgs.global --output config.yaml

# Production setup with API key
./config_bootstrap.py --domain msgs.global \
  --generate-api-key \
  --allowed-ips 10.0.1.0/24,192.168.1.100 \
  --enable-ip-auth \
  --output config.yaml

# With relay domains
./config_bootstrap.py --domain msgs.global \
  --relay-domains customer1.com,customer2.com,customer3.com \
  --output config.yaml
```

### LDAP Configuration

```bash
./config_bootstrap.py --domain example.com \
  --enable-ldap \
  --ldap-server ldap.example.com \
  --ldap-base-dn "dc=example,dc=com" \
  --ldap-bind-dn "cn=mailservice,dc=example,dc=com" \
  --ldap-bind-password "secret" \
  --ldap-user-filter "(mail=%s)" \
  --output config.yaml
```

### SSO Configuration

```bash
./config_bootstrap.py --domain msgs.global \
  --enable-sso \
  --sso-client-id "your-client-id" \
  --sso-client-secret "your-client-secret" \
  --sso-redirect-url "https://msgs.global/oauth/callback" \
  --output config.yaml
```

### Elasticsearch Integration

```bash
./config_bootstrap.py --domain msgs.global \
  --enable-elasticsearch \
  --es-endpoints "http://es1.example.com:9200,http://es2.example.com:9200" \
  --es-api-key "your-es-api-key" \
  --output config.yaml
```

### Complete Production Example

```bash
./config_bootstrap.py \
  --domain msgs.global \
  --relay-domains partner1.com,partner2.com \
  --local-domains internal.msgs.global,msgs.local \
  --mode production \
  --smtp-port 2525 \
  --api-port 8080 \
  --generate-api-key \
  --api-key-name "Production API Key" \
  --admin-user admin \
  --test-user \
  --tls-cert /etc/letsencrypt/live/msgs.global/fullchain.pem \
  --tls-key /etc/letsencrypt/live/msgs.global/privkey.pem \
  --allowed-ips "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16" \
  --enable-ip-auth \
  --max-connections 2000 \
  --max-per-ip 20 \
  --rate-limit 500 \
  --enable-ldap \
  --ldap-server ldap.internal \
  --ldap-base-dn "dc=msgs,dc=global" \
  --ldap-bind-dn "cn=mailservice,dc=msgs,dc=global" \
  --ldap-bind-password "ldap-secret" \
  --enable-sso \
  --sso-client-id "\${ADS_CLIENT_ID}" \
  --sso-client-secret "\${ADS_CLIENT_SECRET}" \
  --enable-elasticsearch \
  --es-endpoints "http://elasticsearch:9200" \
  --es-api-key "\${ES_API_KEY}" \
  --output /etc/mail/config.yaml
```

### Command-Line Options

| Option | Description | Default |
|--------|-------------|---------|
| `--domain` | Primary domain (required) | - |
| `--relay-domains` | Comma-separated relay domains | - |
| `--local-domains` | Comma-separated local domains | `[domain, localhost]` |
| `--mode` | Server mode (production/test/development) | `production` |
| `--output`, `-o` | Output file path | `config.yaml` |
| `--smtp-port` | SMTP port | `2525` |
| `--imap-port` | IMAP port | `1143` |
| `--api-port` | REST API port | `8080` |
| `--grpc-port` | gRPC API port | `50051` |
| `--tls-cert` | TLS certificate path | `./data/certs/server.crt` |
| `--tls-key` | TLS private key path | `./data/certs/server.key` |
| `--api-key` | Specific API key to use | (generated) |
| `--api-key-name` | Name for API key | `Primary API Key` |
| `--generate-api-key` | Generate and print API key | `false` |
| `--allowed-ips` | Comma-separated IP whitelist | `127.0.0.1, ::1` |
| `--enable-ip-auth` | Enable IP whitelist enforcement | `false` |
| `--admin-user` | Admin username | `admin` |
| `--admin-password` | Admin password | (generated) |
| `--test-user` | Create test user | `false` |
| `--enable-ldap` | Enable LDAP auth | `false` |
| `--ldap-server` | LDAP server hostname | - |
| `--ldap-port` | LDAP port | `389` |
| `--ldap-use-tls` | Use LDAPS (port 636) | `false` |
| `--ldap-base-dn` | LDAP base DN | - |
| `--ldap-bind-dn` | LDAP bind DN | - |
| `--ldap-bind-password` | LDAP bind password | - |
| `--ldap-user-filter` | LDAP user search filter | `(mail=%s)` |
| `--enable-sso` | Enable SSO auth | `false` |
| `--sso-provider` | SSO provider name | `afterdarksystems` |
| `--sso-client-id` | OAuth2 client ID | `${ADS_CLIENT_ID}` |
| `--sso-client-secret` | OAuth2 client secret | `${ADS_CLIENT_SECRET}` |
| `--sso-redirect-url` | OAuth2 redirect URL | `https://domain/oauth/callback` |
| `--enable-elasticsearch` | Enable Elasticsearch | `false` |
| `--es-endpoints` | Comma-separated ES endpoints | `http://localhost:9200` |
| `--es-api-key` | Elasticsearch API key | `${ES_API_KEY}` |
| `--max-connections` | Max concurrent connections | `1000` |
| `--max-per-ip` | Max connections per IP | `10` |
| `--rate-limit` | Messages per hour per IP | `100` |

## config_import.py

Import existing Postfix or Sendmail configurations.

### Postfix Import

```bash
# Basic import
./config_import.py --postfix /etc/postfix --output config.yaml

# Verbose mode
./config_import.py --postfix /etc/postfix --output config.yaml --verbose

# Dry run (print to stdout)
./config_import.py --postfix /etc/postfix --dry-run

# Merge with existing config
./config_import.py --postfix /etc/postfix \
  --base-config config-base.yaml \
  --output config.yaml
```

### Sendmail Import

```bash
# Basic import
./config_import.py --sendmail /etc/mail --output config.yaml

# Verbose mode
./config_import.py --sendmail /etc/mail --output config.yaml --verbose
```

### What Gets Imported

#### From Postfix (main.cf)

- **Domain configuration**: `myhostname`, `mydomain`, `mydestination`
- **Listen address**: `inet_interfaces`
- **Local domains**: `mydestination`
- **Relay domains**: `relay_domains`
- **Networks**: `mynetworks`
- **Message size**: `message_size_limit`
- **SMTP restrictions**: All `smtpd_*_restrictions`
- **TLS config**: `smtpd_tls_cert_file`, `smtpd_tls_key_file`
- **Virtual aliases**: `virtual_alias_maps`
- **Transport maps**: `transport_maps`

#### From Sendmail (sendmail.cf)

- **Domain**: `$j` macro
- **Local domains**: `$w` macro
- **Message size**: `MaxMessageSize` option
- **TLS config**: `CertFile`, `KeyFile` options

### Important Notes

1. **Map Files Not Converted**: References to map files (hash:, regexp:, cidr:, etc.) are preserved but files are not converted. You'll need to manually convert or recreate these.

2. **Custom Features**: Postfix/Sendmail-specific features may not have direct equivalents in go-emailservice-ads.

3. **Review Required**: Always review the generated config before using in production.

4. **Restrictions May Need Adjustment**: SMTP restriction lists are imported but may need tweaking for compatibility.

### Post-Import Steps

1. **Review the generated config**:
   ```bash
   cat config-imported.yaml
   ```

2. **Convert map files** (if needed):
   ```bash
   # Example: Convert virtual alias map
   postmap -q - hash:/etc/postfix/virtual > virtual-aliases.txt
   # Then manually add to config or create ADS-compatible map
   ```

3. **Add API keys and authentication**:
   ```bash
   ./config_bootstrap.py --domain $(grep domain: config-imported.yaml | awk '{print $2}') \
     --generate-api-key --output config-merged.yaml
   # Then merge manually
   ```

4. **Test the configuration**:
   ```bash
   ./bin/goemailservices -config config-imported.yaml -test
   ```

## Example Workflows

### New Deployment

```bash
# 1. Bootstrap new config
./scripts/config_bootstrap.py \
  --domain msgs.global \
  --generate-api-key \
  --test-user \
  --enable-ip-auth \
  --allowed-ips 10.0.1.0/24 \
  --output config.yaml

# 2. Generate TLS certificates
certbot certonly --standalone -d msgs.global

# 3. Update TLS paths in config
# Edit config.yaml and set cert/key paths

# 4. Start server
./bin/goemailservices -config config.yaml
```

### Migrating from Postfix

```bash
# 1. Import existing Postfix config
./scripts/config_import.py \
  --postfix /etc/postfix \
  --output config-postfix.yaml \
  --verbose

# 2. Review imported config
cat config-postfix.yaml

# 3. Add API key and modern features
./scripts/config_bootstrap.py \
  --domain $(grep '  domain:' config-postfix.yaml | awk '{print $2}') \
  --generate-api-key \
  --enable-ip-auth \
  --output config-additions.yaml

# 4. Manually merge configs or use base-config
./scripts/config_import.py \
  --postfix /etc/postfix \
  --base-config config-additions.yaml \
  --output config-final.yaml

# 5. Convert any map files (manual step)
# 6. Test configuration
./bin/goemailservices -config config-final.yaml -test

# 7. Deploy
systemctl stop postfix
./bin/goemailservices -config config-final.yaml
```

### Multi-Domain Setup

```bash
# Domain 1: msgs.global (primary)
./scripts/config_bootstrap.py \
  --domain msgs.global \
  --relay-domains relay1.com,relay2.com \
  --generate-api-key \
  --output config-msgs-global.yaml

# Domain 2: internal.local (internal mail)
./scripts/config_bootstrap.py \
  --domain internal.local \
  --local-domains internal.local,corp.local \
  --enable-ldap \
  --ldap-server ldap.internal \
  --ldap-base-dn "dc=internal,dc=local" \
  --output config-internal.yaml

# Merge both (manual step or use YAML merge tools)
```

## Troubleshooting

### Missing YAML Module

```bash
pip3 install pyyaml
```

### Permission Denied

```bash
chmod +x scripts/config_bootstrap.py scripts/config_import.py
```

### Postfix Import Fails

Check that `/etc/postfix/main.cf` exists and is readable:
```bash
ls -la /etc/postfix/main.cf
```

### Generated Config Has Errors

Use the validation mode (if implemented):
```bash
./bin/goemailservices -config config.yaml -validate
```

## See Also

- [Configuration Reference](../docs/CONFIGURATION.md)
- [API Authentication](../API_AUTHENTICATION.md)
- [Security Guide](../SECURITY.md)
- [LDAP Integration](../docs/LDAP.md)
- [SSO Setup](../SSO_SETUP.md)
