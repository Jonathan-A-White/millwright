#!/usr/bin/env python3
"""Time ten searches over a generated card fixture, three ways.

  bench.py fts   cards.jsonl            SQLite FTS5 (candidate b), in this process
  bench.py bd    BEADS_DIR              bd's own query on an embedded fixture (candidate a)
  bench.py live  VAULT                  Dolt SQL over the live served database: SELECTs only (candidate a)
  bench.py view  cards.jsonl            what the phone would hold (candidate c): bytes and scan time model

Each prints a markdown table: ten queries, cold and warm, in milliseconds. Cold is the first
run after the files were evicted from the page cache (posix_fadvise DONTNEED); warm is the
median of the runs after it. Standard library only; the live mode runs `bd sql` with a SELECT
and nothing else.
"""
import json
import os
import sqlite3
import statistics
import subprocess
import sys
import time

QUERIES = [
    "harbor lane", "quarry road", "04101", "juniper street", "cobbler's lane",
    "lantern wharf", "address", "passport photo", "forwarding address", "zzqxv",
]
BD = os.environ.get("BD", "/home/mw/.local/bin/bd")


def evict(path):
    paths = []
    if os.path.isdir(path):
        for root, _, files in os.walk(path):
            paths += [os.path.join(root, f) for f in files]
    else:
        paths = [path]
    os.sync()
    for p in paths:
        fd = os.open(p, os.O_RDONLY)
        try:
            os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED)
        finally:
            os.close(fd)


def ms(seconds):
    return seconds * 1000


def table(rows):
    print("| query | hits | cold ms | warm ms (median of 7) |")
    print("| --- | --- | --- | --- |")
    for q, hits, cold, warm in rows:
        print(f"| `{q}` | {hits} | {cold:.1f} | {warm:.1f} |")
    print(f"| **median of ten** | | **{statistics.median(r[2] for r in rows):.1f}** | **{statistics.median(r[3] for r in rows):.1f}** |")
    print(f"| **slowest** | | **{max(r[2] for r in rows):.1f}** | **{max(r[3] for r in rows):.1f}** |")


def fts(cards_path, tokenizer="unicode61 remove_diacritics 2"):
    db = f"/tmp/todo-search-{os.path.basename(cards_path)}-{tokenizer.split()[0]}.db"
    if os.path.exists(db):
        os.remove(db)
    t0 = time.perf_counter()
    con = sqlite3.connect(db)
    con.execute("create table cards(id text primary key, status text, created text, closed text, title text)")
    con.execute(f"create virtual table doc using fts5(id unindexed, kind unindexed, body, tokenize='{tokenizer}')")
    ncards = nrows = 0
    for line in open(cards_path):
        c = json.loads(line)
        ncards += 1
        con.execute("insert into cards values(?,?,?,?,?)", (c["id"], c["status"], c["created_at"], c.get("closed_at"), c["title"]))
        # rowid = card number * 64 + slot: a changed card's rows are one rowid range, not a scan
        rows = [(ncards * 64, c["id"], "title", c["title"]), (ncards * 64 + 1, c["id"], "description", c["description"])]
        rows += [(ncards * 64 + 2 + j, c["id"], "comment", k["text"]) for j, k in enumerate(c.get("comments", []))]
        con.executemany("insert into doc(rowid, id, kind, body) values(?,?,?,?)", rows)
        nrows += len(rows)
    con.commit()
    build = time.perf_counter() - t0
    con.close()
    size = os.path.getsize(db)
    print(f"cards {ncards}, fts rows {nrows}, build {build:.2f} s, db {size / 1e6:.1f} MB ({tokenizer})\n")

    sql = ("select doc.id, doc.kind, snippet(doc, 2, '[', ']', '…', 12) from doc "
           "where doc match ? order by rank limit 20")
    rows = []
    for q in QUERIES:
        match = '"' + q.replace('"', '""') + '"'
        cold = []
        for _ in range(1):
            evict(db)
            t = time.perf_counter()
            c = sqlite3.connect(db)
            got = c.execute(sql, (match,)).fetchall()
            cold.append(time.perf_counter() - t)
            c.close()
        c = sqlite3.connect(db)
        warm = []
        for _ in range(7):
            t = time.perf_counter()
            c.execute(sql, (match,)).fetchall()
            warm.append(time.perf_counter() - t)
        c.close()
        rows.append((q, len(got), ms(cold[0]), ms(statistics.median(warm))))
    table(rows)
    c = sqlite3.connect(db)
    t = time.perf_counter()
    c.execute(sql, ('"harbo"*',)).fetchall()
    print(f"\nas-you-type prefix `harbo*`, warm, one run: {ms(time.perf_counter() - t):.1f} ms")
    # an incremental refresh: one changed card replaces its rows
    t = time.perf_counter()
    c.execute("delete from doc where rowid between 64 and 127")
    c.execute("insert into doc(rowid, id, kind, body) values(64, 'fx-00000','title','a changed card')")
    c.commit()
    print(f"refresh of one changed card: {ms(time.perf_counter() - t):.1f} ms")


def bd_cli(beads_dir):
    env = {"HOME": "/tmp/fxhome", "PATH": "/usr/bin:/bin", "BEADS_DIR": beads_dir}
    db = os.path.join(beads_dir, "embeddeddolt")

    def run(q):
        expr = " OR ".join(f'{f}="{q}"' for f in ("title", "description", "notes"))
        t = time.perf_counter()
        out = subprocess.run([BD, "query", expr, "--all", "--limit", "0", "--json"], env=env,
                             capture_output=True, text=True, check=True).stdout
        return time.perf_counter() - t, len(json.loads(out) if out.strip() else [])

    rows = []
    for q in QUERIES:
        evict(db)
        cold, hits = run(q)
        warm = statistics.median(run(q)[0] for _ in range(7))
        rows.append((q, hits, ms(cold), ms(warm)))
    table(rows)


def live(vault):
    def like(q):
        lit = "'%" + q.lower().replace("\\", "\\\\").replace("'", "''") + "%'"
        return (f"select id from issues where lower(concat_ws(' ', title, description, notes)) like {lit} "
                f"union select issue_id from comments where lower(text) like {lit}")

    def run(sql):
        t = time.perf_counter()
        out = subprocess.run([BD, "-C", vault, "sql", "--json", sql], capture_output=True, text=True, check=True).stdout
        return time.perf_counter() - t, len(json.loads(out) if out.strip() else [])

    base = statistics.median(run("select 1 as one")[0] for _ in range(7))
    print(f"baseline `select 1` through bd sql: {ms(base):.0f} ms median of 7\n")
    rows = []
    for q in QUERIES:
        first, hits = run(like(q))
        warm = statistics.median(run(like(q))[0] for _ in range(7))
        rows.append((q, hits, ms(first), ms(warm)))
    table(rows)


def view(cards_path):
    import gzip
    entries, cards = [], 0
    for line in open(cards_path):
        c = json.loads(line)
        cards += 1
        summary = " ".join(c["description"].split())[:280]
        entries.append({"id": c["id"], "title": c["title"], "type": "task", "status": c["status"], "priority": 2,
                        "labels": c["labels"], "created": c["created_at"], "updated": c["updated_at"],
                        "closed": c.get("closed_at", ""), "summary": summary, "todo_rank": c["metadata"]["todo_rank"]})
    raw = json.dumps({"v": 2, "beads": entries}, separators=(",", ":")).encode()
    print(f"cards {cards}: view JSON {len(raw) / 1e3:.0f} KB, gzip {len(gzip.compress(raw)) / 1e3:.0f} KB, "
          f"{len(raw) / cards:.0f} bytes a card")
    # the phone's scan: search.ts lower-cases and tests includes() for every word, per bead
    node = ("const b=JSON.parse(require('fs').readFileSync(process.argv[1])).beads;"
            "const qs=JSON.parse(process.argv[2]);const t=[];"
            "for(const q of qs){const w=q.toLowerCase().split(/\\s+/);const s=process.hrtime.bigint();"
            "let n=0;for(const x of b){const all=[x.id,x.title,x.summary,x.labels.join(' ')].join(' ').toLowerCase();"
            "if(w.every(k=>all.includes(k)))n++;}"
            "t.push([q,n,Number(process.hrtime.bigint()-s)/1e6]);}console.log(JSON.stringify(t));")
    open("/tmp/view.json", "wb").write(raw)
    out = subprocess.run(["node", "-e", node, "/tmp/view.json", json.dumps(QUERIES)], capture_output=True, text=True, check=True).stdout
    print("\n| query | hits in the view's summaries | scan ms (in memory, node) |\n| --- | --- | --- |")
    for q, n, t in json.loads(out):
        print(f"| `{q}` | {n} | {t:.2f} |")


if __name__ == "__main__":
    mode, arg = sys.argv[1], sys.argv[2]
    if mode == "fts":
        fts(arg, *(sys.argv[3:4]))
    elif mode == "bd":
        bd_cli(arg)
    elif mode == "live":
        live(arg)
    elif mode == "view":
        view(arg)
