import os, pty, fcntl, termios, struct, time, select, json, tempfile, pyte, sys

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
REAL = len(sys.argv) > 1 and sys.argv[1] == 'real'
home = tempfile.mkdtemp()
fake = tempfile.mkdtemp()
pd = f'{home}/.claude/projects/-tmp-proj'
os.makedirs(pd)
os.makedirs(f'{home}/.claude/sessions')
titles = ['kubernetes ingress debugging session',
          'building a claude code session manager in go',
          'pulumi stack refactor work']
for i, t in enumerate(titles):
    sid = f'0000000{i}-aaaa-bbbb-cccc-dddddddddddd'
    L = [{"type": "user", "cwd": f"{home}/repo", "timestamp": "2026-10-0%dT10:00:00Z" % (i + 1),
          "message": {"role": "user", "content": t + " please look at this carefully now"}},
         {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "ok working on it, this is a long enough reply to keep"}]}}]
    open(f'{pd}/{sid}.jsonl', 'w').write('\n'.join(json.dumps(l, separators=(',', ':')) for l in L) + '\n')
    os.utime(f'{pd}/{sid}.jsonl', (time.time() - i * 100, time.time() - i * 100))
env_path = os.environ['PATH']
if not REAL:
    # index is newest first: #1=session0 k8s, #2=session1 manager, #3=session2 pulumi
    open(f'{fake}/claude', 'w').write(
        '#!/bin/sh\necho "$@" > %s/args.txt\necho \'Sure! [{"n":2,"why":"built ccs session manager"},{"n":9,"why":"bogus"},{"n":1,"why":"k8s only"},{"n":2,"why":"dup"}]\'\n' % fake)
    os.chmod(f'{fake}/claude', 0o755)
    env_path = fake + ':' + env_path
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM='xterm-256color', PATH=env_path)
    if REAL:
        os.environ['ANTHROPIC_API_KEY'] = 'bad'
        os.environ['HOME'] = os.path.expanduser('~')  # real login needs real HOME for creds
        # keep temp sessions via symlink-free approach: copy creds not possible, so skip temp HOME
    os.execv(BIN, [BIN])
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', 30, 140, 0, 0))
scr = pyte.Screen(140, 30)
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


def send(k, t=0.5):
    os.write(fd, k.encode())
    rd(t)


def show(n=8):
    print('\n'.join(l.rstrip() for l in scr.display[:n] + scr.display[-3:]))


rd(2.5)
send('a')
q = 'where I built a session manager' if not REAL else 'the session where I worked on building a claude code session manager'
for c in q:
    send(c, 0.03)
rd(0.3)
send('\r', 2.0 if not REAL else 25.0)
show(9)
if not REAL:
    args = open(f'{fake}/args.txt').read()
    print('--- prompt has index lines:', '#2 | ' in args and '#3 | ' in args, '| bogus n=9 dropped, dup dropped (see rows above)')
send('\x1b')
print('--- after Esc')
show(5)
os.kill(pid, 9)
