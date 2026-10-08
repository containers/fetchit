import importlib.util
from pathlib import Path
import subprocess
import os
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "scope", Path(__file__).resolve().parents[2] / "scripts/ci-change-scope.py"
)
scope = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scope)


class ChangeScopeTests(unittest.TestCase):
    def test_documentation(self):
        self.assertTrue(scope.documentation_only([
            "docs/methods.rst", "docs/conf.py", "README.md", ".readthedocs.yml"
        ]))

    def test_runtime_inputs_and_mixed_changes(self):
        for path in ["pkg/engine/kube.go", "go.mod", "go.sum", "Dockerfile",
                     "examples/readme.md", "examples/kube/pod.yaml", "scripts/build.sh",
                     ".github/workflows/docs.yml", ".github/actions/setup-podman/action.yml",
                     "tests/ci/test_change_scope.py", "unknown-file"]:
            with self.subTest(path=path):
                self.assertFalse(scope.documentation_only(["docs/index.rst", path]))
        self.assertFalse(scope.documentation_only([]))

    def test_manual_scheduled_and_missing_history_run(self):
        for event in ["schedule", "workflow_dispatch", "unknown"]:
            self.assertTrue(scope.runtime_required(event, "base", "head"))
        self.assertTrue(scope.runtime_required("push", "", "head"))
        with patch.object(scope.subprocess, "check_output", side_effect=subprocess.CalledProcessError(1, "git")):
            self.assertTrue(scope.runtime_required("push", "missing", "head"))

    def test_null_delimiters_and_renames(self):
        with patch.object(scope.subprocess, "check_output", return_value=b"docs/a\nb.rst\0README.md\0") as command:
            self.assertFalse(scope.runtime_required("push", "base", "head"))
            self.assertIn("--no-renames", command.call_args.args[0])
        # Moving a runtime file into docs must still exercise runtime checks.
        with patch.object(scope.subprocess, "check_output", return_value=b"pkg/old.go\0docs/old.go\0"):
            self.assertTrue(scope.runtime_required("push", "base", "head"))

    def test_real_git_diff_docs_mixed_and_code_to_docs_rename(self):
        with tempfile.TemporaryDirectory() as directory:
            previous = os.getcwd()
            try:
                os.chdir(directory)
                def git(*args):
                    return subprocess.check_output(["git", *args], stderr=subprocess.DEVNULL, text=True).strip()
                git("init")
                git("config", "user.name", "CI test")
                git("config", "user.email", "ci@example.invalid")
                Path("docs").mkdir()
                Path("docs/index.rst").write_text("before")
                Path("runtime.go").write_text("package engine")
                git("add", ".")
                git("commit", "-m", "base")
                base = git("rev-parse", "HEAD")
                Path("docs/index.rst").write_text("after")
                git("add", ".")
                git("commit", "-m", "docs")
                docs = git("rev-parse", "HEAD")
                self.assertFalse(scope.runtime_required("push", base, docs))
                self.assertFalse(scope.runtime_required("pull_request", base, docs))
                git("mv", "runtime.go", "docs/runtime.go")
                git("commit", "-m", "move runtime into docs")
                self.assertTrue(scope.runtime_required("push", docs, git("rev-parse", "HEAD")))
                self.assertTrue(scope.runtime_required("pull_request", base, git("rev-parse", "HEAD")))
            finally:
                os.chdir(previous)

    def test_pull_request_uses_merge_base(self):
        with patch.object(scope.subprocess, "check_output", side_effect=["ancestor\n", b"docs/index.rst\0"]) as command:
            self.assertFalse(scope.runtime_required("pull_request", "base", "head"))
            self.assertEqual(command.call_args_list[0].args[0], ["git", "merge-base", "base", "head"])
            self.assertEqual(command.call_args_list[1].args[0][-2:], ["ancestor", "head"])


if __name__ == "__main__":
    unittest.main()
