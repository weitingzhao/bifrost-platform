package releasefreeze

import "fmt"

// The finite model of design §6. Identities, names and content classes are the
// alphabet; the transition rules are the two layers (RBAC, then admission) plus
// the apiserver's own conflicts and UID preconditions.

const (
	idO uint8 = iota
	idP
	idT
	idG
	idW
	idCount
)

const (
	classCompliant uint8 = iota
	classOwnerRef
	classFinalizer
	classLabels
	classGenerateName
	classBadName
	classBadSpec
	classCount
)

const (
	howLive uint8 = iota
	howByO
	howByGC
	howByW
	howByG
)

const (
	oldFalse uint8 = iota
	oldTomb
	oldAbsent
)

const (
	uidMatch uint8 = iota
	uidStale
	uidAbsent
)

const (
	propPlain uint8 = iota
	propForeground
)

const (
	updSpec uint8 = iota
	updMeta
	updSub
)

const (
	gateProdAPI uint8 = iota
	gateProdWorkers
	gateSTG
	gateCheck
	gateFresh
	gateOld
	gateCount
)

const (
	faultBody uint8 = iota
	faultTransport
	faultCount
)

const (
	evCreate uint8 = iota
	evUpdate
	evDelete
	evDeleteCollection
	evSweep
	evRead
	evFault
	evTombstone
	evEnableCreate
)

var modelNames = [2]string{
	"freeze-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	"freeze-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
}

type gen struct {
	used    bool
	creator uint8
	class   uint8
	how     uint8
}

type nameState struct {
	gens    [2]gen
	n       uint8
	present bool
}

type world struct {
	names  [2]nameState
	old    uint8
	create bool
}

type rules struct {
	refuseOwnerRef        bool
	ownerOnlyDelete       bool
	createNeedsTombstone  bool
	sweepThroughAdmission bool
}

func correctRules() rules {
	return rules{
		refuseOwnerRef:        true, // rule: no owner reference
		ownerOnlyDelete:       true, // rule: delete username is the Owner
		createNeedsTombstone:  true, // rule: tombstone before create
		sweepThroughAdmission: true, // rule: sweep uses admission
	}
}

type event struct {
	kind    uint8
	who     uint8
	name    uint8
	class   uint8
	field   uint8
	uidMode uint8
	prop    uint8
	gate    uint8
	lin     uint8
	fault   uint8
}

func alphabet() []event {
	out := make([]event, 0, 182)
	for who := uint8(0); who < idCount; who++ {
		for name := uint8(0); name < 2; name++ {
			for class := uint8(0); class < classCount; class++ {
				out = append(out, event{kind: evCreate, who: who, name: name, class: class})
			}
			for field := uint8(0); field < 3; field++ {
				out = append(out, event{kind: evUpdate, who: who, name: name, field: field})
			}
			for uid := uint8(0); uid < 3; uid++ {
				for prop := uint8(0); prop < 2; prop++ {
					out = append(out, event{kind: evDelete, who: who, name: name, uidMode: uid, prop: prop})
				}
			}
		}
		out = append(out, event{kind: evDeleteCollection, who: who})
	}
	out = append(out, event{kind: evSweep})
	for gate := uint8(0); gate < gateCount; gate++ {
		for lin := uint8(0); lin < 2; lin++ {
			out = append(out, event{kind: evRead, gate: gate, lin: lin})
		}
	}
	for f := uint8(0); f < faultCount; f++ {
		out = append(out, event{kind: evFault, fault: f})
	}
	out = append(out, event{kind: evTombstone}, event{kind: evEnableCreate})
	return out
}

func eventsFor(all []event, w world) []event {
	out := make([]event, 0, len(all))
	for _, e := range all {
		if e.kind == evCreate && w.names[e.name].n >= 2 {
			continue
		}
		out = append(out, e)
	}
	return out
}

func rbacCreate(w world, who uint8) bool {
	switch who {
	case idO, idW:
		return true
	case idP:
		return w.create
	default:
		return false
	}
}

func rbacDelete(who uint8) bool { return who == idO || who == idG || who == idW }

func rbacDeleteCollection(who uint8) bool { return who == idO || who == idW }

func rbacUpdate(who uint8) bool { return who == idO || who == idG || who == idW }

// allowed is the expected table. Everything that is not listed here is refused
// and must leave the world unchanged. Sweep uses the same RBAC and admission as a delete.
func allowed(r rules, w world, e event) bool {
	switch e.kind {
	case evCreate:
		ns := w.names[e.name]
		if ns.n >= 2 || ns.present || !rbacCreate(w, e.who) {
			return false
		}
		switch e.class {
		case classCompliant:
			return true
		case classOwnerRef:
			return !r.refuseOwnerRef
		default:
			return false
		}
	case evUpdate:
		return false
	case evDelete:
		if !w.names[e.name].present || !rbacDelete(e.who) || e.uidMode == uidStale || e.prop != propPlain {
			return false
		}
		if r.ownerOnlyDelete && e.who != idO {
			return false
		}
		return e.uidMode == uidMatch || e.uidMode == uidAbsent
	case evDeleteCollection:
		if !rbacDeleteCollection(e.who) {
			return false
		}
		if r.ownerOnlyDelete && e.who != idO {
			return false
		}
		return true
	case evSweep:
		if !rbacDelete(idG) {
			return false
		}
		if r.sweepThroughAdmission && r.ownerOnlyDelete {
			return false // admission refuses the sweep
		}
		return true
	case evRead, evFault:
		return true
	case evTombstone:
		return w.old == oldFalse || w.old == oldTomb
	case evEnableCreate:
		if w.create {
			return true
		}
		if r.createNeedsTombstone && w.old != oldTomb {
			return false
		}
		return true
	default:
		return false
	}
}

func step(r rules, w world, e event) (world, bool) {
	if !allowed(r, w, e) {
		return w, true
	}
	return mutate(w, e), false
}

func mutate(w world, e event) world {
	switch e.kind {
	case evCreate:
		ns := &w.names[e.name]
		ns.gens[ns.n] = gen{used: true, creator: e.who, class: e.class, how: howLive}
		ns.n++
		ns.present = true
	case evDelete:
		if e.prop == propForeground {
			return w
		}
		ns := &w.names[e.name]
		ns.gens[ns.n-1].how = howOf(e.who)
		ns.present = false
	case evDeleteCollection:
		for i := range w.names {
			if w.names[i].present {
				w.names[i].gens[w.names[i].n-1].how = howOf(e.who)
				w.names[i].present = false
			}
		}
	case evSweep:
		for i := range w.names {
			ns := &w.names[i]
			if ns.present && ns.gens[ns.n-1].class == classOwnerRef {
				ns.gens[ns.n-1].how = howByGC
				ns.present = false
			}
		}
	case evTombstone:
		if w.old == oldFalse {
			w.old = oldTomb
		}
	case evEnableCreate:
		w.create = true
	}
	return w
}

func howOf(who uint8) uint8 {
	switch who {
	case idW:
		return howByW
	case idG:
		return howByG
	default:
		return howByO
	}
}

func anyPresent(w world) bool {
	return w.names[0].present || w.names[1].present
}

func allDeletedByO(w world) bool {
	for _, ns := range w.names {
		for i := uint8(0); i < ns.n; i++ {
			live := ns.present && i+1 == ns.n
			if live || ns.gens[i].how != howByO {
				return false
			}
		}
	}
	return true
}

func oldAllows(w world) bool { return w.old == oldFalse }

func listBody(w world) []byte {
	if !anyPresent(w) {
		return EmptyListBody()
	}
	whoName := []string{"O", "P", "T", "G", "W"}
	items := make([]byte, 0, 256)
	items = append(items, '[')
	first := true
	for i, ns := range w.names {
		if !ns.present {
			continue
		}
		if !first {
			items = append(items, ',')
		}
		first = false
		g := ns.gens[ns.n-1]
		items = append(items, []byte(fmt.Sprintf(
			`{"metadata":{"name":"%s","uid":"uid-%d-%d"},"spec":{"who":"%s","reason":"record","at":"2026-10-11T00:00:00Z"}}`,
			modelNames[i], i, ns.n, whoName[g.creator],
		))...)
	}
	items = append(items, ']')
	body := make([]byte, 0, len(items)+128)
	body = append(body, `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{},"items":`...)
	body = append(body, items...)
	body = append(body, '}')
	return body
}

func faultDecision(kind uint8) Decision {
	switch kind {
	case faultBody:
		return DecideHTTP(200, []byte(`{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1","resourceVersion":"2"},"items":[]}`))
	default:
		return DecideHTTP(500, EmptyListBody())
	}
}

func afterAdjacent(r rules, all []event, w world) world {
	for _, e := range eventsFor(all, w) {
		switch e.kind {
		case evRead, evFault, evSweep:
			continue
		}
		next, refused := step(r, w, e)
		if !refused && next != w {
			return next
		}
	}
	return w
}

func judgeSnapshot(snap world, gate uint8) string {
	if gate == gateOld {
		if snap.create && snap.old == oldFalse {
			return "S: old program reports clear after create is enabled"
		}
		return ""
	}
	d := DecideHTTP(200, listBody(snap))
	clear := d.Verdict == VerdictClear
	if !anyPresent(snap) && !clear {
		return "L: no record and the LIST succeeded, but the gate did not report clear"
	}
	if clear && !allDeletedByO(snap) {
		return "S: clear while a record was not deleted by O"
	}
	return ""
}

// check is the written assertion for one event at one world. A non-empty
// string is a violation of S, L, U, or the expected table.
func check(w, next, later world, e event, refused bool) string {
	if refused && next != w {
		return "refused event changed the state"
	}
	switch e.kind {
	case evCreate:
		if refused {
			return ""
		}
		ns := next.names[e.name]
		if !ns.present || ns.gens[ns.n-1].class != e.class || ns.gens[ns.n-1].creator != e.who {
			return "create did not record the class and the creator"
		}
	case evDelete:
		if refused {
			return ""
		}
		if e.prop == propForeground && !next.names[e.name].present {
			return "foreground delete removed the record from the list"
		}
		if e.prop == propPlain && next.names[e.name].present {
			return "plain delete left the record"
		}
	case evDeleteCollection:
		if !refused && anyPresent(next) {
			return "delete-collection left a record"
		}
	case evSweep:
		if !refused {
			for _, ns := range next.names {
				if ns.present && ns.gens[ns.n-1].class == classOwnerRef {
					return "sweep left an owner reference"
				}
			}
		}
	case evTombstone:
		if !refused && w.old == oldFalse && next.old != oldTomb {
			return "tombstone was not written"
		}
	case evEnableCreate:
		if !refused && !next.create {
			return "create was not enabled"
		}
	case evRead:
		snap := w
		if e.lin == 1 {
			snap = later
		}
		return judgeSnapshot(snap, e.gate)
	case evFault:
		if faultDecision(e.fault).Verdict == VerdictClear {
			return "U: a failed or incomplete LIST reported clear"
		}
	}
	return ""
}

type frame struct {
	w      world
	parent int
	via    event
}

func explore(r rules) (states, transitions int, violation string) {
	all := alphabet()
	root := world{}
	nodes := []frame{{w: root, parent: -1}}
	seen := map[world]int{root: 0}
	for i := 0; i < len(nodes); i++ {
		w := nodes[i].w
		later := afterAdjacent(r, all, w)
		for _, e := range eventsFor(all, w) {
			transitions++
			next, refused := step(r, w, e)
			if msg := check(w, next, later, e, refused); msg != "" {
				return len(seen), transitions, formatPath(pathOf(nodes, i, e)) + " :: " + msg
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = len(nodes)
			nodes = append(nodes, frame{w: next, parent: i, via: e})
		}
	}
	return len(nodes), transitions, ""
}

func pathOf(nodes []frame, idx int, last event) []event {
	var rev []event
	rev = append(rev, last)
	for idx > 0 {
		rev = append(rev, nodes[idx].via)
		idx = nodes[idx].parent
	}
	out := make([]event, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

func formatPath(path []event) string {
	if len(path) == 0 {
		return "(empty)"
	}
	s := eventString(path[0])
	for _, e := range path[1:] {
		s += " → " + eventString(e)
	}
	return s
}

func eventString(e event) string {
	who := []string{"O", "P", "T", "G", "W"}[e.who]
	name := []string{"n0", "n1"}[e.name]
	cls := []string{"compliant", "ownerRef", "finalizer", "labels", "generateName", "badName", "badSpec"}[e.class]
	switch e.kind {
	case evCreate:
		return "create " + who + " " + name + " " + cls
	case evUpdate:
		return "update " + who + " " + name + " " + []string{"spec", "metadata", "subresource"}[e.field]
	case evDelete:
		return "delete " + who + " " + name + " " + []string{"uid-match", "uid-stale", "uid-absent"}[e.uidMode] + " " + []string{"plain", "foreground"}[e.prop]
	case evDeleteCollection:
		return "deletecollection " + who
	case evSweep:
		return "sweep"
	case evRead:
		return "read " + []string{"prod-api", "prod-workers", "stg", "check", "fresh", "old"}[e.gate] + " " + []string{"before", "after"}[e.lin]
	case evFault:
		return "fault " + []string{"body", "transport"}[e.fault]
	case evTombstone:
		return "tombstone"
	case evEnableCreate:
		return "enable-create"
	default:
		return "unknown"
	}
}

func search(r rules, pred func(path []event, w world) bool) []event {
	all := alphabet()
	type item struct {
		w    world
		path []event
	}
	q := []item{{}}
	seen := map[world]struct{}{world{}: {}}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		for _, e := range eventsFor(all, cur.w) {
			next, _ := step(r, cur.w, e)
			path := append(append([]event{}, cur.path...), e)
			if pred(path, next) {
				return path
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			if len(seen) > 80000 {
				return nil
			}
			q = append(q, item{w: next, path: path})
		}
	}
	return nil
}
