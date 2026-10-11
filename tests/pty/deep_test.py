import os, pty, fcntl, termios, struct, time, select, json, tempfile, pyte, shutil

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
home = tempfile.mkdtemp()
os.makedirs(f'{home}/.claude/sessions')
os.makedirs(f'{home}/here')


def mk(i, title, nprompts, reply, age_days=0):
    pd = f'{home}/.claude/projects/-p{i}'
    os.makedirs(pd)
    p = f'{pd}/0000000{i}-aaaa-bbbb-cccc-dddddddddddd.jsonl'
    L = []
    for k in range(nprompts):
        L.append({"type": "user", "cwd": f"{home}/here", "timestamp": "2026-10-0%dT10:0%d:00Z" % (i + 1, k),
                  "message": {"role": "user", "content": f"{title} prompt {k} with enough words to count properly"}})
    L.append({"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": reply}]}})
    open(p, 'w').write('\n'.join(json.dumps(l, separators=(',', ':')) for l in L) + '\n')
    if age_days:
        t = time.time() - age_days * 86400
        os.utime(p, (t, t))


mk(0, 'old tiny one', 1, 'short reply that is long enough to be kept in the list ok', 30)
mk(1, 'old tiny two', 2, 'another short reply that is long enough to be kept ok', 20)
mk(2, 'recent big', 4, 'The fix was to set the frobnicator quux flag before starting the daemon properly.')
mk(3, 'recent other', 3, 'nothing interesting in this reply at all, just filler words here')
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM='xterm-256color')
    os.execv(BIN, [BIN])
W, H = 120, 30
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', H, W, 0, 0))
scr = pyte.Screen(W, H)
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


def show(tag, n=8, extra=0):
    print('---', tag)
    for l in scr.display[:n]:
        print(l.rstrip()[:110])
    print(scr.display[-3].rstrip()[:110])


rd(2.5)
show('start')
os.write(fd, b'/'); rd(0.2); os.write(fd, b'frobnicator'); rd(0.5)
print('--- normal / search for reply-only word: rows =', scr.display[0].strip())
os.write(fd, b'\x1b'); rd(0.3); os.write(fd, b'\x1b'); rd(0.3)
os.write(fd, b'D'); rd(0.3); os.write(fd, b'frobnicator QUUX'); rd(0.2); os.write(fd, b'\r'); rd(1.5)
show('deep search', 10)
os.write(fd, b'\x1b'); rd(0.4)
os.write(fd, b'z'); rd(0.5)
show('stale filter', 8)
os.write(fd, b'W'); rd(0.5)
show('after W', 8)
os.write(fd, b'\x1b'); rd(0.4)
os.write(fd, b'?'); rd(0.5)
print('--- help popup lines with new keys:', [l.strip()[:60] for l in scr.display if any(k in l for k in ('deep search', 'stale', 'mark all shown'))])
os.kill(pid, 9)
shutil.rmtree(home)
