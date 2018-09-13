package tmux_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dgnorton/tmux"
)

func TestSession_NewAndKill(t *testing.T) {
	t.Parallel()

	s, err := tmux.NewSession("TmuxTestNewAndKillSession")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Kill(); err != nil {
		t.Fatal(err)
	}
}

func TestNewAndKillWindow(t *testing.T) {
	t.Parallel()

	s, err := tmux.NewSession("TmuxTestNewAndKillWindow")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	names := []string{"w1", "w2", "w3"}

	for _, name := range names {
		w, err := s.NewWindow(name)
		if err != nil {
			t.Fatal(err)
		}

		if w.Name != name {
			t.Fatalf("exp: %s, got: %s", name, w.Name)
		}
	}

	windows, err := s.Windows()
	if err != nil {
		t.Fatal(err)
	}

	for i, w := range windows {
		if i > 1 {
			break
		}
		if err := w.Kill(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewAndKillPane(t *testing.T) {
	t.Parallel()

	s, err := tmux.NewSession("TmuxTestNewAndKillPane")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	names := []string{"w1", "w2", "w3"}

	// Create 3 windows.
	for _, name := range names {
		w, err := s.NewWindow(name)
		if err != nil {
			t.Fatal(err)
		}

		// Create 4 panes in each window.
		for i := 0; i < 3; i++ {
			p, err := w.Split(tmux.ActivePane, nil)
			if err != nil {
				t.Fatal(err)
			}

			if p.Index != i+1 {
				t.Fatalf("exp: %d, got: %d", i+1, p.Index)
			}
		}

		// Make sure the window has the expected number of panes.
		panes, err := w.Panes()
		if err != nil {
			t.Fatal(err)
		} else if len(panes) != 4 {
			t.Fatalf("exp: %d, got: %d", 4, len(panes))
		}

		// Kill the panes in this window.
		for _, p := range panes {
			if err := p.Kill(); err != nil {
				t.Fatal(err)
			}
		}

		// Make sure the window has no panes.
		if _, err = w.Panes(); err == nil {
			t.Fatalf(`window "%s" shouldn't exist after killing all of its panes`, w.Name)
		}
	}
}

func TestPane_SendKeys(t *testing.T) {
	t.Parallel()

	s, err := tmux.NewSession("TestSendKeys")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	windows, err := s.Windows()
	if err != nil {
		t.Fatal(err)
	}

	panes, err := windows[0].Panes()
	if err != nil {
		t.Fatal(err)
	}

	pane := panes[0]

	if _, err := pane.SendKeys(`echo "hello world"`, "Enter"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	result, err := pane.Capture(tmux.NewCapturePaneOptions())
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(result, "\n")
	if lines[2] != `hello world` {
		fmt.Printf("%v\n", lines)
		t.Fatalf(`exp: 'hello world', got: '%s'`, lines[2])
	}
}

func TestPane_Process(t *testing.T) {
	t.Parallel()

	s, err := tmux.NewSession("TestPane_Process")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	windows, err := s.Windows()
	if err != nil {
		t.Fatal(err)
	}

	panes, err := windows[0].Panes()
	if err != nil {
		t.Fatal(err)
	}

	pane := panes[0]

	proc, err := pane.StartProcess("vim")
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	processes, err := pane.Processes()
	if err != nil {
		t.Fatal(err)
	}

	if err := proc.Restart(); err != nil {
		t.Fatal(err)
	}

	if len(processes) != 2 {
		t.Fatalf("got: %d, exp: 2", len(processes))
	}

	if err := proc.Kill(); err != nil {
		t.Fatal(err)
	}
}
