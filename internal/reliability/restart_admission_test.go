package reliability

import (
	"errors"
	"testing"
)

func TestRestartAdmissionRetainsEveryOwnerAndIdempotentRelease(t *testing.T) {
	var gate RestartAdmissionGate
	one, err := gate.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	two, err := gate.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	one()
	one()
	if finish, ready := gate.BeginRestart(nil); ready {
		finish(false)
		t.Fatal("one owner hid another after duplicate release")
	}
	two()
	finish, ready := gate.BeginRestart(nil)
	if !ready {
		t.Fatal("completed owners retained admission")
	}
	finish(true)
	finish(false)
	if _, err := gate.Acquire(); !errors.Is(err, ErrRestartPending) {
		t.Fatalf("accepted seal reopened: %v", err)
	}
}
