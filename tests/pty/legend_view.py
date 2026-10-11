import os, pty, fcntl, termios, struct, time, select, pyte, sys

BIN = os.environ.get('CCS_BIN', os.path.expanduser('~/scripts/ccs/ccs-go'))
W, H = int(sys.argv[1]), int(sys.argv[2])
pid, fd = pty.fork()
if pid == 0:
    os.environ['TERM'] = 'xterm-256color'
    os.execv(BIN, [BIN])
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
os.write(fd, b'?')
rd(0.6)
print('\n'.join(l.rstrip() for l in scr.display))
os.write(fd, b'x')
rd(0.5)
print('--- after any key: popup gone =', not any('Keys & legend' in l for l in scr.display), '| header:', scr.display[0].strip())
os.kill(pid, 9)
