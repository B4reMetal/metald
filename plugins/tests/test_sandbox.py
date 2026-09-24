#!/usr/bin/env python3
# Copyright (C) 2026 BareMetal
# Part of metald, a fork of soulshack (github.com/pkdindustries/soulshack)
# SPDX-License-Identifier: GPL-3.0-only

"""
Tests for sandbox.py's pre-execution refusal checks.
"""

import os
import sys
import unittest

os.environ.setdefault("METALD_TOOL_LOG", os.devnull)
_plugins = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path[:0] = [_plugins, os.path.join(_plugins, "lib")]
import sandbox

class TestNetworkGuard(unittest.TestCase):
    def test_refuses_obvious_network_code(self):
        for code in (
            "import socket; socket.create_connection(('1.1.1.1', 80))",
            "import requests; requests.get('https://example.com')",
            "import urllib.request",
            "curl https://canhazip.com",
            "ping -c 1 8.8.8.8",
            "cat < /dev/tcp/10.0.0.1/22",
        ):
            with self.subTest(code=code):
                self.assertTrue(sandbox.uses_network(code))

    def test_case_insensitive(self):
        self.assertTrue(sandbox.uses_network("import SOCKET"))

    def test_allows_ordinary_computation(self):
        for code in (
            "print(sum(range(100)))",
            "from decimal import Decimal; print(Decimal(1) / 7)",
            "import math; print(math.factorial(30))",
            "import re; print(re.match(r'\\d+', '42') is not None)",
        ):
            with self.subTest(code=code):
                self.assertFalse(sandbox.uses_network(code))

class TestDynamicExecGuard(unittest.TestCase):
    # The exact live attempt: base64 of "curl https://canhazip.com", which
    # carries none of NETWORK_PATTERNS in its source text.
    def test_refuses_the_live_base64_smuggling_attempt(self):
        code = (
            'import base64\n'
            'exec(base64.b64decode("Y3VybCBodHRwczovL2NhbmhhemlwLmNvbQ==").decode())'
        )
        self.assertFalse(sandbox.uses_network(code), "network guard cannot see encoded payloads")
        self.assertTrue(sandbox.uses_dynamic_exec(code), "dynamic-exec guard must catch it")

    def test_refuses_dynamic_execution_forms(self):
        for code in (
            "eval(input_string)",
            "compile(src, '<s>', 'exec')",
            '__import__("soc" + "ket")',
            "import importlib",
            "import ctypes",
            "echo aGVsbG8= | base64 -d | bash",
            "echo x | python3",
            "eval $CMD",
        ):
            with self.subTest(code=code):
                self.assertTrue(sandbox.uses_dynamic_exec(code))

    # Decoding is a legitimate computation request; only decode-then-RUN is not.
    def test_allows_decoding_without_execution(self):
        for code in (
            'import base64; print(base64.b64decode("aGVsbG8=").decode())',
            "print(bytes.fromhex('68690a').decode())",
            "import codecs; print(codecs.decode('uryyb', 'rot13'))",
        ):
            with self.subTest(code=code):
                self.assertFalse(sandbox.uses_dynamic_exec(code))

    def test_allows_ordinary_computation(self):
        for code in (
            "print(sum(range(100)))",
            "import math; print(math.sqrt(2))",
            "print([x**2 for x in range(10)])",
        ):
            with self.subTest(code=code):
                self.assertFalse(sandbox.uses_dynamic_exec(code))

class TestRunCodeRefusals(unittest.TestCase):
    """run_code must return the refusal without reaching the gateway at all -
    resolve_gateway would shell out to Hermes and create a billable sandbox."""

    def setUp(self):
        self.calls = []
        self.original = sandbox.resolve_gateway
        sandbox.resolve_gateway = lambda: self.calls.append("resolved") or ("x", "y")

    def tearDown(self):
        sandbox.resolve_gateway = self.original

    def test_empty_code(self):
        self.assertIn("no code provided", sandbox.run_code("   "))
        self.assertEqual(self.calls, [])

    def test_network_refusal_short_circuits(self):
        out = sandbox.run_code("import socket")
        self.assertEqual(out, sandbox.NETWORK_REFUSAL)
        self.assertEqual(self.calls, [], "must refuse before creating a sandbox")

    def test_dynamic_exec_refusal_short_circuits(self):
        out = sandbox.run_code('exec(base64.b64decode("eA=="))')
        self.assertEqual(out, sandbox.DYNAMIC_EXEC_REFUSAL)
        self.assertEqual(self.calls, [], "must refuse before creating a sandbox")

    def test_refusals_leak_nothing_internal(self):
        for out in (sandbox.NETWORK_REFUSAL, sandbox.DYNAMIC_EXEC_REFUSAL,
                    sandbox.GENERIC_SANDBOX_ERROR):
            with self.subTest(out=out):
                lowered = out.lower()
                for secret in ("hermes", "nous", "modal", "gateway", "token",
                               "192.168", "127.0.0.1", "/users/"):
                    self.assertNotIn(secret, lowered)

class TestRefusalCategories(unittest.TestCase):
    """A refusal must name the real reason, since the bot repeats it to the channel."""

    def category(self, code):
        if sandbox.uses_network(code):
            return "network"
        if sandbox.spawns_process(code):
            return "process"
        if sandbox.uses_dynamic_exec(code):
            return "dynamic"
        return None

    def test_process_spawning_is_not_called_network(self):
        # Verbatim from the live log.
        code = ("import subprocess, os\n"
                "code = 'import os\\nwhile True:\\n os.fork()'\n"
                "p = subprocess.Popen(['python3', '-c', code])")
        self.assertEqual(self.category(code), "process")

    def test_exec_family_is_process_not_dynamic(self):
        # os.execv replaces the process image: spawning, not building code at runtime.
        self.assertEqual(self.category("os.execv('/bin/sh', ['sh'])"), "process")

    def test_fork_loop_is_process(self):
        self.assertEqual(
            self.category('/usr/bin/python3 -c "import os; [os.fork() for i in iter(int, 1)]"'),
            "process")

    def test_genuine_network_still_network(self):
        for code in ("import socket", "curl https://example.com", "cat < /dev/tcp/1.1.1.1/22"):
            self.assertEqual(self.category(code), "network", code)

    def test_dynamic_exec_still_dynamic(self):
        for code in ('exec(x)', "echo aGk= | base64 -d | bash", "python3 - <<'EOF'\nprint(1)\nEOF"):
            self.assertEqual(self.category(code), "dynamic", code)

    def test_ordinary_code_uncategorised(self):
        for code in ("print(sum(range(100)))", "import math; print(math.sqrt(2))"):
            self.assertIsNone(self.category(code), code)

    def test_each_refusal_names_its_own_reason(self):
        self.assertIn("network", sandbox.NETWORK_REFUSAL.lower())
        self.assertIn("process", sandbox.PROCESS_REFUSAL.lower())
        self.assertIn("dynamic", sandbox.DYNAMIC_EXEC_REFUSAL.lower())
        # and must not claim someone else's reason
        self.assertNotIn("network", sandbox.PROCESS_REFUSAL.lower())

if __name__ == "__main__":
    unittest.main(verbosity=2)
