import os, pty, fcntl, termios, struct, time, select, json, tempfile, pyte, shutil

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
home = tempfile.mkdtemp()
gone = f'{home}/gone-dir'
here = f'{home}/here'
os.makedirs(here)
os.makedirs(f'{home}/.claude/sessions')


def mk(i, title, cwd):
    pd = f'{home}/.claude/projects/-p{i}'
    os.makedirs(pd)
    sid = f'0000000{i}-aaaa-bbbb-cccc-dddddddddddd'
    L = [{"type": "user", "cwd": cwd, "timestamp": "2026-10-0%dT10:00:00Z" % (i + 1),
          "message": {"role": "user", "content": title + " please look at this carefully now"}}]
    for k in range(i):
        L.append({"type": "user", "cwd": cwd, "timestamp": "2026-10-0%dT10:0%d:00Z" % (i + 1, k + 1),
                  "message": {"role": "user", "content": f"follow up number {k} with enough words to count"}})
    L.append({"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "ok working on it, this is a long enough reply"}]}})
    open(f'{pd}/{sid}.jsonl', 'w').write('\n'.join(json.dumps(l, separators=(',', ':')) for l in L) + '\n')


mk(0, 'alpha kargo work', here)
mk(1, 'beta missing dir', gone)
mk(2, 'gamma other thing', here)
pid, fd = pty.fork()
if pid == 0:
    os.environ.update(HOME=home, TERM='xterm-256color')
    os.execv(BIN, [BIN])
W, H = 120, 26
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


def show(tag, n=8):
    print('---', tag)
    for l in scr.display[:n]:
        print(l.rstrip()[:75])
    print(scr.display[-3].rstrip()[:110])


def click(x, y):  # SGR mouse press+release, 1-based
    os.write(fd, f'\x1b[<0;{x+1};{y+1}M\x1b[<0;{x+1};{y+1}m'.encode())


rd(2.5)
def tag(txt):
    os.write(fd, b't'); rd(0.3); os.write(fd, txt.encode()); rd(0.2); os.write(fd, b'\r'); rd(0.5)
tag('zeta')
os.write(fd, b'j'); rd(0.3); tag('alpha')
show('tagged two', 7)
click(12, 2); rd(0.5); show('dir click 1', 7)
click(12, 2); rd(0.5); show('dir click 2 (reversed)', 7)
click(58, 2); rd(0.5); show('tags header click', 7)
os.write(fd, b'#'); rd(0.5); show('# filter', 7)
os.write(fd, b'#'); rd(0.5); show('# next', 7)
os.write(fd, b'#'); rd(0.5); show('# wraps to off', 7)
os.kill(pid, 9)
shutil.rmtree(home)
