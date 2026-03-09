# Finance Department Compliance Policy
# Archives all finance team communications and enforces encryption

def evaluate():
    """Ensure finance team compliance"""

    from_addr = get_from()
    to_addrs = get_to()

    # Check if sender or recipient is in finance team
    in_finance = False
    for email in [from_addr] + to_addrs:
        if is_in_group(email, "finance-team"):
            in_finance = True
            break

    if not in_finance:
        # Not finance related, accept
        accept()
        return

    # Archive all finance communications (7 years retention)
    add_header("X-Archive-Required", "true")
    add_header("X-Retention-Years", "7")
    add_header("X-Compliance-Tag", "SOX-Finance")

    # Check for encryption
    has_encryption = False
    if has_header("Content-Type"):
        content_type = get_header("Content-Type")
        if "encrypted" in content_type.lower() or "smime" in content_type.lower():
            has_encryption = True

    # Get external recipients
    external_recipients = []
    for recipient in to_addrs:
        if not is_in_group(recipient, "finance-team"):
            # Check if external domain
            if "@" in recipient:
                domain = recipient.split("@")[1]
                # Check if not internal domain
                # TODO: Check against local domains list
                if domain not in ["company.com"]:
                    external_recipients.append(recipient)

    # Require encryption for external communications
    if len(external_recipients) > 0 and not has_encryption:
        add_header("X-Encryption-Warning", "External communication without encryption")
        add_header("X-External-Recipients", ", ".join(external_recipients))
        log("warn", "Finance email to external recipients without encryption: %s" % ", ".join(external_recipients))

    # Check for financial keywords requiring special handling
    body = get_body().lower()
    subject = get_header("Subject").lower()
    content = body + " " + subject

    sensitive_keywords = [
        "earnings", "revenue", "profit", "loss",
        "merger", "acquisition", "insider",
        "material non-public", "mnpi",
        "quarterly results", "financial results"
    ]

    found_keywords = []
    for keyword in sensitive_keywords:
        if keyword in content:
            found_keywords.append(keyword)

    if len(found_keywords) > 0:
        # Sensitive financial information
        add_header("X-Sensitive-Financial", "true")
        add_header("X-Keywords-Detected", ", ".join(found_keywords))
        add_header("X-Priority", "High")
        log("warn", "Sensitive financial keywords detected: %s" % ", ".join(found_keywords))

        # If going external, require manager approval
        if len(external_recipients) > 0:
            fileinto("INBOX.Pending-Approval")
            notify("mailto:cfo@company.com",
                   "Sensitive financial email requires approval: %s" % subject)
            log("warn", "Email quarantined for CFO approval")
            return

    # Tag and accept
    add_header("X-Compliance-Checked", "true")
    accept()

evaluate()