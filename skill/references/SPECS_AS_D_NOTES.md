# Specs as D-notes

Deprecated guidance.

Use [CANON_LOCATIONS.md](CANON_LOCATIONS.md) instead.

The old rule "specs live as D-notes" was too absolute. Tusker now supports three canon patterns per epic:

- epic `## Design`
- canonical D-note
- repo file via `spec_source`

If you are creating a developer doc, declare intent explicitly:

```bash
tusker new-doc --epic <ACR> --title "<Spec title>" --audience developer --canon-for <ACR>
# or
tusker new-doc --epic <ACR> --title "<Companion doc>" --audience developer --companion-to <ACR-S-0001>
```
