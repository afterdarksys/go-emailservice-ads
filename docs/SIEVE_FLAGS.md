# Sieve delivery flags and named flag sets

Install per-user scripts as described in [QA/UAT](QA_UAT.md). Scripts are read
on delivery; replace them atomically with service-account ownership. Invalid
programs defer delivery for retry. No restart is needed to replace a script. Once an incoming message has an evaluated
execution plan, retries retain that plan; replacements apply to new messages. See
[Sieve workflows](SIEVE_WORKFLOWS.md) for multiple actions and recovery.

Declare `imap4flags` before using flag operations and `variables` before using
named flag sets or `set`. All `require` statements must precede other commands.
This example routes tagged messages with a separate named flag set:

```sieve
require ["fileinto", "imap4flags", "variables"];
setflag "project_flags" ["\\Seen", "\\Flagged", "project-review"];
removeflag "project_flags" "\\Seen";
if hasflag :contains "project_flags" "review" {
    fileinto :flags "${project_flags}" "Projects/Review";
    stop;
}
```

## Flag operations

`setflag` replaces a set; `addflag` adds values; `removeflag` removes them.
One string-list argument targets the default flags. With `variables` declared,
a preceding string names an independent variable:

```sieve
require ["imap4flags", "variables"];
addflag ["\\Seen", "customer"];
setflag "selected" "\\Flagged customer";
removeflag "selected" "CUSTOMER";
keep :flags "${selected}";
```

Lists may contain space-separated names. Empty values, invalid flags, `\Recent`
and unsupported system flags are ignored. Duplicates and removal are
case-insensitive. Named sets are exposed to substitution as space-separated
strings. Writable names are constant ASCII identifiers beginning with a letter
or underscore; namespaces and numeric capture variables cannot be assigned.

`keep :flags` and `fileinto :flags` override default flags for that delivery.
`keep :flags ""` explicitly delivers without flags. Delivery actions capture the
current values; later flag changes do not alter an already selected delivery.
An implicit keep uses the final default set.

## Matching

`hasflag` supports `:is`, `:contains` and `:matches`. It succeeds on any matching
flag from any selected variable. Omit the variable-list to test default flags.
The default comparator is `i;ascii-casemap`; `:comparator "i;octet"` selects
case-sensitive matching. Unknown or duplicate options are errors.

```sieve
require ["fileinto", "imap4flags", "variables"];
setflag "labels" "Project-Blue";
if hasflag :matches "labels" "project-*" {
    fileinto "Projects/${1}";
}
```

Successful wildcard matching supplies `${0}` and numbered captures, preserving
source case. Failed or skipped matches leave previous captures intact; a new
successful match replaces them. Substitution takes one pass, uses current
values, and occurs only with `variables` declared. Each evaluation starts fresh.
Use doubled backslashes in quoted Sieve strings; `\n` is not a C-style newline
escape. Wildcard literals need the additional pattern-escape level.

## Qualification and limits

Run `go test ./internal/policy ./internal/smtpd`. Regressions exercise parser
failures, overrides, named sets, comparators, captures, actual stored mailbox
flags and duplicate delivery retries. These also run under the release race suite.

This completes the flag compatibility work described here, not every Sieve
extension. Multiple delivery actions, redirect, vacation, relational comparisons,
variable modifiers, namespaces and the `string` test remain unsupported. Unknown
capabilities and unsupported syntax fail explicitly. Existing scripts that relied
on omitted declarations, misplaced `require` statements or extra positional flag
arguments need correction; use a single string-list for default flags.

Protocol references: [IMAP flags extension](https://www.rfc-editor.org/rfc/rfc5232.html)
and [Sieve variables](https://www.rfc-editor.org/rfc/rfc5229.html).
