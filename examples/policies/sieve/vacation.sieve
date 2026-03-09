require ["vacation", "date", "relational"];

# CEO Vacation Responder
# Only respond during vacation period

# Check if current date is within vacation period
if currentdate :value "ge" "date" "2026-07-01" {
    if currentdate :value "le" "date" "2026-07-15" {
        vacation :days 7
                 :subject "Out of Office - CEO"
                 :from "ceo@company.com"
                 "Thank you for your email. I am currently out of the office
and will return on July 16, 2026.

For urgent matters, please contact my executive assistant at
ea@company.com or call +1-555-0100.

Best regards,
John Smith
CEO";
    }
}

keep;