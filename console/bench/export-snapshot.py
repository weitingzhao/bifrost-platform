#!/usr/bin/env python3
"""Export a real snapshot for the nerve map bench (console/bench/nerve.html).

Reads, never writes: the bus register in this repository (topics, their domain,
publisher and readers; the subscription rows and clients), the ownership
table's tiers, the seats' reported titles (ConfigMap cicd/lineage-thread-titles
with the read-only kubeconfig, matched to sessions by this repo's commit
trailers) and, through psql as agent_reader (read-only; psql reads its own
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

import yaml  # PyYAML: the bench is a dev tool on this machine

reg = yaml.safe_load(open(os.path.join(repo, 'api/internal/bus/registry/rocket.yaml')))
topics = []
for typ, ch in (reg.get('channels') or {}).items():
    xb = ch.get('x-bifrost') or {}
    topics.append({'type': typ, 'publisher': xb.get('publisher', ''), 'domain': xb.get('domain', ''),
                   'consumers': xb.get('consumers') or [], 'display_only': bool(xb.get('display_only'))})
subs = yaml.safe_load(open(os.path.join(repo, 'api/internal/bus/registry/subscriptions/rocket.yaml')))
# A row's target domain, as registry.Subscription.TargetDomain says (a wake reaches a seat, a notify the owner).
target = {'wake': 'coordination', 'notify': 'approvals'}
rows = [{'id': r['id'], 'on': r.get('on', r.get(True)),  # YAML 1.1 reads the key on: as true
         'deliver': r['deliver'], 'domain': target.get(r['deliver'], '')}
        for r in subs.get('subscriptions') or [] if r.get('by') == 'dispatcher']
clients = [{'name': c['name'], 'kind': c['kind'], 'domain': c.get('domain', '')} for c in subs.get('clients') or []]
own = json.load(open(os.path.join(repo, 'config/domain-ownership.json')))
tiers = [{'id': d['id'], 'tier': d.get('tier'), 'facility': d.get('facility')} for d in own['domains']]

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

# Names, from where the page gets them: each session's reported title (the
# lineage-thread-titles ConfigMap, read-only) by its transcript, matched to the
# session through the commits' Claude-Session and Claude-Transcript trailers.
names = {}
try:
    cm = json.loads(subprocess.run(['kubectl', '--kubeconfig', os.path.expanduser('~/.kube/bifrost-k3s.yaml'), '-n', 'cicd', 'get', 'cm',
                                    'lineage-thread-titles', '-o', 'json'], check=True, capture_output=True, text=True).stdout)['data']
    by_transcript = {k: v['title'] for k, v in json.loads(cm.get('transcripts.json', '{}')).items()}
    log = subprocess.run(['git', '-C', repo, 'log', '--all', '--since=30.days',
                          '--format=%(trailers:key=Claude-Session,valueonly,separator=)|%(trailers:key=Claude-Transcript,valueonly,separator=)'],
                         check=True, capture_output=True, text=True).stdout
    for line in log.splitlines():
        session, _, transcript = line.partition('|')
        session, transcript = session.strip(), transcript.strip()
        if session and transcript in by_transcript:
            names['thread:' + session] = by_transcript[transcript]
    for k, v in json.loads(cm.get('manual.json', '{}')).items():
        names['thread:' + k] = v['title'] if isinstance(v, dict) else v
except Exception as e:  # names are a nicety of the bench: without them it shows ids
    print(f"names not read: {e}", file=sys.stderr)

snap = {'topics': topics, 'events': events, 'letters': letters, 'seats': seats, 'names': names, 'rows': rows, 'clients': clients, 'tiers': tiers}
path = os.path.join(here, 'snapshot.local.json')
json.dump(snap, open(path, 'w'), indent=1)
print(f"{path}: {len(topics)} topics, {len(events)} events, {len(letters)} letter pairs, {len(seats)} seats, {len(names)} names", file=sys.stderr)
