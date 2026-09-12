# Agent access profile UI fixture

These are routed Playwright fixture captures of Settings → Agents → Profiles using the production stylesheet and design tokens:

- `1280-add-profile.png` — desktop layout with the form, private-folder settings, and existing profiles.
- `390-add-profile.png` — narrow layout with the default profile form and progressive access details.
- `390-add-profile-200-percent.png` — narrow viewport at 200% zoom (top of the form).
- `390-add-profile-200-percent-actions.png` — the same zoom level with the form actions in view.
- `legacy-full-existing.png` — existing Full access profile before the in-draft upgrade.
- `legacy-full-project-access.png` — the same profile after choosing project access, before save.

The browser check also verifies that the form has no horizontal overflow at 200% zoom, narrow controls remain at least 44px high, and keyboard focus produces a visible ring. The fixture explicitly includes the imported source tree in Tailwind's scan; captures are therefore styled evidence rather than browser-default HTML.

This is source/fixture evidence only. It does not qualify a live installed agent or native access enforcement.
