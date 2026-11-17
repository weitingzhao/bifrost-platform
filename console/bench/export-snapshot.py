#!/usr/bin/env python3
"""Export a real snapshot for the nerve map bench (console/bench/nerve.html).

Reads, never writes: the bus register in this repository (topics, their domain
and publisher) and, through psql as agent_reader (read-only; psql reads its own
~/.pgpass), the last day of bus events and of letters between seats. Writes
bench/snapshot.local.json, which is not committed: it holds session ids.

  python3 console/bench/export-snapshot.py [--host 192.168.10.73] [--port 30432] [--db bifrost_platform]
"""
import argparse, json, os, re, subprocess, sys

here = os.path.dirname(os.path.abspath(__file__))
repo = os.path.dirname(os.path.dirname(here))

ap = argparse.ArgumentParser()
ap.add_argument('--host', default='192.168.10.73')
ap.add_argument('--port', default='30432')
ap.add_argument('--db', default='bifrost_platform')
ap.add_argument('--psql', default='/opt/homebrew/opt/libpq/bin/psql')
a = ap.parse_args()

def q(sql):
    out = subprocess.run([a.psql, '-h', a.host, '-p', a.port, '-U', 'agent_reader', '-d', a.db, '-At', '-F', '\t', '-c', sql],
                         check=True, capture_output=True, text=True).stdout
    return [line.split('\t') for line in out.splitlines() if line]

topics, cur = [], None
for line in open(os.path.join(repo, 'api/internal/bus/registry/rocket.yaml')):
    m = re.match(r'^  ([a-z]+\.[a-z.]+):$', line)
    if m:
        cur = {'type': m.group(1), 'publisher': '', 'domain': ''}
        topics.append(cur)
    elif cur:
        m = re.match(r'^\s+(publisher|domain): (\S+)', line)
        if m:
            cur[m.group(1)] = m.group(2)

types = ','.join("'%s'" % t['type'] for t in topics)
events = [{'type': t, 'time': at} for t, at in q(
    f"select event_type, to_char(at at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.MS\"Z\"') from agentrt.event "
    f"where at > now() - interval '24 hours' and event_type in ({types}) order by at")]
letters = [{'from': f, 'to': t, 'count': int(n), 'unacked': int(u)} for f, t, n, u in q(
    "select from_actor_id, to_actor_id, count(*), count(*) filter (where acked_at is null) from agentrt.message "
    "where created_at > now() - interval '24 hours' and to_actor_id <> 'controller' group by 1, 2")]
seats = sorted({r[0] for r in q(
    "select distinct from_actor_id from agentrt.message where created_at > now() - interval '24 hours' and to_actor_id = 'controller'")}
    | {l['from'] for l in letters} | {l['to'] for l in letters})

snap = {'topics': topics, 'events': events, 'letters': letters, 'seats': seats}
path = os.path.join(here, 'snapshot.local.json')
json.dump(snap, open(path, 'w'), indent=1)
print(f"{path}: {len(topics)} topics, {len(events)} events, {len(letters)} letter pairs, {len(seats)} seats", file=sys.stderr)
