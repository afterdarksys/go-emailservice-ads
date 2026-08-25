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

# Paw Mail recipient gate (brooklyncats issue #1). paw.brooklyncats.show is
# the canon-cat inbox subdomain: it accepts ONLY the addresses listed here —
# the list mirrors game/engine/mail/canon_cats.yaml in brooklyncats.show and
# must change in lockstep with it. Every other paw RCPT gets a hard 550 at
# SMTP time: no store, no LLM spend downstream, no backscatter. Scoped
# strictly to @paw.brooklyncats.show so no other domain's mail is touched.
PAW_MAIL_ALLOWED = ("rycat@paw.brooklyncats.show",)

def _paw_gate():
    for rcpt in envelope_to():
        # Envelope recipients may arrive as "<addr>" or "Name <addr>" —
        # normalize to the bare addr-spec before matching.
        r = rcpt.strip().lower()
        if "<" in r:
            r = r[r.rfind("<") + 1:]
        r = r.strip("<> \t")
        if r.endswith("@paw.brooklyncats.show"):
            log_entry("paw-mail: envelope recipient " + r)
            if r not in PAW_MAIL_ALLOWED:
                # bounce() is the action this deployed proxy build maps to a
                # hard "550 Rejected" at end-of-DATA (reply_with_smtp_error is
                # newer than the running binary's action switch and would fall
                # through to accept — verified against proxy.go on 2026-08-24).
                log_entry("paw-mail: 550 unknown recipient " + r)
                bounce()
                return True
    return False

def evaluate():
    if _paw_gate():
        return
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
