package config

// DefaultDocument requires provisioned TLS certificates and real accounts.
const DefaultDocument = `server:
  addr: ":587"
  domain: "localhost.local"
  max_message_bytes: 10485760
  max_recipients: 50
  allow_insecure_auth: false   # SECURITY: Require TLS for AUTH
  require_auth: true            # SECURITY: Require authentication
  require_tls: true             # SECURITY: Require STARTTLS
  mode: "test"
  tls:
    cert: "./data/certs/server.crt"
    key: "./data/certs/server.key"
imap:
  addr: ":1143"
  require_tls: true
  tls:
    cert: "./data/certs/server.crt"
    key: "./data/certs/server.key"
api:
  rest_addr: "127.0.0.1:8080"
  grpc_addr: "127.0.0.1:50051"
auth:
  default_users: [] # Provision real accounts; no shared default password.
aftersmtp:
  enabled: false
  ledger_url: "ws://127.0.0.1:9944"
  quic_addr: ":4434"
  grpc_addr: ":4433"
  fallback_db: "fallback_ledger.db"
logging:
  level: "debug"
`
