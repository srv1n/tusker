import { expect, test } from "bun:test";
import { parseWikilink } from "../src/features/knowledge/wikilink";

test("preserved multiline wikilinks normalize for lookup and retain raw markdown", () => {
  const raw = "[[../meta-harness/00 Meta-Harness\n Overview|meta harness]]";
  expect(parseWikilink(raw)).toEqual({
    raw,
    ref: "../meta-harness/00 Meta-Harness Overview",
    label: "meta harness",
  });
});
