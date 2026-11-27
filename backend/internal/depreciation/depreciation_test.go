package depreciation

import "testing"

func TestStraightLineFixture(t *testing.T) {
	v := Input{Cost: 12000000000, LifeMonths: 60, Method: StraightLine, Accumulated: 4800000000, ElapsedMonths: 24}
	r, e := Calculate(v)
	if e != nil || r.Charge != 200000000 || r.BookValue != 7000000000 {
		t.Fatalf("unexpected result %#v %v", r, e)
	}
}
func TestNeverBelowResidual(t *testing.T) {
	v := Input{Cost: 10000, Residual: 1000, Accumulated: 8900, LifeMonths: 12, ElapsedMonths: 10, Method: DoubleDeclining}
	r, e := Calculate(v)
	if e != nil || r.Charge != 100 || r.BookValue != 1000 {
		t.Fatalf("unexpected result %#v %v", r, e)
	}
}
func TestRejectsInvalidBasis(t *testing.T) {
	_, e := Calculate(Input{Cost: 100, Residual: 101, LifeMonths: 12, Method: StraightLine})
	if e == nil {
		t.Fatal("expected error")
	}
}
