# Agent Feedback

- context: Operator ran tusker runs inspect with no arguments
- friction: Error says MISSING_ARG --id but the documented usage is positional: tusker runs inspect <task-id-or-record-id>.
- product-idea: Make the error hint match the positional usage.
- impact: Minor confusion; operator assumed wrong syntax.
- related: AGX-T-0001
