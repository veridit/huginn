#!/usr/bin/env python3
"""Real PTY acceptance test. Builds its own driver, needs no CLI binary.
Usage: python3 scripts/tui-pty.py [artifact-directory]
All task data is synthetic in a TemporaryDirectory, never a real project store.
"""
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time

REPO = Path(__file__).resolve().parents[1]
if not (REPO / "go.mod").exists():
    REPO = Path(__file__).resolve().parents[3]
ARTIFACTS = Path(sys.argv[1]) if len(sys.argv) > 1 else None
if ARTIFACTS:
    ARTIFACTS.mkdir(parents=True, exist_ok=True)

with tempfile.TemporaryDirectory(prefix="huginn-tui-", dir=os.environ.get("JCODE_SCRATCH_DIR")) as tmp:
    root = Path(tmp)
    binary = root / "driver"
    store = root / "tasks"
    store.mkdir()
    subprocess.run(["go", "build", "-o", str(binary), "./internal/tui/cmd/pty-driver"], cwd=REPO, check=True)
    env = dict(os.environ, HUGINN_TUI_TEST_ROOT=str(store), TERM="xterm-256color", COLUMNS="120", LINES="35")
    subprocess.run([str(binary), "seed"], env=env, check=True)
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 35, 120, 0, 0))
    before = termios.tcgetattr(slave)
    process = subprocess.Popen([str(binary)], stdin=slave, stdout=slave, stderr=slave, env=env, close_fds=True)
    raw = bytearray()
    checks = []

    def read_for(seconds=0.35):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            ready, _, _ = select.select([master], [], [], 0.03)
            if ready:
                try:
                    raw.extend(os.read(master, 65536))
                except OSError:
                    return

    def text():
        return re.sub(rb"\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Z0-9]|\x1b[=>]", b"", bytes(raw)).decode("utf-8", "replace")

    def send(value):
        os.write(master, value.encode())
        read_for()

    def check(name, condition):
        checks.append((name, bool(condition)))

    def dump(name):
        if ARTIFACTS:
            (ARTIFACTS / (name + ".txt")).write_text(text())
        print("SCREEN " + name)
        print(text()[-5000:])

    try:
        read_for(1.5)
        check("start renders slug and title", "alpha-task  Alpha task" in text())
        dump("list")
        send("j")
        check("navigate down", "> beta-task" in text())
        send("k\r")
        check("detail goal", "Ship alpha safely" in text())
        send(" ")
        alpha = next(store.glob("*.alpha-task.md"))
        check("toggle acceptance persisted", "- [x] first acceptance" in alpha.read_text())
        send("d")
        check("D17 done-blocked inline", "huginn check" in text() and "second acceptance" in text())
        check("blocked status unchanged", "status: todo" in alpha.read_text())
        dump("detail-blocked")
        send("\x1b")
        send("J")
        alpha = next(store.glob("*.alpha-task.md"))
        beta = next(store.glob("*.beta-task.md"))
        check("move rank persisted", alpha.name.split(".")[0] > beta.name.split(".")[0])
        send("b")
        check("board rendered", "Huginn · board" in text() and "dropped" in text())
        dump("board")
        send("q")
        process.wait(timeout=5)
        read_for()
        check("quit cleanly", process.returncode == 0)
        # BSD sets transient PENDIN when raw mode returns to canonical mode.
        # Exercise the next shell-style read before comparing all attributes.
        os.write(master, b"terminal-restored\n")
        os.set_blocking(slave, False)
        restored_input = b""
        deadline = time.monotonic() + 1
        while time.monotonic() < deadline and not restored_input:
            try:
                restored_input = os.read(slave, 1024)
            except BlockingIOError:
                time.sleep(0.01)
        check("canonical input works after exit", restored_input == b"terminal-restored\n")
        after = termios.tcgetattr(slave)
        check("terminal attributes restored", after == before)
        if after != before:
            print("TERMIOS before:", before)
            print("TERMIOS after: ", after)
        check("alternate screen restored", b"\x1b[?1049l" in raw)
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(master)
        os.close(slave)
        if ARTIFACTS:
            (ARTIFACTS / "session.raw").write_bytes(raw)
    for name, ok in checks:
        print(("PASS " if ok else "FAIL ") + name)
    sys.exit(0 if all(ok for _, ok in checks) else 1)
