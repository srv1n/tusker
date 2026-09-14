import { expect, test } from "bun:test";
import { projectIconChoice, projectInitials } from "../src/features/workbench/navigation/projectIcons";

test("project initials use meaningful name segments", () => {
  expect(projectInitials("rzn-browser")).toBe("RB");
  expect(projectInitials("song_rendition lab")).toBe("SR");
  expect(projectInitials("Tusker")).toBe("TU");
});

test("manual icon choices resolve from the supported set", () => {
  expect(projectIconChoice("audio")?.label).toBe("Audio");
  expect(projectIconChoice("auto")).toBeUndefined();
});
