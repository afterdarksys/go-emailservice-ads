# Security Checks Policy
# Enforces SPF, DKIM, and DMARC authentication

def evaluate():
    """Check email authentication mechanisms"""

    spf = check_spf()
    dkim = check_dkim()
    dmarc = check_dmarc()

    from_domain = get_from().split("@")[1] if "@" in get_from() else ""

    # Log authentication results
    log("info", "Auth check: SPF=%s, DKIM=%s, DMARC=%s for domain %s" % (spf, dkim, dmarc, from_domain))

    # Strict DMARC enforcement
    if dmarc == "reject":
        reject("Message rejected due to DMARC policy (p=reject)")
        return

    if dmarc == "quarantine":
        fileinto("INBOX.Quarantine")
        add_header("X-DMARC-Quarantine", "true")
        log("warn", "Message quarantined due to DMARC policy")
        return

    # SPF and DKIM both failed - suspicious
    if spf == "fail" and dkim == "fail":
        # Check if this is a known forgery target
        high_value_domains = ["paypal.com", "amazon.com", "microsoft.com", "apple.com", "google.com"]

        if from_domain in high_value_domains:
            reject("Message rejected: Failed authentication from high-value domain")
            return
        else:
            # Defer for other domains
            defer("Temporary failure: Authentication failed", retry_after=3600)
            return

    # SPF failed alone
    if spf == "fail":
        add_header("X-SPF-Failed", "true")
        add_header("X-Warning", "SPF verification failed")
        log("warn", "SPF failed for %s" % from_domain)

    # DKIM failed alone
    if dkim == "fail":
        add_header("X-DKIM-Failed", "true")
        add_header("X-Warning", "DKIM verification failed")
        log("warn", "DKIM failed for %s" % from_domain)

    # Add authentication results headers
    add_header("X-SPF-Result", spf)
    add_header("X-DKIM-Result", dkim)
    add_header("X-DMARC-Result", dmarc)

    # Accept with warnings
    accept()

evaluate()