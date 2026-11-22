"""Load test for ferry's previews, as the 0.5.0 review asked: at once a
scanned PDF's thumbnail at 640, a wall of 50 JPEG thumbnails, a download
(and, with --heic, a 12+ MP HEIC at 640). Prints status counts, times and
ferry's memory. Test data and credentials are the test Samba's."""
import http.cookiejar, json, os, sys, threading, time, urllib.parse, urllib.request
from concurrent.futures import ThreadPoolExecutor

BASE = os.environ.get("BASE", "http://127.0.0.1:8197")
PID = int(sys.argv[1])
CGROUP = sys.argv[2]
HERE = os.path.dirname(os.path.abspath(__file__))  # the work directory run.sh fills
HEIC = [a for a in sys.argv[3:] if a.endswith(".heic")]

jar = http.cookiejar.CookieJar()
op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
H = {"X-Ferry-Request": "1"}
q = urllib.parse.quote


def req(method, path, data=None, headers=None):
    r = urllib.request.Request(BASE + path, data=data, method=method, headers={**H, **(headers or {})})
    try:
        with op.open(r, timeout=600) as res:
            n = 0
            while True:
                b = res.read(1 << 20)
                if not b:
                    break
                n += len(b)
            return res.status, n
    except urllib.error.HTTPError as e:
        return e.code, 0


def status(key):
    for l in open(f"/proc/{PID}/status"):
        if l.startswith(key + ":"):
            return int(l.split()[1]) // 1024
    return -1


req("POST", "/api/v1/login", json.dumps({"username": "alex", "password": "alex-test-password"}).encode(), {"Content-Type": "application/json"})
d = "Load-%d" % int(time.time())
req("POST", f"/api/v1/shares/alex/folders?path={q(d)}")
jpgs = sorted(os.listdir(os.path.join(HERE, "jpg")))
for f in jpgs:
    req("PUT", f"/api/v1/shares/alex/content?path={q(d + '/' + f)}", open(os.path.join(HERE, "jpg", f), "rb").read())
pdfs = []
for i in range(3):
    name = f"scan-{i}.pdf"
    req("PUT", f"/api/v1/shares/alex/content?path={q(d + '/' + name)}", open(os.path.join(HERE, "scan-12mp.pdf"), "rb").read())
    pdfs.append(name)
for h in HEIC:
    req("PUT", f"/api/v1/shares/alex/content?path={q(d + '/' + os.path.basename(h))}", open(h, "rb").read())
print(f"uploaded; ferry RSS {status('VmRSS')} MB, peak {status('VmHWM')} MB")

results = {}
lock = threading.Lock()


def record(kind, st, t):
    with lock:
        results.setdefault(kind, []).append((st, t))


def timed(kind, method, path):
    t = time.time()
    st, n = req(method, path)
    record(kind, st, time.time() - t)


samples = []
stop = threading.Event()


def sample():
    while not stop.is_set():
        try:
            samples.append(status("VmRSS"))
        except FileNotFoundError:
            return
        time.sleep(0.1)


threads = [threading.Thread(target=sample)]
threads.append(threading.Thread(target=lambda: timed("download big.bin", "GET", "/api/v1/shares/alex/content?path=big.bin")))
threads.append(threading.Thread(target=lambda: [timed("pdf 640", "GET", f"/api/v1/shares/alex/thumbnail?size=640&path={q(d + '/' + p)}") for _ in range(3) for p in pdfs]))
if HEIC:
    threads.append(threading.Thread(target=lambda: [timed("heic 640", "GET", f"/api/v1/shares/alex/thumbnail?size=640&path={q(d + '/' + os.path.basename(h))}") for _ in range(2) for h in HEIC]))
    threads.append(threading.Thread(target=lambda: [timed("heic 320", "GET", f"/api/v1/shares/alex/thumbnail?size=320&path={q(d + '/' + os.path.basename(h))}") for _ in range(2) for h in HEIC]))


def wall():
    with ThreadPoolExecutor(6) as ex:  # a browser's connections
        for _ in range(2):
            list(ex.map(lambda f: timed("jpeg 320", "GET", f"/api/v1/shares/alex/thumbnail?size=320&path={q(d + '/' + f)}"), jpgs))


threads.append(threading.Thread(target=wall))
start = time.time()
for t in threads:
    t.start()
for t in threads[1:]:
    t.join()
stop.set()
threads[0].join()
took = time.time() - start

for kind, rs in sorted(results.items()):
    codes = {}
    for st, _ in rs:
        codes[st] = codes.get(st, 0) + 1
    ts = sorted(t for _, t in rs)
    print(f"{kind:18} {len(rs):3} requests  codes {codes}  median {ts[len(ts) // 2]:.2f} s  max {ts[-1]:.2f} s")
peak = open(os.path.join(CGROUP, "memory.peak")).read().strip() if os.path.exists(os.path.join(CGROUP, "memory.peak")) else "?"
events = open(os.path.join(CGROUP, "memory.events")).read().replace("\n", " ")
print(f"took {took:.1f} s; ferry RSS max sampled {max(samples)} MB, VmHWM {status('VmHWM')} MB; cgroup memory.peak {int(peak) >> 20 if peak != '?' else '?'} MB; {events}")
req("DELETE", f"/api/v1/shares/alex/entries?recursive=true&path={q(d)}")
