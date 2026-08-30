package matcher

import "testing"

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		e, p string
		w    bool
	}{
		{"order.created", "order.created", true},
		{"order.updated", "order.created", false},
		{"order.created", "order.*", true},
		{"order.created.v2", "order.*", true},
		{"order", "order.*", false},
		{"orders.created", "order.*", false},
		{"order.", "order.*", false},
		{"anything", "*", true},
		{"", "*", true},
		{"order.created", "*.created", false},
		{"Order.Created", "order.created", false},
		{"order.created", "", false},
		{"", "", true},
		{"payment.refund.failed", "payment.*", true},
		{"payment", "payment.*", false},
		{"push", "push", true},
		{"push", "pu*", false},
	} {
		if g := Match(c.e, c.p); g != c.w {
			t.Errorf("%q %q=%v", c.p, c.e, g)
		}
	}
	if !MatchAny("a.b", []string{"x", "a.*"}) || MatchAny("a", nil) {
		t.Fatal("any")
	}
}
