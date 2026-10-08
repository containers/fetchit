#!/usr/bin/env python3
"""Conservatively decide whether a GitHub event needs runtime/image jobs."""
import os
import subprocess


def documentation_only(paths):
    return bool(paths) and all(
        path.startswith("docs/")
        or path in {".readthedocs.yml", ".readthedocs.yaml"}
        or ("/" not in path and path.endswith(".md"))
        for path in paths
    )


def runtime_required(event, base, head):
    if event not in {"pull_request", "push"} or not base or not head:
        return True
    try:
        if event == "pull_request":
            base = subprocess.check_output(
                ["git", "merge-base", base, head], text=True
            ).strip()
        paths = subprocess.check_output(
            ["git", "diff", "--name-only", "--no-renames", "-z", base, head]
        ).decode("utf-8", errors="surrogateescape").split("\0")
    except subprocess.CalledProcessError:
        # Missing history or an unresolvable push baseline must not skip tests.
        return True
    return not documentation_only([path for path in paths if path])


if __name__ == "__main__":
    runtime = runtime_required(
        os.environ.get("CI_EVENT", ""),
        os.environ.get("CI_BASE", ""),
        os.environ.get("CI_HEAD", ""),
    )
    result = "true" if runtime else "false"
    print(f"Runtime/image checks required: {result}")
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write(f"runtime={result}\n")
