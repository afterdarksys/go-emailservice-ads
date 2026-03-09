# Anti-Spam Policy using Starlark
# Checks multiple spam indicators and scores the message

def evaluate():
    """Main entry point for policy evaluation"""

    # Initialize spam score
    score = 0
    reasons = []

    # Check IP reputation
    ip_rep = get_ip_reputation()
    if ip_rep < 30:
        score += 5
        reasons.append("Low IP reputation (%d)" % ip_rep)

    # Check SPF, DKIM, DMARC
    spf = check_spf()
    dkim = check_dkim()
    dmarc = check_dmarc()

    if spf == "fail":
        score += 3
        reasons.append("SPF failed")

    if dkim == "fail":
        score += 3
        reasons.append("DKIM failed")

    if spf == "fail" and dkim == "fail":
        # Both failed - very suspicious
        score += 2
        reasons.append("Both SPF and DKIM failed")

    # Check RBLs
    rbls = ["zen.spamhaus.org", "bl.spamcop.net", "b.barracudacentral.org"]
    for rbl in rbls:
        if check_rbl(rbl):
            score += 4
            reasons.append("Listed in RBL: %s" % rbl)

    # Content analysis
    body = get_body().lower()
    subject = get_header("Subject").lower()

    # Spam keywords
    spam_keywords = [
        "viagra", "cialis", "pharmacy",
        "casino", "lottery", "winner",
        "nigerian prince", "inheritance",
        "click here now", "act now",
        "limited time", "urgent",
        "congratulations", "selected"
    ]

    keyword_matches = 0
    for keyword in spam_keywords:
        if keyword in body or keyword in subject:
            keyword_matches += 1

    if keyword_matches > 0:
        score += keyword_matches * 2
        reasons.append("Spam keywords found: %d" % keyword_matches)

    # Check for excessive caps in subject
    if subject:
        caps_count = sum(1 for c in subject if c.isupper())
        if len(subject) > 0 and (caps_count / len(subject)) > 0.5:
            score += 2
            reasons.append("Excessive capitals in subject")

    # Check for suspicious attachments
    attachments = get_attachments()
    dangerous_extensions = [".exe", ".scr", ".com", ".bat", ".pif", ".vbs", ".js"]

    for attachment in attachments:
        if attachment.extension in dangerous_extensions:
            score += 5
            reasons.append("Dangerous attachment: %s" % attachment.filename)

    # Make decision based on score
    if score >= 10:
        # Definite spam - reject
        reject("Message rejected as spam (score: %d). Reasons: %s" % (score, ", ".join(reasons)))
    elif score >= 6:
        # Probably spam - quarantine
        fileinto("INBOX.Spam")
        add_header("X-Spam-Score", str(score))
        add_header("X-Spam-Reasons", ", ".join(reasons))
        log("info", "Message quarantined as spam (score: %d)" % score)
    elif score >= 3:
        # Suspicious - tag and deliver
        add_header("X-Spam-Score", str(score))
        add_header("X-Spam-Flag", "Suspicious")
        log("info", "Message tagged as suspicious (score: %d)" % score)
        accept()
    else:
        # Clean - accept
        add_header("X-Spam-Score", "0")
        accept()

# Execute policy
evaluate()