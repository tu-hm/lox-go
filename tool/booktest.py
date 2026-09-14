#!/usr/bin/env python3
"""Run the Crafting Interpreters test suite against this interpreter.

A port of the book's tool/bin/test.dart, which is Dart and expects to invoke
jlox or clox from that repo's own build directory. This runs the same .lox
files and the same expectation syntax against our binary instead.

We are a jlox-equivalent through chapter 13, so we run the book's "jlox" suite:
everything under test/, minus the early-chapter directories that predate a
working interpreter and the limit tests that only apply to clox's bytecode. The
skip list below is copied from _defineTestSuites() in test.dart.

Expectations are comments in the .lox source:

    // expect: <stdout line>
    // Error at 'x': <message>        compile error on this line, exit 65
    // [line N] Error: <message>      compile error on line N
    // [java line N] Error: ...       java-suite only (that is us)
    // expect runtime error: <msg>    exit 70, plus a [line N] stack frame
    // nontest                        not a test at all, skip the file

--corpus is a directory with a test/ subdirectory in it. The vendored corpus is
at test/, so that is the repository root; tool/booktest.sh is the front door and
picks it for you.

Usage:
    tool/booktest.py --interpreter ./glox --corpus .
    tool/booktest.py -i ./glox -c . -f closure -- -parser=llk -k=2
"""

import argparse
import json
import os
import re
import subprocess
import sys

EXPECTED_OUTPUT = re.compile(r"// expect: ?(.*)")
EXPECTED_ERROR = re.compile(r"// (Error.*)")
ERROR_LINE = re.compile(r"// \[((java|c) )?line (\d+)\] (Error.*)")
EXPECTED_RUNTIME_ERROR = re.compile(r"// expect runtime error: (.+)")
SYNTAX_ERROR = re.compile(r"\[.*line (\d+)\] (Error.+)")
STACK_TRACE = re.compile(r"\[line (\d+)\]")
NONTEST = re.compile(r"// nontest")

# Our resolver reports unused locals, which is the chapter 11 challenge and not
# something jlox does. The harness treats any stderr line that is not a syntax
# error as an unexpected failure, so these are filtered out by default. Pass
# --strict-stderr to see the suite judge them.
WARNING_LINE = re.compile(r"^\[line \d+\] Warning\b")

# The book's "jlox" suite. "skip" entries are the book's own, not ours.
SUITE = {
    "test": "pass",
    # No interpreter yet in those chapters.
    "test/scanning": "skip",
    "test/expressions": "skip",
    # The JVM does not implement IEEE equality on boxed doubles, so the book
    # skips this for jlox. Go's float64 == is correct and we pass it; run it
    # with --run-skipped to see that.
    "test/number/nan_equality.lox": "skip",
    # Limits of clox's bytecode format. A tree-walker has none of them.
    "test/limit/loop_too_large.lox": "skip",
    "test/limit/no_reuse_constants.lox": "skip",
    "test/limit/too_many_constants.lox": "skip",
    "test/limit/too_many_locals.lox": "skip",
    "test/limit/too_many_upvalues.lox": "skip",
    # jlox leans on the JVM to notice runaway recursion. We inherit Go's, which
    # is a fatal unrecoverable stack overflow rather than a Lox runtime error.
    "test/limit/stack_overflow.lox": "skip",
}

# Tests we fail on purpose, because this interpreter implements a challenge the
# book's own jlox does not. Reported separately and not counted as failures —
# but a divergence that starts *passing* is an error, so a stale entry here
# cannot rot silently.
KNOWN_DIVERGENCES = {
    "test/field/get_on_class.lox":
        "class methods challenge (ch. 12): LoxClass is a propertyOwner, so "
        "Foo.bar is a class-property lookup rather than 'Only instances have "
        "properties.'",
    "test/field/set_on_class.lox":
        "static fields challenge (ch. 12): Foo.bar = v sets a field on the "
        "class rather than raising 'Only instances have fields.'",
}

LANGUAGE = "java"  # which of the book's "[java line N]" annotations apply to us


def state_for(path):
    """Longest-prefix match against SUITE, the way test.dart resolves state."""
    sub, state = "", None
    for part in path.split("/"):
        sub = sub + "/" + part if sub else part
        if sub in SUITE:
            state = SUITE[sub]
    return state


class Test:
    def __init__(self, path, corpus):
        self.path = path
        self.corpus = corpus
        self.expected_output = []
        self.expected_errors = set()
        self.expected_runtime_error = None
        self.runtime_error_line = 0
        self.expected_exit = 0
        self.expectations = 0
        self.failures = []

    def parse(self):
        with open(os.path.join(self.corpus, self.path), encoding="utf-8",
                  errors="replace") as f:
            lines = f.read().split("\n")

        for n, line in enumerate(lines, 1):
            if NONTEST.search(line):
                return "nontest"

            m = EXPECTED_OUTPUT.search(line)
            if m:
                self.expected_output.append((n, m.group(1)))
                self.expectations += 1
                continue

            m = EXPECTED_ERROR.search(line)
            if m:
                self.expected_errors.add("[%d] %s" % (n, m.group(1)))
                self.expected_exit = 65
                self.expectations += 1
                continue

            m = ERROR_LINE.search(line)
            if m:
                lang = m.group(2)
                if lang is None or lang == LANGUAGE:
                    self.expected_errors.add("[%s] %s" % (m.group(3), m.group(4)))
                    self.expected_exit = 65
                    self.expectations += 1
                continue

            m = EXPECTED_RUNTIME_ERROR.search(line)
            if m:
                self.runtime_error_line = n
                self.expected_runtime_error = m.group(1)
                self.expected_exit = 70
                self.expectations += 1

        if self.expected_errors and self.expected_runtime_error:
            return "malformed"
        return "ok"

    def fail(self, message, lines=None):
        self.failures.append(message)
        if lines:
            self.failures.extend(lines)

    def run(self, interpreter, extra_args, strict_stderr, timeout):
        argv = [interpreter] + extra_args + [self.path]
        try:
            proc = subprocess.run(argv, cwd=self.corpus, capture_output=True,
                                  text=True, timeout=timeout)
            out, err, code = proc.stdout, proc.stderr, proc.returncode
        except subprocess.TimeoutExpired:
            self.fail("Timed out after %ds." % timeout)
            return self.failures

        out_lines = out.split("\n")
        if out_lines and out_lines[-1] == "":
            out_lines.pop()
        err_lines = err.split("\n")
        if err_lines and err_lines[-1] == "":
            err_lines.pop()
        if not strict_stderr:
            err_lines = [l for l in err_lines if not WARNING_LINE.match(l)]

        if self.expected_runtime_error is not None:
            self.check_runtime_error(err_lines)
        else:
            self.check_compile_errors(err_lines)
        self.check_exit_code(code, err_lines)
        self.check_output(out_lines)
        return self.failures

    def check_runtime_error(self, err_lines):
        if len(err_lines) < 2:
            self.fail("Expected runtime error '%s' and got none."
                      % self.expected_runtime_error)
            return
        if err_lines[0] != self.expected_runtime_error:
            self.fail("Expected runtime error '%s' and got:"
                      % self.expected_runtime_error)
            self.fail(err_lines[0])

        match = None
        for line in err_lines[1:]:
            match = STACK_TRACE.search(line)
            if match:
                break
        if match is None:
            self.fail("Expected stack trace and got:", err_lines[1:])
        elif int(match.group(1)) != self.runtime_error_line:
            self.fail("Expected runtime error on line %d but was on line %s."
                      % (self.runtime_error_line, match.group(1)))

    def check_compile_errors(self, err_lines):
        found, unexpected = set(), 0
        for line in err_lines:
            m = SYNTAX_ERROR.search(line)
            if m:
                error = "[%s] %s" % (m.group(1), m.group(2))
                if error in self.expected_errors:
                    found.add(error)
                    continue
                if unexpected < 10:
                    self.fail("Unexpected error:")
                    self.fail(line)
                unexpected += 1
            elif line != "":
                if unexpected < 10:
                    self.fail("Unexpected output on stderr:")
                    self.fail(line)
                unexpected += 1
        if unexpected > 10:
            self.fail("(truncated %d more...)" % (unexpected - 10))
        for error in sorted(self.expected_errors - found):
            self.fail("Missing expected error: " + error)

    def check_exit_code(self, code, err_lines):
        if code == self.expected_exit:
            return
        if len(err_lines) > 10:
            err_lines = err_lines[:10] + ["(truncated...)"]
        self.fail("Expected return code %d and got %d. Stderr:"
                  % (self.expected_exit, code), err_lines)

    def check_output(self, out_lines):
        i = 0
        while i < len(out_lines):
            if i >= len(self.expected_output):
                self.fail("Got output '%s' when none was expected." % out_lines[i])
            else:
                line, expected = self.expected_output[i]
                if expected != out_lines[i]:
                    self.fail("Expected output '%s' on line %d and got '%s'."
                              % (expected, line, out_lines[i]))
            i += 1
        while i < len(self.expected_output):
            line, expected = self.expected_output[i]
            self.fail("Missing expected output '%s' on line %d." % (expected, line))
            i += 1


def collect(corpus, prefix):
    paths = []
    for dirpath, _, filenames in os.walk(os.path.join(corpus, "test")):
        for name in sorted(filenames):
            if not name.endswith(".lox"):
                continue
            path = os.path.relpath(os.path.join(dirpath, name), corpus)
            path = path.replace(os.sep, "/")
            if "benchmark" in path:  # timing scripts, not tests
                continue
            if prefix and not path.startswith(("test/" + prefix, prefix)):
                continue
            paths.append(path)
    return sorted(paths)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("-i", "--interpreter", required=True,
                    help="path to the interpreter binary")
    ap.add_argument("-c", "--corpus", required=True,
                    help="checkout of munificent/craftinginterpreters")
    ap.add_argument("-f", "--filter", default="",
                    help="only run tests under this path, e.g. 'closure'")
    ap.add_argument("--strict-stderr", action="store_true",
                    help="judge our unused-variable warnings as unexpected output")
    ap.add_argument("--run-skipped", action="store_true",
                    help="also run what the book's jlox suite skips")
    ap.add_argument("--timeout", type=int, default=30)
    ap.add_argument("--json", help="write full results to this file")
    ap.add_argument("args", nargs="*",
                    help="extra interpreter flags, after --")
    opts = ap.parse_args()

    interpreter = os.path.abspath(opts.interpreter)
    corpus = os.path.abspath(opts.corpus)
    if not os.path.isdir(os.path.join(corpus, "test")):
        sys.exit("no test/ directory under %s" % corpus)

    passed = skipped = 0
    expectations = 0
    failures, divergences, regressions = [], [], []

    for path in collect(corpus, opts.filter):
        state = state_for(path)
        if state is None:
            sys.exit("unknown test state for '%s' — add it to SUITE" % path)
        if state == "skip" and not opts.run_skipped:
            skipped += 1
            continue

        test = Test(path, corpus)
        parsed = test.parse()
        if parsed == "nontest":
            continue
        if parsed == "malformed":
            sys.exit("%s expects both a compile error and a runtime error" % path)

        result = test.run(interpreter, opts.args, opts.strict_stderr, opts.timeout)
        expectations += test.expectations

        if path in KNOWN_DIVERGENCES:
            if result:
                divergences.append((path, result))
            else:
                regressions.append(path)
            continue
        if result:
            failures.append((path, result))
        else:
            passed += 1

    for path, lines in failures:
        print("FAIL %s" % path)
        for line in lines:
            print("     " + line)
        print()

    if divergences:
        print("Known divergences (expected, not counted as failures):")
        for path, _ in divergences:
            print("  %s\n      %s" % (path, KNOWN_DIVERGENCES[path]))
        print()

    for path in regressions:
        print("STALE %s now passes — remove it from KNOWN_DIVERGENCES." % path)
    if regressions:
        print()

    print("%d passed, %d failed, %d known divergences, %d skipped "
          "(%d expectations)"
          % (passed, len(failures), len(divergences), skipped, expectations))

    if opts.json:
        with open(opts.json, "w") as f:
            json.dump({
                "passed": passed,
                "failed": [p for p, _ in failures],
                "divergences": [p for p, _ in divergences],
                "regressions": regressions,
                "skipped": skipped,
                "expectations": expectations,
            }, f, indent=1)

    return 1 if failures or regressions else 0


if __name__ == "__main__":
    sys.exit(main())
