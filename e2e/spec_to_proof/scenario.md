# Deterministic scenario

1. Initialize a temporary Git repository and Tusker vault.
2. Generate a V2 planning context from the local spec; import two tasks with a
   hard edge and non-overlapping owned paths.
3. Seed only the local runtime registration needed for direct interactive work.
4. Start the frontier task, fail it before mutation, then start a new attempt
   in the retained workspace and commit its declared material.
5. Submit, bind the canonical source snapshot, create an independent review
   session, and submit the packet’s exact snapshot values.
6. Run the command proof at review pass and close. Confirm the implementation
   does not leak to the base checkout.

The Go scenario invokes the production CLI parser/router. It never launches a
resident daemon, dispatches a runner, or calls an external provider.
