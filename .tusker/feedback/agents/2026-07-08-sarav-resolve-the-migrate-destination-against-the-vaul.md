# Agent Feedback

- context: Ran 'tusker attachments migrate --write' from repo root during dogfood-prep vault hygiene to clear the deprecated Attachments/ dir.
- friction: It moved the 4 legacy review-packets to '.tusker/.tusker/scratch/<task>/legacy-attachments/' — the vault dir '.tusker' is double-prepended to the scratch destination, so files land one level too deep. The dry-run prints the correct-looking '.tusker/scratch/<task>/legacy-attachments/' path, hiding the bug until you inspect the tree. Had to hand-move them back to '.tusker/scratch/<task>/legacy-attachments/'.
- product-idea: Resolve the migrate destination against the vault root once (not vault-root + '.tusker/scratch'), and make dry-run print the exact absolute path it will write so preview matches reality. Add a test asserting no '.tusker/.tusker' or 'scratch/scratch' component in the destination.
- impact: Silent wrong-path move on a sanctioned cleanup command; validator still passes (only checks Attachments/ is gone), so the mislocation goes unnoticed and pollutes scratch.
- related: tusker attachments migrate (V7_ATTACHMENTS_FORBIDDEN cleanup path); dogfood-prep commit ab219d7
- dedupe-key: attachments-migrate-double-nest-scratch
