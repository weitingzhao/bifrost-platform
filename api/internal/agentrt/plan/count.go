package plan

// Step states as stored on a step node. Items, rounds and architecture rows
// have no stored state; it is computed here.
const (
	StatePlanned = "planned"
	StateActive  = "active"
	StateDone    = "done"
	StateStopped = "stopped"
	StateDropped = "dropped"
)

// Step is what counting needs to know about one step.
type Step struct {
	ID    string
	State string
	// Manual is true when the step became done through a manual atom.
	Manual bool
}

// Count is the derived progress of an item or a round.
type Count struct {
	Done    int    // steps done
	Total   int    // steps not dropped: the denominator
	Manual  int    // of Done, how many through a manual atom
	Stopped int    // of the not-done steps, how many are stopped
	State   string // planned, active, done or stopped
}

// CountSteps applies the one counting rule of design v5 §5.1.2: the
// denominator is every step that is not dropped; a stopped step is not done;
// a step done through a manual atom counts as done and is counted apart;
// adding a step to a done item makes it not done again, because nothing is
// stored. For a round, pass the steps of all its items.
func CountSteps(steps []Step) Count {
	var c Count
	started := false
	for _, s := range steps {
		switch s.State {
		case StateDropped:
			continue
		case StateDone:
			c.Done++
			if s.Manual {
				c.Manual++
			}
			started = true
		case StateStopped:
			c.Stopped++
			started = true
		case StateActive:
			started = true
		}
		c.Total++
	}
	switch {
	case c.Total == 0:
		c.State = StatePlanned
	case c.Done == c.Total:
		c.State = StateDone
	case c.Stopped == c.Total-c.Done:
		c.State = StateStopped
	case started:
		c.State = StateActive
	default:
		c.State = StatePlanned
	}
	return c
}
