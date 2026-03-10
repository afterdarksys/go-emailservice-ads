# API Authentication Guide

## Overview

The go-emailservice-ads REST API supports **two authentication methods**:
1. **API Key Authentication** (Bearer token) - Recommended for programmatic access
2. **Basic Authentication** - User-based authentication

---

## 1. API Key Authentication (Bearer Token)

**Recommended for:** Web platforms, mobile apps, automated scripts, CI/CD pipelines

### Your API Key

```
1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH
```

**Name:** Web Platform
**Permissions:** read, write
**Description:** API key for web platform integration

### Usage

#### cURL Example
```bash
curl -H "Authorization: Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH" \
  http://apps.afterdarksys.com:8080/api/v1/queue/stats
```

#### JavaScript (Fetch API)
```javascript
fetch('http://apps.afterdarksys.com:8080/api/v1/queue/stats', {
  headers: {
    'Authorization': 'Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH'
  }
})
.then(response => response.json())
.then(data => console.log(data));
```

#### Python (requests)
```python
import requests

headers = {
    'Authorization': 'Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH'
}

response = requests.get(
    'http://apps.afterdarksys.com:8080/api/v1/queue/stats',
    headers=headers
)
print(response.json())
```

#### Go
```go
req, _ := http.NewRequest("GET", "http://apps.afterdarksys.com:8080/api/v1/queue/stats", nil)
req.Header.Set("Authorization", "Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH")

client := &http.Client{}
resp, err := client.Do(req)
```

---

## 2. Basic Authentication

**Recommended for:** Testing, interactive use, admin access

### Available Users

#### User 1: testuser
- **Username:** `testuser`
- **Password:** `testpass123`
- **Email:** `testuser@localhost.local`

#### User 2: admin
- **Username:** `admin`
- **Password:** `admin123`
- **Email:** `admin@localhost.local`

### Usage

#### cURL Example
```bash
curl -u testuser:testpass123 http://apps.afterdarksys.com:8080/api/v1/queue/stats
# or
curl -u admin:admin123 http://apps.afterdarksys.com:8080/api/v1/queue/stats
```

#### JavaScript (Fetch API)
```javascript
// Method 1: Using basic auth header
const credentials = btoa('testuser:testpass123');
fetch('http://apps.afterdarksys.com:8080/api/v1/queue/stats', {
  headers: {
    'Authorization': 'Basic ' + credentials
  }
})
.then(response => response.json())
.then(data => console.log(data));

// Method 2: Using URL
fetch('http://testuser:testpass123@apps.afterdarksys.com:8080/api/v1/queue/stats')
  .then(response => response.json())
  .then(data => console.log(data));
```

#### Python (requests)
```python
import requests

response = requests.get(
    'http://apps.afterdarksys.com:8080/api/v1/queue/stats',
    auth=('testuser', 'testpass123')
)
print(response.json())
```

---

## API Endpoints

### Public Endpoints (No Authentication Required)

#### Health Check
```bash
GET /health
```
Returns service health status and uptime.

**Example:**
```bash
curl http://apps.afterdarksys.com:8080/health
```

**Response:**
```json
{
  "status": "ok",
  "uptime": "5m30s"
}
```

#### Readiness Check
```bash
GET /ready
```
Returns service readiness status.

#### Prometheus Metrics
```bash
GET /metrics
```
Returns Prometheus-formatted metrics.

---

### Protected Endpoints (Authentication Required)

All endpoints below require either **API Key** or **Basic Auth**.

#### Queue Management

##### Get Queue Statistics
```bash
GET /api/v1/queue/stats
```

**Example with API Key:**
```bash
curl -H "Authorization: Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH" \
  http://apps.afterdarksys.com:8080/api/v1/queue/stats
```

**Response:**
```json
{
  "metrics": {
    "enqueued": {},
    "processed": {},
    "failed": {},
    "duplicates": 0,
    "last_update": "2026-03-10T01:00:00Z"
  },
  "storage": {
    "total": 0,
    "pending": 0,
    "processing": 0,
    "dlq": 0
  }
}
```

##### Get Pending Messages
```bash
GET /api/v1/queue/pending?tier=<tier>
```

**Parameters:**
- `tier` (optional): Queue tier (emergency, msa, int, out, bulk)

**Example:**
```bash
curl -H "Authorization: Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH" \
  "http://apps.afterdarksys.com:8080/api/v1/queue/pending?tier=out"
```

#### Policy Management

##### List Policies
```bash
GET /api/v1/policies
```

##### Get Policy Statistics
```bash
GET /api/v1/policies/stats
```

##### Reload Policies
```bash
POST /api/v1/policies/reload
```

#### Dead Letter Queue (DLQ)

##### List DLQ Messages
```bash
GET /api/v1/dlq/list
```

##### Retry DLQ Message
```bash
POST /api/v1/dlq/retry/<message_id>
```

**Example:**
```bash
curl -X POST \
  -H "Authorization: Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH" \
  http://apps.afterdarksys.com:8080/api/v1/dlq/retry/msg-12345
```

#### Message Management

##### Get Message Details
```bash
GET /api/v1/message/<message_id>
```

**Example:**
```bash
curl -H "Authorization: Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH" \
  http://apps.afterdarksys.com:8080/api/v1/message/msg-12345
```

##### Delete Message
```bash
DELETE /api/v1/message/<message_id>
```

**Example:**
```bash
curl -X DELETE \
  -H "Authorization: Bearer 1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH" \
  http://apps.afterdarksys.com:8080/api/v1/message/msg-12345
```

#### Replication Management

##### Get Replication Status
```bash
GET /api/v1/replication/status
```

##### Promote to Primary
```bash
POST /api/v1/replication/promote
```

---

## Security Best Practices

### 1. API Key Management

✅ **DO:**
- Store API keys in environment variables or secure secret management systems
- Use HTTPS in production (currently HTTP for testing)
- Rotate API keys periodically
- Use different API keys for different environments (dev, staging, production)
- Monitor API key usage and set up alerts for unusual activity

❌ **DON'T:**
- Hardcode API keys in your source code
- Commit API keys to version control
- Share API keys in plain text (Slack, email, etc.)
- Use the same API key across multiple applications

### 2. Production Deployment

Before deploying to production:

1. **Change default passwords** in `config.yaml`:
   ```yaml
   auth:
     default_users:
     - username: "admin"
       password: "STRONG_RANDOM_PASSWORD"  # Change this!
       email: "admin@yourdomain.com"
   ```

2. **Enable HTTPS/TLS** for all API endpoints

3. **Implement rate limiting** per API key

4. **Set up monitoring** for failed authentication attempts

5. **Rotate API keys** regularly (recommended: every 90 days)

---

## Adding New API Keys

To add additional API keys, edit `config.yaml`:

```yaml
api:
  rest_addr: ":8080"
  grpc_addr: ":50051"
  api_keys:
  - name: "Web Platform"
    key: "1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH"
    description: "API key for web platform integration"
    permissions: ["read", "write"]
  - name: "Mobile App"
    key: "YOUR_NEW_API_KEY_HERE"
    description: "API key for mobile application"
    permissions: ["read", "write"]
  - name: "Analytics Service"
    key: "ANOTHER_API_KEY_HERE"
    description: "Read-only key for analytics"
    permissions: ["read"]
```

### Generate a New API Key

```bash
openssl rand -base64 48 | tr -d '\n'
```

After adding keys, restart the service:
```bash
ssh root@apps.afterdarksys.com 'cd /opt/go-emailservice-ads && docker-compose restart mail-primary'
```

---

## Error Responses

### 401 Unauthorized

**Missing or invalid credentials:**
```json
{
  "error": "Unauthorized - provide API key or Basic Auth"
}
```

**Invalid API key:**
```json
{
  "error": "Invalid API key"
}
```

### 403 Forbidden

User authenticated but lacks permission for the requested resource.

### 405 Method Not Allowed

Incorrect HTTP method for the endpoint.

---

## Testing the API

### Quick Test Script (Bash)

```bash
#!/bin/bash
API_KEY="1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH"
BASE_URL="http://apps.afterdarksys.com:8080"

echo "Testing API Key authentication..."
curl -H "Authorization: Bearer $API_KEY" "$BASE_URL/api/v1/queue/stats"

echo -e "\n\nTesting Basic Auth..."
curl -u testuser:testpass123 "$BASE_URL/api/v1/queue/stats"

echo -e "\n\nTesting invalid credentials..."
curl -u wrong:credentials "$BASE_URL/api/v1/queue/stats"
```

### Integration Test (Python)

```python
import requests
import json

API_KEY = "1lHlAbGYzOtDn2F1Muw2hkpktgNcQ1aCPA57s6DUfXSTHaDfuzN+YXMADUW2BIoH"
BASE_URL = "http://apps.afterdarksys.com:8080"

def test_api_key_auth():
    """Test API key authentication"""
    headers = {"Authorization": f"Bearer {API_KEY}"}
    response = requests.get(f"{BASE_URL}/api/v1/queue/stats", headers=headers)
    assert response.status_code == 200
    print("✓ API key authentication works")
    return response.json()

def test_basic_auth():
    """Test basic authentication"""
    response = requests.get(
        f"{BASE_URL}/api/v1/queue/stats",
        auth=("testuser", "testpass123")
    )
    assert response.status_code == 200
    print("✓ Basic authentication works")
    return response.json()

def test_invalid_auth():
    """Test invalid credentials are rejected"""
    response = requests.get(
        f"{BASE_URL}/api/v1/queue/stats",
        auth=("wrong", "credentials")
    )
    assert response.status_code == 401
    print("✓ Invalid credentials properly rejected")

if __name__ == "__main__":
    print("Running API authentication tests...\n")
    test_api_key_auth()
    test_basic_auth()
    test_invalid_auth()
    print("\n✓ All tests passed!")
```

---

## Support & Troubleshooting

### Common Issues

**Issue:** `401 Unauthorized`
- **Solution:** Check that your API key or credentials are correct
- Verify the Authorization header format: `Bearer <key>` or `Basic <base64>`

**Issue:** `Invalid API key`
- **Solution:** Ensure the API key matches exactly (no extra spaces, newlines)
- Check that the config.yaml has been updated with the key

**Issue:** Connection refused
- **Solution:** Verify the service is running: `docker ps --filter name=mail-primary`
- Check logs: `docker logs mail-primary`

### Logs

Check authentication attempts in the logs:
```bash
ssh root@apps.afterdarksys.com 'docker logs -f mail-primary | grep -i auth'
```

---

**Last Updated:** 2026-03-10
**API Version:** v1
**Service:** go-emailservice-ads v2.1.0
