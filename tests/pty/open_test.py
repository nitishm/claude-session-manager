import os, pty, fcntl, termios, struct, time, select, json, tempfile, pyte

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
home = tempfile.mkdtemp()
fake = tempfile.mkdtemp()
pd = f'{home}/.claude/projects/-tmp-proj'
os.makedirs(pd)
os.makedirs(f'{home}/.claude/sessions')
for i, t in enumerate(['kubernetes ingress debugging', 'building a session manager in go', 'pulumi stack refactor']):
    sid = f'0000000{i}-aaaa-bbbb-cccc-dddddddddddd'
    L = [{"type": "user", "cwd": "/tmp", "timestamp": "2026-10-0%dT10:00:00Z" % (i + 1),
          "message": {"role": "user", "content": t + " please look at this carefully now"}},
         {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "ok working on it, this is a long enough reply to keep"}]}}]
    open(f'{pd}/{sid}.jsonl', 'w').write('\n'.join(json.dumps(l, separators=(',', ':')) for l in L) + '\n')
    os.utime(f'{pd}/{sid}.jsonl', (time.time() - i * 100, time.time() - i * 100))
# fakes: never touch the real terminal apps or the clipboard
open(f'{fake}/claude', 'w').write('#!/bin/sh\necho "CLAUDE $@" >> %s/log.txt\n' % fake)
open(f'{fake}/osascript', 'w').write('#!/bin/sh\necho "OSA $@" >> %s/log.txt\n' % fake)
for f in ('claude', 'osascript'):
    os.chmod(f'{fake}/{f}', 0o755)


def session(keys, label):
    pid, fd = pty.fork()
    if pid == 0:
        os.environ.update(HOME=home, TERM='xterm-256color', TERM_PROGRAM='iTerm.app', PATH=fake + ':' + os.environ['PATH'])
        os.execv(BIN, [BIN])
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 160, 0, 0))
    scr = pyte.Screen(160, 30)
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

    rd(2.5)
    print(f'=== {label}')
    for k in keys:
        try:
            os.write(fd, k.encode())
        except OSError:
            break
        rd(0.6)
        bar = [l.strip() for l in scr.display if 'in:' in l or 'opened' in l or 'cancelled' in l]
        if bar:
            print('   bar:', bar[0][:150])
    try:
        os.kill(pid, 9)
    except OSError:
        pass
    time.sleep(0.2)


def log():
    p = f'{fake}/log.txt'
    out = open(p).read().strip() if os.path.exists(p) else '(none)'
    if os.path.exists(p):
        os.remove(p)
    return out


session(['\r', '\x1b'], 'Enter then Esc -> cancelled, nothing run')
print('   log:', log())
session(['f', 't'], 'f then t -> fork in new tab (iTerm path: create tab)')
print('   log:', log()[:400])
session(['\r', 'w'], 'Enter then w -> resume in new window')
print('   log:', log()[:400])
session(['f', 'h'], 'f then h -> fork in this terminal (exec claude)')
print('   log:', log())
session([' ', ' ', 'O', 'w'], 'mark 2, O then w -> 2 windows (iTerm: one osascript each)')
print('   log:', log().replace('\n', '\n         ')[:700])
session(['R'], 'R with no interrupted sessions')
