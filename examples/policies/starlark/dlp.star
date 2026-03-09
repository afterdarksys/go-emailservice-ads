# Data Loss Prevention (DLP) Policy
# Prevents sensitive data from leaving the organization

def evaluate():
    """Scan outbound email for sensitive data"""

    body = get_body()
    subject = get_header("Subject")

    # Patterns to detect
    patterns = {
        "SSN": r"\b\d{3}-\d{2}-\d{4}\b",
        "Credit Card": r"\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b",
        "API Key": r"\b[A-Za-z0-9]{32,}\b",
        "Password": r"password\s*[:=]\s*\S+",
        "Confidential": r"\b(confidential|secret|internal|proprietary)\b",
    }

    violations = []

    # Check body and subject
    content = (body + " " + subject).lower()

    for pattern_name, pattern in patterns.items():
        if match_pattern(content, pattern):
            violations.append(pattern_name)

    # Check attachments
    attachments = get_attachments()
    sensitive_extensions = [".xls", ".xlsx", ".doc", ".docx", ".pdf", ".csv"]

    has_sensitive_attachment = False
    for attachment in attachments:
        if attachment.extension.lower() in sensitive_extensions:
            has_sensitive_attachment = True
            break

    # Decision logic
    if len(violations) > 0:
        if len(violations) >= 3 or "Credit Card" in violations or "SSN" in violations:
            # Critical violation - block
            reject("Message blocked by DLP policy. Sensitive data detected: %s" % ", ".join(violations))
            log("warn", "DLP: Blocked message with violations: %s" % ", ".join(violations))
        else:
            # Minor violation - quarantine for review
            fileinto("INBOX.DLP-Review")
            add_header("X-DLP-Violations", ", ".join(violations))
            log("info", "DLP: Quarantined message for review: %s" % ", ".join(violations))
    elif has_sensitive_attachment:
        # Sensitive attachment - add warning header
        add_header("X-DLP-Warning", "Contains sensitive attachment")
        log("info", "DLP: Message contains sensitive attachment")
        accept()
    else:
        # Clean
        accept()

evaluate()