---
title: "System overview"
subject: overview
status: canonical
read_when: "Starting work or locating the current authority for a repository fact."
skip_when: "You already know the owning subsystem and need its detailed runbook."
---

# System overview

Tusker tracks work in a Git repository. A task is a contract. It states the
result, the checks, and the proof.

## Authority map

| Fact | Current authority |
| --- | --- |
| Command behavior | `cmd/tusker/` and `tusker capabilities --json` |
| Record fields and allowed values | `internal/v7schema/schema.go` |
| Close policy | `internal/v7policy/` and the close commands in `cmd/tusker/` |
| Repository tracker state | `.tusker/` |
| Machine runtime state | `cmd/tusker/runtime_store.go` |
| Local HTTP API | `cmd/tusker/serve_command.go` and `cmd/tusker/serve_*.go` |
| Web routes and screens | `internal/serve/ui/src/` |
| macOS shell | `apps/mac/TuskerBar/` |
| Agent operation rules | `skills/tusker/` |
| Project facts | `.tusker/knowledge/domains/project/` |
| Public system explanation | `docs/system/` |

Source and schemas win when a page is wrong. A runtime observation can show
what one process did. It does not change the source contract.

## Main flow

1. A person creates a task contract.
2. The CLI checks its fields and state revision.
3. An interactive worker or an enabled daemon claims the work.
4. The worker changes the repository.
5. The worker records proof for the acceptance rows.
6. A reviewer submits a typed result.
7. Landing and close check the bound revisions and proof.

Planning, review, import, and automation are separate actions. A plan does not
dispatch work. Project registration does not enable automation.

## Important boundaries

- `.tusker/` is repository truth. `daemon.db` is machine runtime truth.
- Serve is a guarded view and control surface. It is not a second tracker.
- TuskerBar uses or starts the local daemon. It does not store task state.
- Generated indexes and the embedded web `dist/` are build outputs.
- Names such as `tusker.task/v7` are file-format identifiers. They are not
  product generations.

## Read next

- Governing specs and decisions: `.tusker/specs/` (find a subject with `tusker docs find`)
- [Workflow audit and remaining repairs](../reports/spec-to-proof-audit.md)
- [Tasks and proof](tasks-and-proof.md)
- [Proof and closeout](proof-and-closeout.md)
- [Storage and runtime](storage-and-runtime.md)
- [CLI reference](cli.md)
- [Orchestration](orchestration.md)
- [Serve UI](serve-ui.md)
- [Platform support](platform-support.md)

## Documentation rule

Each page must name its code sources. Use short sentences. Use active voice.
Use one term for one idea. Do not copy plans, old task history, or runtime logs
into this document set.

System behavior pages live in `docs/system/`. Governing specs and decisions
live in `.tusker/specs/` and `.tusker/specs/decisions/`. Use `tusker docs find`
to route by subject before reading a full document; superseded subjects point
to their current replacement.

Run `tusker docs map --vault ./.tusker` after a system page changes.

## Code sources

- `cmd/tusker/cli.go`
- `cmd/tusker/runtime_store.go`
- `cmd/tusker/serve_command.go`
- `internal/v7schema/schema.go`
- `internal/v7policy/`
- `apps/mac/TuskerBar/Sources/TuskerBar/RuntimeSupervisor.swift`

<!-- tusker:docs-map:begin -->
```mermaid
graph TD
  n_2026_09_10_agent_coordination_grill["Agent coordination decisions — 10 September 2026"]
  n_2026_09_11_agent_access_grill["Agent access decisions — 11 September 2026"]
  n_agent_access["Agent access: simple profiles, honest permissions"]
  n_agent_coordination["Agent contacts, clarification and autonomous wave continuation"]
  n_cli["CLI reference"]
  n_completion_and_integrated_acceptance["Completion semantics and integrated acceptance"]
  n_decisions_2026_09_07_work_area_redesign_grill["Work-area redesign discussion record"]
  n_delivery_and_waves["Tasks and waves"]
  n_direct_wave_authoring["Direct task and wave authoring"]
  n_documents_experience["Documents experience and bounded CLI discovery"]
  n_execution_observability["Execution observability: names, lineage, and truthful multi-agent tracking"]
  n_execution_observability_grill["Decision log: execution observability and direct-agent identity"]
  n_execution_observability_system["Execution observability"]
  n_full_height_workspace["Full-height workspace with layered side navigation"]
  n_gates["Gates"]
  n_knowledge_and_feedback["Knowledge and feedback"]
  n_landing_and_completion["Landing and completion"]
  n_model_level_configuration["Manual model setup and three work levels"]
  n_orchestration["Orchestration"]
  n_overview["System overview"]
  n_planning_handoff_and_agent_entry["Planning capture, task handoff and progressive agent guidance"]
  n_platform_support["Platform support"]
  n_project_registration_and_visibility["Project registration and visibility"]
  n_project_registration_and_visibility_decisions["Project registration and visibility decisions"]
  n_proof_and_closeout["Proof and closeout"]
  n_real_work_lifecycle_cli["Make real task execution and closeout consistent across CLI and UI"]
  n_real_work_test_packets["Real-product testing — three implementation assignments"]
  n_real_work_test_repository["Build a resettable test repository for actual coding-agent work"]
  n_real_work_ui_acceptance["Verify the live task wave and Documents experience against the seeded project"]
  n_remaining_product_work["Remaining product work after the three real-work streams"]
  n_repeatable_work_testing["Repeatable work scenarios and CLI parity"]
  n_runner_boundary_decisions["Runner boundary decisions — 7 September 2026"]
  n_runner_execution_boundary["Use the operator's installed coding agents"]
  n_runners_and_acp["Runners and ACP"]
  n_serve_ui["Serve UI"]
  n_skills["Skills"]
  n_skills_and_documentation["External skills and clear everyday documentation"]
  n_spec_to_proof["From product intent to proven work"]
  n_storage_and_runtime["Storage and runtime"]
  n_tasks_and_proof["Tasks and proof"]
  n_tusker_trust_and_efficiency["Trustworthy Tusker with efficient agent workflows"]
  n_work_area_build_packets["Parallel build packets for the Tusker work experience"]
  n_work_area_redesign["Tusker work experience — implementation specification"]
  n_work_experience_map["Find the everyday Tusker work experience"]
  n_work_knowledge_and_retention["Project knowledge, model choices and lightweight evidence"]
  n_2026_09_10_agent_coordination_grill -->|decides for| n_agent_coordination
  n_2026_09_10_agent_coordination_grill -->|link| n_agent_coordination
  n_2026_09_10_agent_coordination_grill -->|part of| n_agent_coordination
  n_2026_09_11_agent_access_grill -->|decides for| n_agent_access
  n_2026_09_11_agent_access_grill -->|link| n_agent_access
  n_2026_09_11_agent_access_grill -->|part of| n_agent_access
  n_agent_access -->|link| n_model_level_configuration
  n_agent_access -->|link| n_runner_execution_boundary
  n_agent_access -->|part of| n_runners_and_acp
  n_agent_access -->|source| n_2026_09_11_agent_access_grill
  n_agent_access -->|source| n_model_level_configuration
  n_agent_access -->|source| n_runner_execution_boundary
  n_agent_access -->|updates| n_cli
  n_agent_access -->|updates| n_runners_and_acp
  n_agent_access -->|updates| n_serve_ui
  n_agent_coordination -->|part of| n_execution_observability
  n_agent_coordination -->|source| n_2026_09_10_agent_coordination_grill
  n_agent_coordination -->|source| n_completion_and_integrated_acceptance
  n_agent_coordination -->|source| n_execution_observability
  n_agent_coordination -->|source| n_planning_handoff_and_agent_entry
  n_agent_coordination -->|source| n_runner_execution_boundary
  n_agent_coordination -->|updates| n_cli
  n_agent_coordination -->|updates| n_delivery_and_waves
  n_agent_coordination -->|updates| n_execution_observability_system
  n_agent_coordination -->|updates| n_orchestration
  n_agent_coordination -->|updates| n_runners_and_acp
  n_agent_coordination -->|updates| n_serve_ui
  n_cli -->|part of| n_overview
  n_completion_and_integrated_acceptance -->|part of| n_planning_handoff_and_agent_entry
  n_completion_and_integrated_acceptance -->|source| n_planning_handoff_and_agent_entry
  n_completion_and_integrated_acceptance -->|source| n_real_work_test_packets
  n_completion_and_integrated_acceptance -->|source| n_remaining_product_work
  n_completion_and_integrated_acceptance -->|updates| n_landing_and_completion
  n_decisions_2026_09_07_work_area_redesign_grill -->|link| n_full_height_workspace
  n_decisions_2026_09_07_work_area_redesign_grill -->|link| n_model_level_configuration
  n_decisions_2026_09_07_work_area_redesign_grill -->|link| n_planning_handoff_and_agent_entry
  n_decisions_2026_09_07_work_area_redesign_grill -->|link| n_repeatable_work_testing
  n_decisions_2026_09_07_work_area_redesign_grill -->|link| n_work_area_redesign
  n_decisions_2026_09_07_work_area_redesign_grill -->|link| n_work_knowledge_and_retention
  n_decisions_2026_09_07_work_area_redesign_grill -->|part of| n_work_area_redesign
  n_decisions_2026_09_07_work_area_redesign_grill -->|source| n_work_area_redesign
  n_delivery_and_waves -->|part of| n_overview
  n_direct_wave_authoring -->|part of| n_planning_handoff_and_agent_entry
  n_direct_wave_authoring -->|source| n_agent_coordination
  n_direct_wave_authoring -->|source| n_planning_handoff_and_agent_entry
  n_direct_wave_authoring -->|updates| n_cli
  n_direct_wave_authoring -->|updates| n_delivery_and_waves
  n_documents_experience -->|link| n_tusker_trust_and_efficiency
  n_documents_experience -->|link| n_work_knowledge_and_retention
  n_documents_experience -->|part of| n_work_knowledge_and_retention
  n_documents_experience -->|source| n_knowledge_and_feedback
  n_documents_experience -->|source| n_tusker_trust_and_efficiency
  n_documents_experience -->|source| n_work_knowledge_and_retention
  n_execution_observability -->|part of| n_overview
  n_execution_observability -->|updates| n_cli
  n_execution_observability -->|updates| n_orchestration
  n_execution_observability -->|updates| n_overview
  n_execution_observability -->|updates| n_serve_ui
  n_execution_observability_grill -->|decides for| n_execution_observability
  n_execution_observability_grill -->|part of| n_execution_observability
  n_execution_observability_system -->|part of| n_overview
  n_full_height_workspace -->|part of| n_work_area_redesign
  n_full_height_workspace -->|source| n_documents_experience
  n_full_height_workspace -->|source| n_project_registration_and_visibility
  n_full_height_workspace -->|source| n_work_area_redesign
  n_full_height_workspace -->|updates| n_serve_ui
  n_gates -->|part of| n_overview
  n_knowledge_and_feedback -->|part of| n_overview
  n_landing_and_completion -->|part of| n_overview
  n_model_level_configuration -->|part of| n_work_area_redesign
  n_model_level_configuration -->|source| n_decisions_2026_09_07_work_area_redesign_grill
  n_model_level_configuration -->|source| n_runner_execution_boundary
  n_model_level_configuration -->|source| n_work_knowledge_and_retention
  n_model_level_configuration -->|updates| n_cli
  n_model_level_configuration -->|updates| n_runners_and_acp
  n_model_level_configuration -->|updates| n_serve_ui
  n_orchestration -->|part of| n_overview
  n_overview -->|link| n_cli
  n_overview -->|link| n_orchestration
  n_overview -->|link| n_platform_support
  n_overview -->|link| n_proof_and_closeout
  n_overview -->|link| n_serve_ui
  n_overview -->|link| n_storage_and_runtime
  n_overview -->|link| n_tasks_and_proof
  n_planning_handoff_and_agent_entry -->|link| n_agent_coordination
  n_planning_handoff_and_agent_entry -->|link| n_model_level_configuration
  n_planning_handoff_and_agent_entry -->|link| n_skills_and_documentation
  n_planning_handoff_and_agent_entry -->|link| n_spec_to_proof
  n_planning_handoff_and_agent_entry -->|part of| n_spec_to_proof
  n_planning_handoff_and_agent_entry -->|source| n_decisions_2026_09_07_work_area_redesign_grill
  n_planning_handoff_and_agent_entry -->|source| n_spec_to_proof
  n_planning_handoff_and_agent_entry -->|source| n_work_knowledge_and_retention
  n_platform_support -->|part of| n_overview
  n_project_registration_and_visibility -->|part of| n_overview
  n_project_registration_and_visibility -->|source| n_work_area_redesign
  n_project_registration_and_visibility_decisions -->|decides for| n_project_registration_and_visibility
  n_project_registration_and_visibility_decisions -->|part of| n_project_registration_and_visibility
  n_proof_and_closeout -->|part of| n_overview
  n_real_work_lifecycle_cli -->|part of| n_real_work_test_packets
  n_real_work_lifecycle_cli -->|source| n_repeatable_work_testing
  n_real_work_lifecycle_cli -->|source| n_work_knowledge_and_retention
  n_real_work_test_packets -->|link| n_real_work_lifecycle_cli
  n_real_work_test_packets -->|link| n_real_work_test_repository
  n_real_work_test_packets -->|link| n_real_work_ui_acceptance
  n_real_work_test_packets -->|part of| n_repeatable_work_testing
  n_real_work_test_packets -->|source| n_repeatable_work_testing
  n_real_work_test_packets -->|source| n_work_knowledge_and_retention
  n_real_work_test_repository -->|part of| n_real_work_test_packets
  n_real_work_test_repository -->|source| n_repeatable_work_testing
  n_real_work_test_repository -->|source| n_work_knowledge_and_retention
  n_real_work_ui_acceptance -->|part of| n_real_work_test_packets
  n_real_work_ui_acceptance -->|source| n_repeatable_work_testing
  n_real_work_ui_acceptance -->|source| n_work_knowledge_and_retention
  n_remaining_product_work -->|link| n_documents_experience
  n_remaining_product_work -->|link| n_real_work_lifecycle_cli
  n_remaining_product_work -->|link| n_real_work_test_repository
  n_remaining_product_work -->|link| n_real_work_ui_acceptance
  n_remaining_product_work -->|part of| n_planning_handoff_and_agent_entry
  n_remaining_product_work -->|source| n_planning_handoff_and_agent_entry
  n_remaining_product_work -->|source| n_real_work_test_packets
  n_remaining_product_work -->|source| n_work_knowledge_and_retention
  n_repeatable_work_testing -->|part of| n_work_area_redesign
  n_repeatable_work_testing -->|source| n_work_area_redesign
  n_repeatable_work_testing -->|source| n_work_knowledge_and_retention
  n_repeatable_work_testing -->|updates| n_cli
  n_repeatable_work_testing -->|updates| n_runners_and_acp
  n_repeatable_work_testing -->|updates| n_tasks_and_proof
  n_runner_boundary_decisions -->|decides for| n_runner_execution_boundary
  n_runner_boundary_decisions -->|part of| n_runner_execution_boundary
  n_runner_boundary_decisions -->|source| n_runner_execution_boundary
  n_runner_execution_boundary -->|part of| n_spec_to_proof
  n_runner_execution_boundary -->|source| n_runner_boundary_decisions
  n_runner_execution_boundary -->|updates| n_runners_and_acp
  n_runners_and_acp -->|link| n_runner_execution_boundary
  n_runners_and_acp -->|part of| n_overview
  n_serve_ui -->|part of| n_overview
  n_skills -->|part of| n_overview
  n_skills_and_documentation -->|part of| n_planning_handoff_and_agent_entry
  n_skills_and_documentation -->|source| n_planning_handoff_and_agent_entry
  n_spec_to_proof -->|link| n_2026_09_10_agent_coordination_grill
  n_spec_to_proof -->|link| n_agent_coordination
  n_spec_to_proof -->|link| n_delivery_and_waves
  n_spec_to_proof -->|link| n_model_level_configuration
  n_spec_to_proof -->|link| n_orchestration
  n_spec_to_proof -->|link| n_overview
  n_spec_to_proof -->|link| n_planning_handoff_and_agent_entry
  n_spec_to_proof -->|link| n_runner_execution_boundary
  n_spec_to_proof -->|link| n_tasks_and_proof
  n_spec_to_proof -->|part of| n_overview
  n_storage_and_runtime -->|part of| n_overview
  n_tasks_and_proof -->|link| n_proof_and_closeout
  n_tasks_and_proof -->|part of| n_overview
  n_tusker_trust_and_efficiency -->|link| n_spec_to_proof
  n_tusker_trust_and_efficiency -->|part of| n_overview
  n_work_area_build_packets -->|link| n_work_area_redesign
  n_work_area_build_packets -->|part of| n_work_area_redesign
  n_work_area_build_packets -->|source| n_work_area_redesign
  n_work_area_redesign -->|link| n_full_height_workspace
  n_work_area_redesign -->|link| n_project_registration_and_visibility
  n_work_area_redesign -->|link| n_work_area_build_packets
  n_work_area_redesign -->|part of| n_overview
  n_work_area_redesign -->|source| n_decisions_2026_09_07_work_area_redesign_grill
  n_work_area_redesign -->|source| n_work_area_build_packets
  n_work_area_redesign -->|updates| n_serve_ui
  n_work_experience_map -->|link| n_decisions_2026_09_07_work_area_redesign_grill
  n_work_experience_map -->|link| n_repeatable_work_testing
  n_work_experience_map -->|link| n_work_area_build_packets
  n_work_experience_map -->|link| n_work_area_redesign
  n_work_experience_map -->|link| n_work_knowledge_and_retention
  n_work_experience_map -->|part of| n_work_area_redesign
  n_work_experience_map -->|source| n_work_area_redesign
  n_work_knowledge_and_retention -->|link| n_repeatable_work_testing
  n_work_knowledge_and_retention -->|part of| n_work_area_redesign
  n_work_knowledge_and_retention -->|source| n_decisions_2026_09_07_work_area_redesign_grill
  n_work_knowledge_and_retention -->|source| n_work_area_redesign
```
<!-- tusker:docs-map:end -->
