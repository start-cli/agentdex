package tui

import (
	"bytes"
	"os"
	"testing"
)

func TestTableAlignsColumns(t *testing.T) {
	Configure("never", os.Stdout)

	var b bytes.Buffer
	tbl := NewTable("ID", "NAME")
	tbl.Append("a", "Alpha")
	tbl.Append("bb", "Beta")
	tbl.Render(&b)

	want := "ID  NAME\na   Alpha\nbb  Beta\n"
	if b.String() != want {
		t.Errorf("table render =\n%q\nwant\n%q", b.String(), want)
	}
}

func TestConfigureNeverDisablesColour(t *testing.T) {
	Configure("never", os.Stdout)
	if got := Header.Sprint("x"); got != "x" {
		t.Errorf("colour not disabled: %q", got)
	}
}

func TestConfigureAlwaysEnablesColour(t *testing.T) {
	Configure("always", os.Stdout)
	if got := Header.Sprint("x"); got == "x" {
		t.Error("colour not enabled under always")
	}
	Configure("never", os.Stdout) // restore for other tests
}
