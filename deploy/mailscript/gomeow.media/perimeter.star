"""Baseline perimeter policy for the shared mailhub (apps.afterdarksys.com),
fronting gomeow.media, brooklyncats.show, bedstuy.studio, and afterdarksys.com.

Deliberately conservative for the initial rollout: enforce only the checks
with near-zero false-positive risk (malicious attachments, a real AV hit),
and log-only for everything score-based (auth failures) until real production
traffic has been observed. Tighten (add `if get_score() >= N: quarantine()`)
once that data exists.

Quarantine here means "route to Junk instead of INBOX, don't drop" — see
--forward-quarantine and EmailService's content_filter in emailservice.yaml.
MailScript adds the X-MailScript-Quarantine control header itself when a
script calls quarantine(); EmailService accepts that header only from the
private MailScript network and strips it before storage either way.
"""

def evaluate():
    auth = verify_auth()
    if not auth["authenticated"]:
        add_score(4.0, "sender failed aligned SPF/DKIM authentication")
    if auth["arc"] not in ("none", "pass"):
        add_score(3.0, "ARC chain did not verify: " + auth["arc"])
    if forged_auth_results():
        add_score(5.0, "forged Authentication-Results header")

    # Dangerous attachment types: quarantine outright. Low false-positive
    # risk — legitimate mail essentially never carries a raw executable or
    # an Office macro attachment.
    if has_executable_attachment() or has_macro_attachment():
        quarantine()
        return

    # Real AV hit: quarantine. getvirusstatus() defaults to "unknown" when
    # clamd wasn't reachable, so an unreachable scanner never silently
    # passes as verified-clean — it just doesn't quarantine on that basis.
    if getvirusstatus() == "infected":
        quarantine()
        return

    # Authentication-score threshold: intentionally not enforced yet. Once
    # there is real traffic to review, replace the header-flag branch below
    # with:
    #   if get_score() >= 5:
    #       quarantine()
    #       return
    if get_score() >= 5:
        add_header("X-MailScript-Score-Flag", "would-quarantine")

    accept()
