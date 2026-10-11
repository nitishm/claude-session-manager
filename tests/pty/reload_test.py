import os, pty, fcntl, termios, struct, time, select, json, tempfile, pyte

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
home = tempfile.mkdtemp()
pd = f'{home}/.claude/projects/-tmp-proj'
os.makedirs(pd)
os.makedirs(f'{home}/.claude/sessions')


def mk(i, title, extra_prompts=0):
    sid = f'0000000{i}-aaaa-bbbb-cccc-dddddddddddd'
    L = [{"type": "user", "cwd": "/tmp", "timestamp": "2026-10-0%dT10:00:00Z" % (i + 1),
          "message": {"role": "user", "content": title + " please look at this carefully now"}}]
    for k in range(extra_prompts):
        L.append({"type": "user", "cwd": "/tmp", "timestamp": "2026-10-0%dT10:0%d:00Z" % (i + 1, k + 1),
                  "message": {"role": "user", "content": f"follow up number {k} with enough words to count"}})
    L.append({"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "ok working on it, this is a long enough reply to keep"}]}})
    open(f'{pd}/{sid}.jsonl', 'w').write('\n'.join(json.dumps(l, separators=(',', ':')) for l in L) + '\n')


mk(0, 'first original session')
mk(1, 'second original session')
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM='xterm-256color')
    os.execv(BIN, [BIN])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', 28, 150, 0, 0))
scr = pyte.Screen(150, 28)
st = pyte.ByteStream(scr)


def rd(t):
    end = time.time() + t
    while time.time() < end:
        r, _, _ = select.select([fd], [], [], 0.1)
        if r:
            try:
                d = os.read(fd, 65536)
            except OSError:
                return
            if b']11;?' in d:
                os.write(fd, b'\x1b]11;rgb:0000/0000/0000\x1b\\')
            if b'[6n' in d:
                os.write(fd, b'\x1b[1;1R')
            st.feed(d)


def rows():
    return [l.rstrip()[:75] for l in scr.display[2:8] if l.strip()]


rd(2.5)
print('--- start:', scr.display[0].strip())
os.write(fd, b'j'); rd(0.5)  # put cursor on row 2 to check it stays on the same session
sel_before = [l for l in scr.display if l.startswith('▌')][0][:70]
print('selected before:', sel_before.strip())
# new fork-like session appears, and an existing one gains prompts
mk(2, 'brand new forked session')
mk(1, 'second original session', extra_prompts=3)
t0 = time.time()
seen = None
while time.time() - t0 < 45:
    rd(1.0)
    if any('brand new forked' in l for l in scr.display):
        seen = time.time() - t0
        break
print('new session appeared after: %s s' % (round(seen, 1) if seen else 'NEVER'))
print('\n'.join(rows()))
print(scr.display[0].strip())
sel_after = [l for l in scr.display if l.startswith('▌')][0][:70]
print('selected after: ', sel_after.strip())
os.kill(pid, 9)
