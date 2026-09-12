#!/usr/bin/env python3
"""Black-box mailbox checks: installed CLI, fresh repo/state, no model or daemon."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tusker", default="tusker")
    parser.add_argument("--output", type=Path, help="Write JSON command receipts and results")
    args = parser.parse_args()
    binary = shutil.which(args.tusker)
    if not binary:
        parser.error("Tusker executable not found")
    root = Path(tempfile.mkdtemp(prefix="tusker-coordination-acceptance-")).resolve()
    source_binary = str(Path(binary).resolve())
    shutil.copy2(source_binary, root / "tusker")
    binary = str(root / "tusker")
    repo = root / "repo"
    repo.mkdir()
    env = dict(os.environ, TUSKER_STATE_ROOT=str(root / "state"))
    calls, results = [], []

    def call(*argv):
        process = subprocess.run(
            [binary, *argv], cwd=repo, env=env, capture_output=True,
            text=True, timeout=30,
        )
        calls.append(dict(argv=list(argv), exit=process.returncode,
                          stdout=process.stdout, stderr=process.stderr))
        try:
            payload = json.loads(process.stdout)
        except json.JSONDecodeError:
            payload = {}
        return process, payload

    def ok(*argv):
        process, payload = call(*argv)
        assert process.returncode == 0, process.stderr or process.stdout
        assert payload.get("ok", True) is not False, payload
        return payload or process.stdout

    def rejected(*argv):
        process, payload = call(*argv)
        assert process.returncode != 0 or payload.get("ok") is False, (
            "Expected explicit rejection, received " + process.stdout
        )

    def check(name, fn):
        try:
            fn()
            result = dict(name=name, status="PASS")
        except (AssertionError, KeyError, TypeError, subprocess.TimeoutExpired) as exc:
            result = dict(name=name, status="FAIL", detail=str(exc))
        results.append(result)
        print(result["status"] + " " + name, flush=True)

    ok("init", "--yes")
    registration = ok("projects", "add", "--repo", str(repo),
                      "--vault", str(repo / ".tusker"), "--json")
    project = registration["project"]["project_id"]
    assert registration["project"]["enabled"] is False
    ok("new", "epic", "--acronym", "ACT", "--title", "Coordination acceptance")
    tasks = []
    for number in range(1, 6):
        flags = []
        if number == 4:
            flags = ["--architect", "task:" + tasks[0], "--origin", "task:" + tasks[0],
                     "--peers", "schema=task:" + tasks[1]]
        created = ok("new", "task", "--epic", "ACT", "--status", "backlog",
                     "--title", "Acceptance participant " + str(number), *flags)
        tasks.append(re.search(r"ACT-T-[0-9]{4}", created).group())
    architect, peer, _, worker, _ = tasks
    body = "Choose red or blue.\nKeep this literal: $(not-a-command), 'quotes', café."
    base = ["--project", project, "--json"]
    ask = ["message", "ask", *base, "--task", worker, "--contact", "architect",
           "--sender", "task:" + worker, "--key", "worker-question", "--body", body, "--yield"]
    state = {}

    def contacts():
        # Public inspection flag: these disposable tasks intentionally stay held.
        packet = ok("packet", worker, "--for", "agent", "--force")
        text = packet if isinstance(packet, str) else json.dumps(packet)
        state["packet"] = text
        for expected in ("architect", "origin", "schema", architect, peer):
            assert expected.casefold() in text.casefold(), "Packet omits " + expected

    def packet_example():
        example = re.search(r"- Ask: `([^`]+)`", state["packet"]).group(1)
        replacements = {"<address>": "task:" + architect,
                        "<stable-key>": "packet-example", "<question>": "Packet example"}
        argv = [replacements.get(word, word) for word in shlex.split(example)[1:]]
        message = ok(*argv)["message"]
        assert (message["projectId"], message["recipient"]) == (
            project, {"kind": "task", "id": architect}
        ), "Packet command routed to " + json.dumps(
            {"projectId": message["projectId"], "recipient": message["recipient"]}
        )

    def question():
        saved = ok(*ask)
        message = saved["message"]
        state["question"] = message["id"]
        assert message["body"] == body
        assert message["recipient"] == {"kind": "task", "id": architect}
        assert message["replyRequired"] and message["yieldSender"]
        assert message["state"] == "queued" and message["transportState"] == "pending"

    def duplicate():
        again = ok(*ask)
        assert again["duplicate"] is True
        assert again["message"]["id"] == state["question"]

    def conflicting_key():
        changed = ask.copy()
        changed[changed.index("--body") + 1] = "Changed request with the same key"
        rejected(*changed)

    def reopen():
        # Every CLI call is a fresh process; no database access or test-only hooks.
        saved = ok("message", "show", *base, "--id", state["question"])["message"]
        assert saved["body"] == body and saved["id"] == state["question"]

    def reply():
        response = ok("message", "reply", *base, "--sender", "task:" + architect,
                      "--recipient", worker, "--recipient-kind", "task",
                      "--key", "architect-answer", "--reply-to", state["question"],
                      "--body", "Use blue.")
        state["answer"] = response["message"]["id"]
        messages = ok("message", "list", *base)["messages"]
        assert {state["question"], state["answer"]}.issubset({m["id"] for m in messages})
        assert response["message"]["replyTo"] == state["question"]

    def wrong_reply_target():
        rejected("message", "reply", *base, "--sender", "task:" + architect,
                 "--recipient", peer, "--recipient-kind", "task",
                 "--key", "wrong-target", "--reply-to", state["question"], "--body", "Wrong task.")

    def peer_question():
        message = ok("message", "ask", *base, "--task", worker, "--contact", "peer:schema",
                     "--sender", "task:" + worker, "--key", "peer-question",
                     "--body", "Which contract version should I use?")["message"]
        assert message["recipient"] == {"kind": "task", "id": peer}

    def payload_limit():
        rejected("message", "send", *base, "--sender", "task:" + worker,
                 "--recipient", architect, "--recipient-kind", "task",
                 "--key", "oversize", "--body", "x" * 32769)

    def wrong_project():
        rejected("message", "show", "--project", "unrelated-project", "--json",
                 "--id", state["question"])

    check("Task packet retains architect, origin and named peer", contacts)
    if "packet" in state:
        check("Packet's own ask example reaches the canonical recipient", packet_example)
    check("Question preserves body and waits without claiming delivery", question)
    if "question" in state:
        check("Identical request returns the same message", duplicate)
        check("Changed payload with reused key is rejected", conflicting_key)
        check("Question survives a new CLI process", reopen)
        check("Answer is durable and correlated to its question", reply)
        check("Reply addressed to another task is refused", wrong_reply_target)
        check("Another project cannot read the question", wrong_project)
    else:
        results.append(dict(name="Dependent question checks", status="BLOCKED"))
    check("Named peer question reaches the recorded peer", peer_question)
    check("Payload over 32 KiB is refused", payload_limit)
    report = dict(
        schema="tusker.coordination-acceptance/v1", binary=binary, source_binary=source_binary,
        binary_sha256=hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
        test_root=str(root), project_id=project, task_ids=tasks,
        boundary="Public CLI/mailbox only. No model, daemon, SQL or internal Go helper. "
                 "Does not prove agent wakeup, task yield/resume, or subsequent-wave execution.",
        results=results, calls=calls,
    )
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + "\n")
    print("Fixture retained: " + str(root))
    return int(any(result["status"] != "PASS" for result in results))


if __name__ == "__main__":
    raise SystemExit(main())
