# Additional defense after listener authorization and mandatory scanner checks.
# Decisions use the authenticated envelope and verification results supplied
# by the platform. Source IP alone does not grant relay privileges.
def filter(message):
    if message.virus_status == "infected":
        reject("Malware detected")
        return
    if message.internal and not message.authenticated:
        if not cidr_contains("10.0.0.0/8", message.remote_ip):
            quarantine("Review unexpected internal sender")
            return
    print("Evaluated sender: " + message.sender)
    accept()
