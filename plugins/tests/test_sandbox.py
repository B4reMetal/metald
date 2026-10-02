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
from unittest.mock import patch

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


class CompiledLanguageTests(unittest.TestCase):
    """c and cpp are compiled in the sandbox so the bot can check code before claiming it works."""

    ORDINARY_C = '#include <stdio.h>\nint main(void){int a[3]={1,2,3};int *p=&a[1];printf("%d\\n",*p);return 0;}'

    def test_c_command_compiles_then_runs(self):
        cmd = sandbox.build_command("c")
        self.assertIn("cc -std=c11", cmd)
        self.assertIn("/tmp/main.c", cmd)
        self.assertIn("&& /tmp/main", cmd)

    def test_cpp_uses_cxx(self):
        self.assertIn("c++ -std=c++20", sandbox.build_command("cpp"))

    def test_python_runs_its_file(self):
        self.assertEqual(sandbox.build_command("python"), "python3 /tmp/main.py </dev/null")

    def test_code_travels_as_a_file_not_in_the_command(self):
        # A 90k-character emulator in the exec command was rejected by Fly as too large.
        big = self.ORDINARY_C + "/*" + "x" * 90000 + "*/"
        seen = {}

        def fake_create(origin, token, timeout_s, image=sandbox.DEFAULT_IMAGE, files=None):
            seen["files"] = files
            return "m1"

        def fake_exec(origin, token, sid, command, timeout_s):
            seen["command"] = command
            return {"output": "ok", "returncode": 0, "status": "ok"}

        with patch.object(sandbox, "resolve_gateway", return_value=("o", "t")), \
             patch.object(sandbox, "create_sandbox", side_effect=fake_create), \
             patch.object(sandbox, "run_exec", side_effect=fake_exec), \
             patch.object(sandbox, "terminate"), \
             patch.object(sandbox.safetyreview, "enabled", return_value=False):
            self.assertEqual(sandbox.run_code(big, "c"), "ok")
        self.assertEqual(seen["files"], {"/tmp/main.c": big})
        self.assertLess(len(seen["command"]), 200)

    def test_address_of_is_not_a_background_job(self):
        self.assertFalse(sandbox.spawns_process(self.ORDINARY_C, "c"))
        self.assertFalse(sandbox.uses_network(self.ORDINARY_C, "c"))
        self.assertFalse(sandbox.uses_dynamic_exec(self.ORDINARY_C, "c"))

    def test_c_network_refused(self):
        for code in ('#include <sys/socket.h>\nint main(){}',
                     '#include <netdb.h>\nint main(){getaddrinfo("x",0,0,0);}',
                     'int main(){connect(3,0,0);}'):
            self.assertTrue(sandbox.uses_network(code, "c"), code)

    def test_c_processes_refused(self):
        for code in ('int main(){fork();}', 'int main(){system("ls");}',
                     'int main(){execvp("sh",0);}', '#include <spawn.h>\nint main(){posix_spawn(0,0,0,0,0,0);}'):
            self.assertTrue(sandbox.spawns_process(code, "c"), code)

    def test_c_raw_syscalls_and_asm_refused(self):
        for code in ('int main(){syscall(41,2,1,0);}', 'int main(){__asm__("syscall");}',
                     '#include <dlfcn.h>\nint main(){dlopen("x",0);}'):
            self.assertTrue(sandbox.uses_dynamic_exec(code, "c"), code)

    def test_compiled_runs_use_the_compiler_image(self):
        seen = {}

        def fake_create(origin, token, timeout_s, image=sandbox.DEFAULT_IMAGE, files=None):
            seen["image"] = image
            raise sandbox.SandboxError("stop here")

        with patch.object(sandbox, "resolve_gateway", return_value=("o", "t")), \
             patch.object(sandbox, "create_sandbox", side_effect=fake_create), \
             patch.object(sandbox.safetyreview, "enabled", return_value=False):
            sandbox.run_code(self.ORDINARY_C, "c")
        self.assertEqual(seen["image"], sandbox.C_IMAGE)


class AmpersandTests(unittest.TestCase):
    """A goal round was refused for matrix exponentiation's "n & 1"; & is only a job in bash."""

    def test_python_bitwise_and_is_allowed(self):
        self.assertFalse(sandbox.spawns_process("def odd(n):\n    return n & 1\n", "python"))
        self.assertFalse(sandbox.spawns_process("flags = a & b", "python"))

    def test_bash_background_job_is_refused(self):
        self.assertTrue(sandbox.spawns_process("sleep 100 &", "bash"))

    def test_python_processes_still_refused(self):
        self.assertTrue(sandbox.spawns_process("import subprocess; subprocess.run(['ls'])", "python"))


class BashCompileTests(unittest.TestCase):
    """A goal round wrote its emulator with a bash heredoc and called cc; that must be steered to language c."""

    HEREDOC = "mkdir -p /tmp/c8 && cat > /tmp/c8/chip8.c <<'EOF'\n#include <stdio.h>\nint main(void){return 0 & 1;}\nEOF\ncc -Wall -o /tmp/c8/c8 /tmp/c8/chip8.c && /tmp/c8/c8"

    def test_compiling_in_bash_gets_the_c_hint(self):
        out = sandbox.run_code(self.HEREDOC, "bash")
        self.assertIn('language "c"', out)
        self.assertTrue(out.startswith("Error: refused - don't build c"))

    def test_bash_and_list_and_redirects_are_not_jobs(self):
        for code in ("mkdir -p x && echo ok", "echo hi >&2", "ls &> /dev/null", "a |& b"):
            self.assertFalse(sandbox.spawns_process(code, "bash"), code)

    def test_bash_background_job_still_refused(self):
        self.assertTrue(sandbox.spawns_process("sleep 100 &", "bash"))
        self.assertTrue(sandbox.spawns_process("sleep 1 & wait", "bash"))


class OutputClipTests(unittest.TestCase):
    """A run with many warnings lost its final "ALL PASS" line when only the start was kept."""

    def test_long_output_keeps_start_and_end(self):
        out = "warning\n" * 5000 + "ALL PASS"
        clipped = sandbox.clip_output(out, 3000)
        self.assertTrue(clipped.startswith("warning"))
        self.assertTrue(clipped.endswith("ALL PASS"))
        self.assertLess(len(clipped), 3200)

    def test_short_output_untouched(self):
        self.assertEqual(sandbox.clip_output("ok", 3000), "ok")
