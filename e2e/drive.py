#!/usr/bin/env python3
"""Drive the Oolong TUI on a pty with scripted keystrokes.

Usage: drive.py <capture-file> <cwd> <script...>

Script items are "delay:keys" pairs (keys may use python escapes like \\r,
\\x1b), sent in order once the app is up. Use @resize=ROWSxCOLS as the keys
to resize the PTY and deliver SIGWINCH. The binary comes from $OOLONG_BIN,
extra arguments from $OOLONG_ARGS; raw terminal output lands in the capture
file for the caller to assert on.

The app queries the terminal at startup (OSC 11 background, DSR cursor
position, kitty keyboard, DECRQM sync/grapheme modes); a driver that answers
nothing gets its keystrokes swallowed by the query readers. This one replies
like a dark-background xterm and sets a 100x30 winsize, without which Bubble
Tea sees 0x0. Stdlib only; POSIX only.
"""
import os, pty, select, struct, sys, termios, fcntl, time, signal

capture_path, workdir = sys.argv[1], sys.argv[2]
script = []
for item in sys.argv[3:]:
    delay, _, keys = item.partition(":")
    script.append((float(delay), keys.encode().decode("unicode_escape").encode()))

pid, fd = pty.fork()
if pid == 0:
    os.chdir(workdir)
    argv = [os.environ["OOLONG_BIN"]] + os.environ.get("OOLONG_ARGS", "").split()
    os.execv(argv[0], [a for a in argv if a])

def resize(rows, cols):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    os.kill(pid, signal.SIGWINCH)

# 100x30 window, or Bubble Tea sees 0x0.
resize(30, 100)

captured = b""
deadline = time.time() + 25
answered = set()

def answer_queries(data):
    """Reply to terminal capability queries like a dark-background xterm."""
    out = b""
    if b"\x1b]11;?" in data and "bg" not in answered:
        answered.add("bg")
        out += b"\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\"
    if b"\x1b[6n" in data:
        out += b"\x1b[1;1R"
    if b"\x1b[?u" in data and "kitty" not in answered:
        answered.add("kitty")
        out += b"\x1b[?0u"
    for mode in (b"2026", b"2027"):
        if b"\x1b[?" + mode + b"$p" in data and mode not in answered:
            answered.add(mode)
            out += b"\x1b[?" + mode + b";0$y"
    return out

# Let the app start and answer its queries before scripting keys.
next_key_at = time.time() + 1.5
idx = 0
exited = False
while time.time() < deadline:
    r, _, _ = select.select([fd], [], [], 0.05)
    if r:
        try:
            data = os.read(fd, 65536)
        except OSError:
            exited = True
            break
        if not data:
            exited = True
            break
        captured += data
        reply = answer_queries(data)
        if reply:
            os.write(fd, reply)
    if idx < len(script) and time.time() >= next_key_at:
        delay, keys = script[idx]
        if keys.startswith(b"@resize="):
            rows, cols = keys.removeprefix(b"@resize=").decode().split("x", 1)
            resize(int(rows), int(cols))
        else:
            os.write(fd, keys)
        idx += 1
        next_key_at = time.time() + delay
    if idx >= len(script) and time.time() >= next_key_at + 1.0:
        break

with open(capture_path, "wb") as f:
    f.write(captured)

if not exited:
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
os.waitpid(pid, 0)
print(f"captured {len(captured)} bytes, script items sent: {idx}/{len(script)}")
sys.exit(0 if idx == len(script) else 1)
