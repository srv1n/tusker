# Tusker Skill Package

This package teaches coding agents how to operate Tusker in any repository. It
conforms to the Agent Skills package layout at `skills/tusker/` and uses
progressive disclosure: discovery loads only `name` and `description`,
activation loads the bounded `SKILL.md`, and execution reads only the directly
routed reference or asset needed for the current task.
