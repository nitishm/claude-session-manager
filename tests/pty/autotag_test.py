import os, pty, fcntl, termios, struct, time, select, json, tempfile, pyte, shutil, stat

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
home = tempfile.mkdtemp()
os.makedirs(f'{home}/.claude/sessions')
os.makedirs(f'{home}/bin')
open(f'{home}/bin/claude', 'w').write('''#!/bin/sh
echo "$@" > %s/args.txt
echo '{"1":["Kargo","#Work Stuff","a-very-long-tag-name-exceeding"],"9":["bad"]}'
''' % home)
os.chmod(f'{home}/bin/claude', 0o755)
pd = f'{home}/.claude/projects/-p'
os.makedirs(pd)
L = [{"type": "user", "cwd": "/tmp", "timestamp": "2026-10-01T10:00:00Z",
      "message": {"role": "user", "content": "alpha thing please look at this carefully now"}},
     {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "ok working on it, this is a long enough reply"}]}}]
open(f'{pd}/00000000-aaaa-bbbb-cccc-dddddddddddd.jsonl', 'w').write('\n'.join(json.dumps(l, separators=(',', ':')) for l in L) + '\n')
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM='xterm-256color', PATH=f'{home}/bin:' + os.environ['PATH'])
    os.execv(BIN, [BIN])
W, H = 120, 20
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


rd(2.5)
os.write(fd, b'T'); rd(2.5)
for l in scr.display[2:5]:
    print(l.rstrip()[:75])
print(scr.display[-3].rstrip()[:110])
print('state:', open(f'{home}/.cache/ccs/state-go.json').read())
print('args:', open(f'{home}/args.txt').read()[:300])
os.kill(pid, 9)
shutil.rmtree(home)
