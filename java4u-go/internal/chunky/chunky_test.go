package chunky

import "testing"

func TestSupportsVersion(t *testing.T) {
	for _, ok := range []string{"26.1", "26.3", "26.4-snapshot-7", "27.0"} {
		if !SupportsVersion(ok) {
			t.Errorf("%s should be supported", ok)
		}
	}
	for _, no := range []string{"1.21.4", "1.20.1", "25w14a", "rd-132211", "", "x26"} {
		if SupportsVersion(no) {
			t.Errorf("%s should not be supported", no)
		}
	}
}

func TestLibrariesArePinned(t *testing.T) {
	for _, l := range Libraries {
		if len(l.SHA1) != 40 || l.Size <= 0 || l.URL == "" {
			t.Errorf("library %s is not pinned", l.Path)
		}
	}
}
