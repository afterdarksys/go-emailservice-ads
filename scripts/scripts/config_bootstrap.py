#!/usr/bin/env python3
"""
Config Bootstrap Utility for go-emailservice-ads

Generates a production-ready config.yaml from command-line arguments.

Usage:
    ./config_bootstrap.py --domain msgs.global --relay-domains example.com,test.com \\
        --api-key "your-api-key-here" --enable-ldap --ldap-server ldap.example.com

"""

import argparse
import secrets
import sys
import yaml
from pathlib import Path


def generate_api_key():
    """Generate a cryptographically secure API key"""
    return secrets.token_urlsafe(48)


def generate_password(length=16):
    """Generate a secure password"""
    return secrets.token_urlsafe(length)


def create_base_config():
    """Create base configuration structure"""
    return {
        'server': {
            'addr': ':2525',
            'domain': 'localhost.local',
            'max_message_bytes': 10485760,
            'max_recipients': 50,
            'allow_insecure_auth': False,
            'require_auth': True,
            'require_tls': True,
            'mode': 'production',
            'max_connections': 1000,
            'max_per_ip': 10,
            'rate_limit_per_ip': 100,
            'enable_greylist': False,
            'disable_vrfy': True,
            'disable_expn': True,
            'local_domains': ['localhost', 'localhost.local'],
            'tls': {
                'cert': './data/certs/server.crt',
                'key': './data/certs/server.key'
            }
        },
        'imap': {
            'addr': ':1143',
            'require_tls': True,
            'tls': {
                'cert': './data/certs/server.crt',
                'key': './data/certs/server.key'
            }
        },
        'api': {
            'rest_addr': ':8080',
            'grpc_addr': ':50051',
            'api_keys': [],
            'allowed_ips': ['127.0.0.1', '::1'],
            'require_ip_auth': False
        },
        'auth': {
            'default_users': []
        },
        'sso': {
            'enabled': False,
            'provider': 'afterdarksystems',
            'directory_url': 'https://directory.msgs.global',
            'auth_url': 'https://sso.afterdarksystems.com/oauth2/authorize',
            'token_url': 'https://sso.afterdarksystems.com/oauth2/token',
            'userinfo_url': 'https://sso.afterdarksystems.com/oauth2/userinfo',
            'client_id': '${ADS_CLIENT_ID}',
            'client_secret': '${ADS_CLIENT_SECRET}',
            'redirect_url': 'https://example.com/oauth/callback',
            'scopes': ['openid', 'email', 'profile']
        },
        'aftersmtp': {
            'enabled': True,
            'ledger_url': 'ws://127.0.0.1:9944',
            'quic_addr': ':4434',
            'grpc_addr': ':4433',
            'fallback_db': './data/fallback_ledger.db'
        },
        'logging': {
            'level': 'info'
        },
        'elasticsearch': {
            'enabled': False,
            'endpoints': ['http://localhost:9200'],
            'index_prefix': 'mail-events',
            'bulk_size': 1000,
            'flush_interval': '5s',
            'api_key': '${ES_API_KEY}',
            'retention_days': 90,
            'replicas': 1,
            'shards': 3
        }
    }


def main():
    parser = argparse.ArgumentParser(
        description='Bootstrap go-emailservice-ads configuration',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Basic setup
  %(prog)s --domain msgs.global --output config.yaml

  # With relay domains
  %(prog)s --domain msgs.global --relay-domains example.com,test.com

  # Full LDAP setup
  %(prog)s --domain msgs.global --enable-ldap \\
    --ldap-server ldap.example.com --ldap-base-dn "dc=example,dc=com" \\
    --ldap-bind-dn "cn=admin,dc=example,dc=com" --ldap-bind-password "secret"

  # Production with API key and IP whitelist
  %(prog)s --domain msgs.global --api-key "generated-key" \\
    --allowed-ips 10.0.1.0/24,192.168.1.100 --enable-ip-auth

  # Enable SSO
  %(prog)s --domain msgs.global --enable-sso \\
    --sso-client-id "client-id" --sso-client-secret "secret"
        """
    )

    # Basic options
    parser.add_argument('--domain', required=True, help='Primary domain (e.g., msgs.global)')
    parser.add_argument('--relay-domains', help='Comma-separated list of relay domains')
    parser.add_argument('--local-domains', help='Comma-separated list of local domains')
    parser.add_argument('--output', '-o', default='config.yaml', help='Output file path')
    parser.add_argument('--mode', choices=['production', 'test', 'development'],
                        default='production', help='Server mode')

    # TLS options
    parser.add_argument('--tls-cert', help='Path to TLS certificate')
    parser.add_argument('--tls-key', help='Path to TLS private key')

    # API options
    parser.add_argument('--api-key', help='API key for REST API (generates one if not provided)')
    parser.add_argument('--api-key-name', default='Primary API Key', help='Name for API key')
    parser.add_argument('--generate-api-key', action='store_true', help='Generate and print API key')
    parser.add_argument('--allowed-ips', help='Comma-separated list of allowed IPs/CIDRs')
    parser.add_argument('--enable-ip-auth', action='store_true', help='Enable IP whitelist enforcement')

    # User accounts
    parser.add_argument('--admin-user', default='admin', help='Admin username')
    parser.add_argument('--admin-password', help='Admin password (generates one if not provided)')
    parser.add_argument('--test-user', action='store_true', help='Create test user account')

    # LDAP options
    parser.add_argument('--enable-ldap', action='store_true', help='Enable LDAP authentication')
    parser.add_argument('--ldap-server', help='LDAP server hostname')
    parser.add_argument('--ldap-port', type=int, default=389, help='LDAP port')
    parser.add_argument('--ldap-use-tls', action='store_true', help='Use LDAPS (port 636)')
    parser.add_argument('--ldap-base-dn', help='LDAP base DN')
    parser.add_argument('--ldap-bind-dn', help='LDAP bind DN')
    parser.add_argument('--ldap-bind-password', help='LDAP bind password')
    parser.add_argument('--ldap-user-filter', default='(mail=%s)',
                        help='LDAP user search filter')

    # SSO options
    parser.add_argument('--enable-sso', action='store_true', help='Enable SSO authentication')
    parser.add_argument('--sso-provider', default='afterdarksystems', help='SSO provider name')
    parser.add_argument('--sso-client-id', help='OAuth2 client ID')
    parser.add_argument('--sso-client-secret', help='OAuth2 client secret')
    parser.add_argument('--sso-redirect-url', help='OAuth2 redirect URL')

    # Elasticsearch options
    parser.add_argument('--enable-elasticsearch', action='store_true', help='Enable Elasticsearch')
    parser.add_argument('--es-endpoints', help='Comma-separated Elasticsearch endpoints')
    parser.add_argument('--es-api-key', help='Elasticsearch API key')

    # Network options
    parser.add_argument('--smtp-port', type=int, default=2525, help='SMTP port')
    parser.add_argument('--imap-port', type=int, default=1143, help='IMAP port')
    parser.add_argument('--api-port', type=int, default=8080, help='REST API port')
    parser.add_argument('--grpc-port', type=int, default=50051, help='gRPC API port')

    # Security options
    parser.add_argument('--max-connections', type=int, default=1000, help='Max concurrent connections')
    parser.add_argument('--max-per-ip', type=int, default=10, help='Max connections per IP')
    parser.add_argument('--rate-limit', type=int, default=100, help='Messages per hour per IP')

    args = parser.parse_args()

    # Create base config
    config = create_base_config()

    # Apply domain settings
    config['server']['domain'] = args.domain
    config['server']['local_domains'] = [args.domain, 'localhost', 'localhost.local']

    if args.local_domains:
        additional_domains = [d.strip() for d in args.local_domains.split(',')]
        config['server']['local_domains'].extend(additional_domains)

    # Apply relay domains
    if args.relay_domains:
        relay_domains = [d.strip() for d in args.relay_domains.split(',')]
        config['server']['relay_domains'] = relay_domains

    # Apply mode
    config['server']['mode'] = args.mode

    # Apply ports
    config['server']['addr'] = f':{args.smtp_port}'
    config['imap']['addr'] = f':{args.imap_port}'
    config['api']['rest_addr'] = f':{args.api_port}'
    config['api']['grpc_addr'] = f':{args.grpc_port}'

    # Apply security settings
    config['server']['max_connections'] = args.max_connections
    config['server']['max_per_ip'] = args.max_per_ip
    config['server']['rate_limit_per_ip'] = args.rate_limit

    # TLS configuration
    if args.tls_cert and args.tls_key:
        config['server']['tls']['cert'] = args.tls_cert
        config['server']['tls']['key'] = args.tls_key
        config['imap']['tls']['cert'] = args.tls_cert
        config['imap']['tls']['key'] = args.tls_key

    # API key configuration
    api_key = args.api_key
    if not api_key and args.generate_api_key:
        api_key = generate_api_key()
        print(f"Generated API Key: {api_key}", file=sys.stderr)

    if api_key:
        config['api']['api_keys'].append({
            'name': args.api_key_name,
            'key': api_key,
            'description': f'API key for {args.domain}',
            'permissions': ['read', 'write']
        })

    # IP whitelist
    if args.allowed_ips:
        ips = [ip.strip() for ip in args.allowed_ips.split(',')]
        config['api']['allowed_ips'].extend(ips)

    config['api']['require_ip_auth'] = args.enable_ip_auth

    # User accounts
    admin_password = args.admin_password or generate_password()
    if not args.admin_password:
        print(f"Generated Admin Password: {admin_password}", file=sys.stderr)

    config['auth']['default_users'].append({
        'username': args.admin_user,
        'password': admin_password,
        'email': f'{args.admin_user}@{args.domain}'
    })

    if args.test_user:
        test_password = generate_password()
        print(f"Generated Test User Password: {test_password}", file=sys.stderr)
        config['auth']['default_users'].append({
            'username': 'testuser',
            'password': test_password,
            'email': f'testuser@{args.domain}'
        })

    # LDAP configuration
    if args.enable_ldap:
        if not args.ldap_server or not args.ldap_base_dn:
            print("Error: --ldap-server and --ldap-base-dn required when enabling LDAP",
                  file=sys.stderr)
            sys.exit(1)

        ldap_port = 636 if args.ldap_use_tls else args.ldap_port
        ldap_url = f"{'ldaps' if args.ldap_use_tls else 'ldap'}://{args.ldap_server}:{ldap_port}"

        config['ldap'] = {
            'enabled': True,
            'url': ldap_url,
            'base_dn': args.ldap_base_dn,
            'bind_dn': args.ldap_bind_dn or '',
            'bind_password': args.ldap_bind_password or '',
            'user_filter': args.ldap_user_filter,
            'timeout': 10,
            'start_tls': not args.ldap_use_tls
        }

    # SSO configuration
    if args.enable_sso:
        config['sso']['enabled'] = True
        if args.sso_client_id:
            config['sso']['client_id'] = args.sso_client_id
        if args.sso_client_secret:
            config['sso']['client_secret'] = args.sso_client_secret
        if args.sso_redirect_url:
            config['sso']['redirect_url'] = args.sso_redirect_url
        else:
            config['sso']['redirect_url'] = f'https://{args.domain}/oauth/callback'

    # Elasticsearch configuration
    if args.enable_elasticsearch:
        config['elasticsearch']['enabled'] = True
        if args.es_endpoints:
            endpoints = [e.strip() for e in args.es_endpoints.split(',')]
            config['elasticsearch']['endpoints'] = endpoints
        if args.es_api_key:
            config['elasticsearch']['api_key'] = args.es_api_key

    # Write config file
    output_path = Path(args.output)

    # Create backup if file exists
    if output_path.exists():
        backup_path = output_path.with_suffix('.yaml.backup')
        print(f"Backing up existing config to {backup_path}", file=sys.stderr)
        output_path.rename(backup_path)

    with open(output_path, 'w') as f:
        yaml.dump(config, f, default_flow_style=False, sort_keys=False, indent=2)

    print(f"Configuration written to {output_path}", file=sys.stderr)
    print(f"\nNext steps:", file=sys.stderr)
    print(f"1. Review and customize {output_path}", file=sys.stderr)
    print(f"2. Generate TLS certificates if needed", file=sys.stderr)
    print(f"3. Start the server: ./bin/goemailservices -config {output_path}", file=sys.stderr)

    # Print summary
    print(f"\nConfiguration Summary:", file=sys.stderr)
    print(f"  Domain: {args.domain}", file=sys.stderr)
    print(f"  SMTP Port: {args.smtp_port}", file=sys.stderr)
    print(f"  API Port: {args.api_port}", file=sys.stderr)
    print(f"  LDAP: {'Enabled' if args.enable_ldap else 'Disabled'}", file=sys.stderr)
    print(f"  SSO: {'Enabled' if args.enable_sso else 'Disabled'}", file=sys.stderr)
    print(f"  Elasticsearch: {'Enabled' if args.enable_elasticsearch else 'Disabled'}", file=sys.stderr)
    print(f"  IP Whitelist: {'Enabled' if args.enable_ip_auth else 'Disabled'}", file=sys.stderr)


if __name__ == '__main__':
    main()
