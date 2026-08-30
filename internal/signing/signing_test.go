package signing

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"hookline/internal/domain"
)

func TestVectors(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	cases := []struct {
		s string
		b string
		t time.Time
		w string
	}{
		{"whsec_test_secret_1", `{"hello":"world"}`, ts, "v1=40c946df3c6d5ff57d5d5ac21da91fa8116451adb4674feb3efe1e535c6577cf"},
		{"whsec_test_secret_1", "{}", ts, "v1=3cc6d220bd6d001d0f1931d315cb1765435a39bb810cde4e05e23a2b6d71a945"},
		{"whsec_test_secret_1", "", ts, "v1=1255ce5be0b18d90cb33658a5bd4895f474de17cd78fe3fd952061be13ae7830"},
		{"whsec_test_secret_2", `{"hello":"world"}`, ts, "v1=bedbfdf6ab28c696d800ba48448f9d219823e158a61213d24bbcfa99c4b088e1"},
		{"whsec_test_secret_1", `{"hello":"world"}`, ts.Add(time.Second), "v1=4444a02c1b2edeb52d75631c0720fc6470e38456cba2de1265fb8b4b98bb9efd"},
		{"whsec_test_secret_1", `{"event":"push","repo":"hookline"}`, ts, "v1=2d33fdaff6b726f047cc53c7a2dfda894d277a5c5eb3e83cc19d6a54cc2eed97"},
	}
	for _, c := range cases {
		if g := Sign([]byte(c.b), c.s, c.t); g != c.w {
			t.Errorf("%s != %s", g, c.w)
		}
		if e := Verify([]byte(c.b), c.w, strconv.FormatInt(c.t.Unix(), 10), c.s, c.t, 5*time.Minute); e != nil {
			t.Fatal(e)
		}
	}
}

func TestRejects(t *testing.T) {
	now := time.Unix(1700000000, 0)
	sig := Sign([]byte("x"), "s", now)
	for _, e := range []error{Verify([]byte("y"), sig, "1700000000", "s", now, time.Minute), Verify([]byte("x"), sig, "bad", "s", now, time.Minute), Verify([]byte("x"), sig, "1700000000", "s", now.Add(time.Hour), time.Minute), Verify([]byte("x"), sig, "1700000000", "s", now.Add(-time.Hour), time.Minute), Verify([]byte("x"), "bad", "1700000000", "s", now, time.Minute), Verify([]byte("x"), "v1=", "1700000000", "s", now, time.Minute), Verify([]byte("x"), strings.Repeat("x", 10_000), "1700000000", "s", now, time.Minute), Verify([]byte("x"), sig, "1700000000", "s", now, -1)} {
		if e == nil {
			t.Fatal("accepted invalid")
		}
	}
	if !errors.Is(Verify([]byte("x"), sig, "1700000000", "s", now.Add(time.Hour), time.Minute), domain.ErrSignatureExpired) {
		t.Fatal("wrong expiry")
	}
	gh := "sha256=9958b54bbf3f6b5c55577dac92771098531947079093befb4342e22d792cb496"
	if e := VerifyGitHub([]byte(`{"zen":"Design for failure."}`), gh, "gh_test_secret"); e != nil {
		t.Fatal(e)
	}
	if VerifyGitHub(nil, "bad", "s") == nil || VerifyGitHub([]byte("x"), gh, "s") == nil {
		t.Fatal("github invalid accepted")
	}
}
