# IP Reputation Check Policy
# Blocks or defers mail from low-reputation IPs

def evaluate():
    """Check sender IP reputation"""

    ip = get_remote_ip()
    reputation = get_ip_reputation()

    log("info", "IP reputation check: %s = %d" % (ip, reputation))

    # Block known bad actors (reputation < 20)
    if reputation < 20:
        reject("Your IP address has a poor reputation (score: %d). Please contact abuse@ if you believe this is an error." % reputation)
        return

    # Defer suspicious IPs (reputation 20-40)
    if reputation < 40:
        defer("Temporarily deferred due to sender reputation (score: %d)" % reputation, retry_after=1800)
        return

    # Warn on medium reputation (40-60)
    if reputation < 60:
        add_header("X-IP-Reputation", "Medium (%d)" % reputation)
        add_header("X-Warning", "Sender has medium IP reputation")
        log("warn", "Medium IP reputation: %s = %d" % (ip, reputation))

    # Tag high reputation senders (80+)
    if reputation >= 80:
        add_header("X-IP-Reputation", "High (%d)" % reputation)
        log("info", "High IP reputation: %s = %d" % (ip, reputation))

    accept()

evaluate()