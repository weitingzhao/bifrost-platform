//bifrost:owner foundation

package store_test

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planwiring"
)

// minParallel is how many tests of the package run at once when the command
// does not say (-parallel): at least this many, whatever GOMAXPROCS is. CI
// gives the test step 2 CPUs, so -parallel defaults to 2 there; the tests
// mostly wait on PostgreSQL (a database each, locks, sleeps), not on the CPU,
// and 4 at a time took the package from 58 s to 41 s at GOMAXPROCS=2 here
// (TD-374).
const minParallel = 4

// parallelFloor is the -parallel TestMain sets, and whether it sets one: none
// when the command names it or GOMAXPROCS is already at least minParallel.
func parallelFloor(explicit bool, procs int) (value int, set bool) {
	if explicit || procs >= minParallel {
		return 0, false
	}
	return minParallel, true
}

// parallelAsked is what TestMain found before it set anything: whether the
// command named -parallel, and the value it left (review 322 B1: flag.Set
// makes a flag look set to flag.Visit, so a test cannot tell afterwards).
var parallelAsked struct {
	explicit bool
	want     string
}

func TestMain(m *testing.M) {
	flag.Parse()
	explicit := false
	flag.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "test.parallel" })
	parallelAsked.explicit = explicit
	parallelAsked.want = flag.Lookup("test.parallel").Value.String()
	if n, set := parallelFloor(explicit, runtime.GOMAXPROCS(0)); set {
		if err := flag.Set("test.parallel", fmt.Sprint(n)); err != nil {
			fmt.Fprintln(os.Stderr, "store tests: -parallel:", err)
			os.Exit(2)
		}
		parallelAsked.want = fmt.Sprint(n)
	}
	planwiring.Bind() // the plan code's ports are bound by the test, not by linking
	agentrttest.Warm(seededTemplate, seedTemplate)
	os.Exit(m.Run())
}

// TestMain's floor holds: without -parallel on the command line, at least
// minParallel tests run at once; with it, the command's number.
func TestParallelFloor(t *testing.T) {
	t.Parallel()
	got := flag.Lookup("test.parallel").Value.String()
	if got != parallelAsked.want {
		t.Fatalf("-parallel is %s; TestMain meant %s", got, parallelAsked.want)
	}
	if !parallelAsked.explicit && got != fmt.Sprint(max(runtime.GOMAXPROCS(0), minParallel)) {
		t.Fatalf("-parallel is %s at GOMAXPROCS %d without -parallel; want at least %d", got, runtime.GOMAXPROCS(0), minParallel)
	}
	for _, c := range []struct {
		explicit bool
		procs    int
		value    int
		set      bool
	}{{false, 1, minParallel, true}, {false, 2, minParallel, true}, {false, minParallel, 0, false}, {false, 10, 0, false}, {true, 2, 0, false}} {
		if v, set := parallelFloor(c.explicit, c.procs); v != c.value || set != c.set {
			t.Errorf("parallelFloor(%v, %d) = %d, %v; want %d, %v", c.explicit, c.procs, v, set, c.value, c.set)
		}
	}
}
