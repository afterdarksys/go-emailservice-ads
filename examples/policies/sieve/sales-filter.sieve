require ["fileinto", "envelope", "subaddress", "imap4flags"];

# Sales Team Auto-Filing Rules

# High priority - quotes and proposals
if header :contains "subject" ["quote", "proposal", "pricing"] {
    addflag "\\Flagged";
    fileinto "INBOX.Sales.Priority";
    stop;
}

# Customer inquiries
if anyof (
    header :contains "subject" ["inquiry", "question", "information"],
    header :contains "from" ["customer", "client"]
) {
    fileinto "INBOX.Sales.Inquiries";
    stop;
}

# Leads and prospects
if header :contains "subject" ["interested", "demo", "trial"] {
    addflag "\\Flagged";
    fileinto "INBOX.Sales.Leads";
    stop;
}

# Contracts and legal
if anyof (
    header :contains "subject" ["contract", "agreement", "terms"],
    header :contains "from" "legal"
) {
    fileinto "INBOX.Sales.Contracts";
    stop;
}

# Meeting requests
if header :contains "subject" ["meeting", "call", "zoom", "schedule"] {
    fileinto "INBOX.Sales.Meetings";
    stop;
}

# Default: keep in inbox
keep;