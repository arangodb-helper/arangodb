# ArangoDB Config File Update Proposal

## Context

The ArangoDB starter creates config files for all the `arangod` process
it starts. These config files contain various settings such as authentication
and server addresses.

The user is allowed to manually edit these config files.

Once the starter has created such a config file, it currently never touches
it again. This means that when you specify something else on the commandline
of the starter, it will not be reflected in the config file.
For example, when you initially run the starter without authentication
enabled and you later run it with authencation enabled, the database
will still run without authentication.

This causes confusion.

This proposal intends to change that in a way that:

- Is less confusing for the user
- Still makes it possible to make manual changes to the config files

## Proposed changes

We propose that the starter adds a hash to each generated config file.
This hash reflects the content of the config file, without any comments or
blank lines or spacing.

When the starter runs again and finds an existing config file, is re-calculates
a hash based on the current content of the config file and compares it with the hash
stored in the config file.

If these hashes are the same, the config file has not been modified by the user.
In this case, the starter will update the config file according to its current
commandline options, except for those settings that are immutable.

When the user tries to change an immutable setting, the starter will yield a warning.

If the hashes are different, the config file has been modified by the user.
In that case, the starter will not modify the config file, but yield warnings
about all settings in the config file that conflict with the current commandline
options of the starter.

## Immutable settings

The following settings are considered immutable:

- Storage engine

## Hash details

The hash of a config file is stored in a single line in the config file
that looks like this:

```text
# CONTENT-HASH: <hex encoded hash value>
```
