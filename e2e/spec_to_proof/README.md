# Spec-to-proof temporary project

`TestTrustFullJourney` is the deterministic offline counterpart to the fresh
agent fixture. It creates a temporary Git repository and vault, sends
`init`, `delivery import`, work lifecycle, review, and close commands through
the production CLI parser/router, and seeds only the runtime registry needed
to model an interactive project. It does not launch a daemon or an external
provider.
