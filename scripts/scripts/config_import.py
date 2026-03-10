#!/usr/bin/env python3
"""
Config Import Utility for go-emailservice-ads

Imports existing Postfix or Sendmail configurations and converts them
to go-emailservice-ads config.yaml format.

Usage:
    ./config_import.py --postfix /etc/postfix --output config.yaml
    ./config_import.py --sendmail /etc/mail --output config.yaml
"""

import argparse
import re
import sys
from pathlib import Path
from typing import Dict, List, Optional, Any
import yaml


class PostfixConfigParser:
    """Parse Postfix main.cf and master.cf"""

    def __init__(self, postfix_dir: Path):
        self.postfix_dir = postfix_dir
        self.main_cf = postfix_dir / 'main.cf'
        self.master_cf = postfix_dir / 'master.cf'
        self.params = {}

    def parse(self) -> Dict[str, Any]:
        """Parse Postfix configuration"""
        if not self.main_cf.exists():
            raise FileNotFoundError(f"Postfix main.cf not found: {self.main_cf}")

        self._parse_main_cf()
        config = self._convert_to_ads_config()
        return config

    def _parse_main_cf(self):
        """Parse main.cf file"""
        with open(self.main_cf) as f:
            current_key = None
            current_value = []

            for line in f:
                line = line.rstrip()

                # Skip comments and empty lines
                if not line or line.startswith('#'):
                    continue

                # Continuation line (starts with whitespace)
                if line[0].isspace() and current_key:
                    current_value.append(line.strip())
                    continue

                # Save previous parameter
                if current_key:
                    self.params[current_key] = ' '.join(current_value)

                # New parameter
                if '=' in line:
                    key, value = line.split('=', 1)
                    current_key = key.strip()
                    current_value = [value.strip()]
                else:
                    current_key = None
                    current_value = []

            # Save last parameter
            if current_key:
                self.params[current_key] = ' '.join(current_value)

    def _convert_to_ads_config(self) -> Dict[str, Any]:
        """Convert Postfix params to ADS config"""
        config = {
            'server': {},
            'imap': {},
            'api': {
                'rest_addr': ':8080',
                'grpc_addr': ':50051',
                'api_keys': [],
                'allowed_ips': ['127.0.0.1', '::1'],
                'require_ip_auth': False
            },
            'auth': {'default_users': []},
            'logging': {'level': 'info'}
        }

        # Domain configuration
        myhostname = self.params.get('myhostname', 'localhost')
        mydomain = self.params.get('mydomain', myhostname)
        config['server']['domain'] = mydomain

        # Listen address
        inet_interfaces = self.params.get('inet_interfaces', 'all')
        if inet_interfaces == 'all':
            addr = ':25'
        elif inet_interfaces == 'localhost':
            addr = '127.0.0.1:25'
        else:
            addr = f'{inet_interfaces}:25'
        config['server']['addr'] = addr

        # Local domains
        mydestination = self.params.get('mydestination', '')
        local_domains = [d.strip() for d in mydestination.split(',') if d.strip()]
        config['server']['local_domains'] = local_domains or [mydomain]

        # Relay domains
        relay_domains = self.params.get('relay_domains', '')
        if relay_domains:
            domains = [d.strip() for d in relay_domains.split(',') if d.strip()]
            config['server']['relay_domains'] = domains

        # My networks
        mynetworks = self.params.get('mynetworks', '127.0.0.0/8')
        networks = [n.strip() for n in mynetworks.split(',') if n.strip()]
        config['server']['mynetworks'] = networks

        # Message size limit
        message_size_limit = self.params.get('message_size_limit', '10240000')
        try:
            config['server']['max_message_bytes'] = int(message_size_limit)
        except ValueError:
            config['server']['max_message_bytes'] = 10485760

        # SMTP restrictions
        restrictions = self._parse_restrictions()
        if restrictions:
            config['access_control'] = restrictions

        # TLS configuration
        tls_config = self._parse_tls()
        if tls_config:
            config['server']['tls'] = tls_config
            config['imap']['tls'] = tls_config

        # Virtual alias maps
        virtual_alias_maps = self.params.get('virtual_alias_maps', '')
        if virtual_alias_maps:
            config['virtual_alias_maps'] = virtual_alias_maps

        # Transport maps
        transport_maps = self.params.get('transport_maps', '')
        if transport_maps:
            config['transport_maps'] = transport_maps

        # Sender/recipient restrictions
        config['server']['require_auth'] = True
        config['server']['require_tls'] = 'smtpd_tls_security_level' in self.params

        return config

    def _parse_restrictions(self) -> Optional[Dict[str, List[str]]]:
        """Parse SMTP restrictions"""
        restrictions = {}

        restriction_keys = [
            'smtpd_client_restrictions',
            'smtpd_helo_restrictions',
            'smtpd_sender_restrictions',
            'smtpd_recipient_restrictions',
            'smtpd_data_restrictions',
            'smtpd_end_of_data_restrictions'
        ]

        for key in restriction_keys:
            if key in self.params:
                value = self.params[key]
                # Remove 'smtpd_' prefix and convert to our format
                new_key = key.replace('smtpd_', '').replace('_restrictions', '_restrictions')
                restrictions[new_key] = [r.strip() for r in value.split(',')]

        return restrictions if restrictions else None

    def _parse_tls(self) -> Optional[Dict[str, str]]:
        """Parse TLS configuration"""
        tls_cert = self.params.get('smtpd_tls_cert_file')
        tls_key = self.params.get('smtpd_tls_key_file')

        if tls_cert and tls_key:
            return {
                'cert': tls_cert,
                'key': tls_key
            }
        return None


class SendmailConfigParser:
    """Parse Sendmail sendmail.cf"""

    def __init__(self, sendmail_dir: Path):
        self.sendmail_dir = sendmail_dir
        self.sendmail_cf = sendmail_dir / 'sendmail.cf'
        self.params = {}
        self.macros = {}

    def parse(self) -> Dict[str, Any]:
        """Parse Sendmail configuration"""
        if not self.sendmail_cf.exists():
            raise FileNotFoundError(f"Sendmail sendmail.cf not found: {self.sendmail_cf}")

        self._parse_sendmail_cf()
        config = self._convert_to_ads_config()
        return config

    def _parse_sendmail_cf(self):
        """Parse sendmail.cf file"""
        with open(self.sendmail_cf) as f:
            for line in f:
                line = line.strip()

                # Skip comments and empty lines
                if not line or line.startswith('#'):
                    continue

                # Macro definition (D)
                if line.startswith('D'):
                    match = re.match(r'D([A-Za-z])(.+)', line)
                    if match:
                        macro_name = match.group(1)
                        macro_value = match.group(2)
                        self.macros[macro_name] = macro_value

                # Option (O)
                elif line.startswith('O'):
                    match = re.match(r'O\s*([^=]+)=(.+)', line)
                    if match:
                        option_name = match.group(1).strip()
                        option_value = match.group(2).strip()
                        self.params[option_name] = option_value

    def _convert_to_ads_config(self) -> Dict[str, Any]:
        """Convert Sendmail params to ADS config"""
        config = {
            'server': {},
            'imap': {},
            'api': {
                'rest_addr': ':8080',
                'grpc_addr': ':50051',
                'api_keys': [],
                'allowed_ips': ['127.0.0.1', '::1'],
                'require_ip_auth': False
            },
            'auth': {'default_users': []},
            'logging': {'level': 'info'}
        }

        # Domain from $j macro
        domain = self.macros.get('j', 'localhost')
        config['server']['domain'] = domain

        # Default listen address
        config['server']['addr'] = ':25'

        # Local domains from $w macro
        local_domains = [domain]
        if 'w' in self.macros:
            local_domains.append(self.macros['w'])
        config['server']['local_domains'] = local_domains

        # Message size limit from MaxMessageSize option
        max_message_size = self.params.get('MaxMessageSize', '10485760')
        try:
            config['server']['max_message_bytes'] = int(max_message_size)
        except ValueError:
            config['server']['max_message_bytes'] = 10485760

        # TLS from CertFile and KeyFile options
        cert_file = self.params.get('CertFile') or self.params.get('ServerCertFile')
        key_file = self.params.get('KeyFile') or self.params.get('ServerKeyFile')

        if cert_file and key_file:
            config['server']['tls'] = {
                'cert': cert_file,
                'key': key_file
            }
            config['imap']['tls'] = {
                'cert': cert_file,
                'key': key_file
            }

        # Authentication required
        config['server']['require_auth'] = True
        config['server']['require_tls'] = bool(cert_file)

        return config


def merge_configs(base: Dict[str, Any], imported: Dict[str, Any]) -> Dict[str, Any]:
    """Merge imported config with base config"""
    result = base.copy()

    for key, value in imported.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key].update(value)
        else:
            result[key] = value

    return result


def main():
    parser = argparse.ArgumentParser(
        description='Import Postfix/Sendmail config to go-emailservice-ads',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Import Postfix configuration
  %(prog)s --postfix /etc/postfix --output config.yaml

  # Import Sendmail configuration
  %(prog)s --sendmail /etc/mail --output config.yaml

  # Merge with existing config
  %(prog)s --postfix /etc/postfix --base-config config.yaml --output config-new.yaml

  # Dry run (print to stdout)
  %(prog)s --postfix /etc/postfix --dry-run
        """
    )

    parser.add_argument('--postfix', type=Path, help='Postfix configuration directory')
    parser.add_argument('--sendmail', type=Path, help='Sendmail configuration directory')
    parser.add_argument('--base-config', type=Path, help='Base config.yaml to merge with')
    parser.add_argument('--output', '-o', type=Path, default='config-imported.yaml',
                        help='Output file path')
    parser.add_argument('--dry-run', action='store_true',
                        help='Print config to stdout without writing')
    parser.add_argument('--verbose', '-v', action='store_true', help='Verbose output')

    args = parser.parse_args()

    if not args.postfix and not args.sendmail:
        parser.error("Either --postfix or --sendmail must be specified")

    if args.postfix and args.sendmail:
        parser.error("Cannot specify both --postfix and --sendmail")

    try:
        # Parse configuration
        if args.postfix:
            if args.verbose:
                print(f"Parsing Postfix configuration from {args.postfix}", file=sys.stderr)
            parser_obj = PostfixConfigParser(args.postfix)
            imported_config = parser_obj.parse()
        else:
            if args.verbose:
                print(f"Parsing Sendmail configuration from {args.sendmail}", file=sys.stderr)
            parser_obj = SendmailConfigParser(args.sendmail)
            imported_config = parser_obj.parse()

        # Merge with base config if provided
        if args.base_config:
            if args.verbose:
                print(f"Merging with base config {args.base_config}", file=sys.stderr)
            with open(args.base_config) as f:
                base_config = yaml.safe_load(f)
            final_config = merge_configs(base_config, imported_config)
        else:
            final_config = imported_config

        # Output configuration
        if args.dry_run:
            yaml.dump(final_config, sys.stdout, default_flow_style=False, sort_keys=False, indent=2)
        else:
            # Create backup if file exists
            if args.output.exists():
                backup_path = args.output.with_suffix('.yaml.backup')
                if args.verbose:
                    print(f"Backing up existing config to {backup_path}", file=sys.stderr)
                args.output.rename(backup_path)

            with open(args.output, 'w') as f:
                yaml.dump(final_config, f, default_flow_style=False, sort_keys=False, indent=2)

            print(f"Configuration imported to {args.output}", file=sys.stderr)
            print(f"\nNext steps:", file=sys.stderr)
            print(f"1. Review and customize {args.output}", file=sys.stderr)
            print(f"2. Import any custom maps (virtual_alias_maps, transport_maps, etc.)", file=sys.stderr)
            print(f"3. Configure authentication (LDAP, SSO, or local users)", file=sys.stderr)
            print(f"4. Test configuration: ./bin/goemailservices -config {args.output} -test", file=sys.stderr)

            # Print warnings
            print(f"\nWarnings:", file=sys.stderr)
            print(f"  - Access control restrictions have been imported but may need adjustment", file=sys.stderr)
            print(f"  - Map files (hash:, regexp:, etc.) are referenced but not converted", file=sys.stderr)
            print(f"  - Custom Postfix/Sendmail features may not have equivalents", file=sys.stderr)
            print(f"  - Review TLS configuration and certificate paths", file=sys.stderr)

    except FileNotFoundError as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error importing configuration: {e}", file=sys.stderr)
        if args.verbose:
            import traceback
            traceback.print_exc()
        sys.exit(1)


if __name__ == '__main__':
    main()
