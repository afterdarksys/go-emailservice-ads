# Baseline MailScript policy for gomeow.media. It is intentionally observe-only:
# deploy it first, then add reputation/content rules after reviewing decisions.
#
# For a quarantine decision, MailScript must add:
# X-MailScript-Quarantine: true
# before relaying. The EmailService accepts that control header only from the
# configured private MailScript network and removes it before storage.
log(msg="gomeow.media perimeter policy: accepted")
mail.deliver()
