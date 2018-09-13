package tmux

import (
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/process"
)

const (
	// ActivePane can be passed to any method or function that takes a pane ID
	// string.
	ActivePane = ""
)

// Sessions is a list of sessions.
type Sessions []*Session

// FindByName searches the list of sessions and returns a pointer to the one
// matching name.
func (a Sessions) FindByName(name string) *Session {
	for _, s := range a {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// ListSessions returns a list of sessions currently running.
func ListSessions() (Sessions, error) {
	result, err := Run("list-sessions", "-F", "'#{session_name}'")
	if err != nil {
		return nil, err
	}

	names := strings.Split(result, "\n")

	sessions := make([]*Session, 0, len(names))
	for _, name := range names {
		name = strings.Trim(name, "'")
		sessions = append(sessions, &Session{
			Name: name,
		})
	}

	return sessions, nil
}

// FindSessionFunc returns one or more sessions matching the fn predicate. If
// fn returns true for match, then the session will be returned. If fn returns
// true for cont, FindSessionFunc will continue searching for other matches.
func FindSessionFunc(fn func(*Session) (match bool, cont bool)) (Sessions, error) {
	ss, err := ListSessions()
	if err != nil {
		return nil, err
	}

	sessions := []*Session{}
	for _, s := range ss {
		match, cont := fn(s)
		if match {
			sessions = append(sessions, s)
		}
		if !cont {
			break
		}
	}

	return sessions, nil
}

// FindSession finds a session by name.
func FindSession(name string) (*Session, error) {
	ss, err := FindSessionFunc(func(s *Session) (match bool, cont bool) {
		return s.Name == name, s.Name != name
	})

	if err != nil {
		return nil, err
	}

	if len(ss) == 0 {
		return nil, nil
	}

	return ss[0], nil
}

// Session is an active session.
type Session struct {
	Name    string
	Verbose bool
}

// NewSession starts a new session with the given name.
func NewSession(name string) (*Session, error) {
	// See if session with that name already exists.
	if s, _ := FindSession(name); s != nil {
		return s, fmt.Errorf(`session named "%s" already exists`, name)
	}

	// Create the new session.
	_, err := Run("new-session", "-d", "-s", name)
	if err != nil {
		return nil, err
	}

	// Find the new session and return it.
	s, err := FindSession(name)
	if err != nil {
		return nil, err
	}

	if s == nil {
		return s, fmt.Errorf(`failed to create session "%s"`, name)
	}

	return s, nil
}

// Target returns the target name for the session. This is the value that would
// normally be passed to the -t <target> command line argument when running
// tmux commands.
func (s *Session) Target() string {
	return s.Name
}

// Kill kills the session.
func (s *Session) Kill() error {
	_, err := run(s, "kill-session")
	return err
}

// PanePIDs returns a list of process PIDs started by the session.
func (s *Session) PanePIDs() ([]int, error) {
	result, err := run(s, "list-panes", "-F", "'#{pane_pid}'")
	if err != nil {
		return nil, err
	}

	strs := strings.Split(result, "\n")

	return toInts(strs)
}

// Windows returns a list of windows in a session.
func (s *Session) Windows() ([]*Window, error) {
	result, err := run(s, "list-windows", "-F", "'#{window_id} #I #{window_active} #W'")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(result, "\n")

	windows := make([]*Window, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		w, err := parseWindow(line)
		if err != nil {
			return nil, err
		}
		w.Session = s

		windows = append(windows, w)
	}

	return windows, nil
}

// NewWindow creates a new window in the session.
func (s *Session) NewWindow(name string) (*Window, error) {
	// See if a window with this name already exists in the session.
	if w, _ := s.Window(name); w != nil {
		return nil, fmt.Errorf(`window "%s" already exists in session "%s"`, name, s.Name)
	}

	// Create the new window.
	_, err := run(s, "new-window", "-n", name)
	if err != nil {
		return nil, err
	}

	// Find and return the newly created window.
	return s.Window(name)
}

// KillWindow kills a window.
func (s *Session) KillWindow(name string) error {
	// Find the window to be killed.
	w, err := s.Window(name)
	if err != nil {
		return err
	}

	// Kill it.
	return w.Kill()
}

// Window returns the requested window, if it exists in the session.
func (s *Session) Window(name string) (*Window, error) {
	ws, err := s.Windows()
	if err != nil {
		return nil, err
	}

	for _, w := range ws {
		if w.Name == name {
			return w, nil
		}
	}

	return nil, fmt.Errorf(`window "%s" not found in session "%s"`, name, s.Name)
}

// Window represents a window in a session.
type Window struct {
	ID      string
	Index   int
	Active  bool
	Name    string
	Session *Session
}

// Target returns the target name for the window. This is the value that would
// normally be passed to the -t <target> command line argument when running
// tmux commands.
func (w *Window) Target() string {
	return fmt.Sprintf("%s:%s", w.Session.Target(), w.Name)
}

// Kill kills the window.
func (w *Window) Kill() error {
	_, err := run(w, "kill-window")
	return err
}

// Panes returns a slice of pointers to Pane. Each pane is a child of the window.
func (w *Window) Panes() ([]*Pane, error) {
	result, err := run(w, "list-panes", "-F", "'#D #P #T #{pane_active} #{pane_pid}'")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(result, "\n")

	panes := make([]*Pane, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		p, err := parsePane(line)
		if err != nil {
			return nil, err
		}
		p.Window = w

		panes = append(panes, p)
	}

	return panes, nil
}

// Pane finds the pane associated with the given ID.
func (w *Window) Pane(id string) (*Pane, error) {
	panes, err := w.Panes()
	if err != nil {
		return nil, err
	}

	for _, p := range panes {
		if p.ID == id {
			return p, nil
		}
	}

	return nil, fmt.Errorf(`pane ID "%s" not found`, id)
}

// ActivePane returns the currently active pane.
func (w *Window) ActivePane() (*Pane, error) {
	panes, err := w.Panes()
	if err != nil {
		return nil, err
	}

	for _, p := range panes {
		if p.Active {
			return p, nil
		}
	}

	return nil, fmt.Errorf(`no active pane for window "%s"`, w.ID)
}

// Split splits the specified pane or the active pane if none was specified.
func (w *Window) Split(paneID string, opts *SplitOptions) (*Pane, error) {
	var p *Pane
	var err error

	if paneID == "" {
		p, err = w.ActivePane()
	} else {
		p, err = w.Pane(paneID)
	}

	if err != nil {
		return nil, err
	}

	return p.Split(opts)
}

// Orientation is used to specify vertical or horizontal layout.
type Orientation int

// String converts the orientation value to a string that can be passed to
// tmux's command line.
func (o Orientation) String() string {
	if o == Vertical {
		return "-v"
	}
	return "-h"
}

const (
	Vertical Orientation = iota
	Horizontal
)

type SplitOptions struct {
	Orientation Orientation
	Title       string
}

func NewSplitOptions() *SplitOptions {
	return &SplitOptions{}
}

// Pane is a pane in a window.
type Pane struct {
	ID     string
	Active bool
	Index  int
	PID    int32
	Title  string
	Window *Window
}

// Kill kills the pane.
func (p *Pane) Kill() error {
	_, err := run(p, "kill-pane")
	return err
}

// Split creates a new pane by splitting the current pane either vertically or
// horizontally. The new pane is returned.
func (p *Pane) Split(opts *SplitOptions) (*Pane, error) {
	if opts == nil {
		opts = NewSplitOptions()
	}

	if opts.Title != "" {
		// TODO: set-titles and/or set-titles-string
	}

	_, err := run(p, "split-window", opts.Orientation.String())
	if err != nil {
		return nil, err
	}

	return p.Window.ActivePane()
}

// SendKeys sends key strokes to the pane. For example, to list files with sizes,
//   []string{"ls -l", "Enter"}.
func (p *Pane) SendKeys(keys ...string) (string, error) {
	return run(p, "send-keys", keys...)
}

// CapturePaneOptions specifies options when capture text from a pane.
type CapturePaneOptions struct {
	// BufferName is the name of the tmux buffer to capture text into.
	// If BufferName is "stdout", text will be written to stdout. Keep in mind
	// that it will be the stdout of the caller and not the pane.
	BufferName string
	// EndLine is the index of the last line of text to capture.
	EndLine int
	// StartLine is the index of the first line of text to capture.
	StartLine int
}

// NewCapturePaneOptions returns default options that can be passed to the
// CapturePane method.
func NewCapturePaneOptions() *CapturePaneOptions {
	return &CapturePaneOptions{
		BufferName: "stdout",
		EndLine:    math.MaxInt32,
		StartLine:  math.MaxInt32,
	}
}

// Capture captures text currently in the pane's buffer. The captured text
// is returned if BufferName is set to "stdout".
func (p *Pane) Capture(opts *CapturePaneOptions) (string, error) {
	args := []string{}

	if opts.BufferName == "stdout" {
		args = append(args, "-p")
	} else if opts.BufferName != "" {
		args = append(args, opts.BufferName)
	}

	if opts.EndLine != math.MaxInt32 {
		args = append(args, []string{"-E", strconv.Itoa(opts.EndLine)}...)
	}

	if opts.StartLine != math.MaxInt32 {
		args = append(args, []string{"-S", strconv.Itoa(opts.StartLine)}...)
	}

	return run(p, "capture-pane", args...)
}

// StartProcess uses send-keys to run a command in the pane.
// TODO: find some proper less cludgy and embarrasing way to do this.
func (p *Pane) StartProcess(cmd string, args ...string) (*Process, error) {
	args = append([]string{cmd}, args...)

	// See if this process is supposed to be backgrounded.
	bground := false
	for i, _ := range args {
		if args[i] == "&" {
			bground = true
			break
		}
	}

	// If the caller didn't request the process to run in the background,
	// temporarily start it in the background so we can easily grab the PID,
	// then we'll bring it into the foreground.
	if !bground {
		args = append(args, "&")
	}

	// Join command and args into a single string to send.
	cmdline := strings.Join(args, " ")

	// Send command line as keystrokes to the pane.
	_, err := p.SendKeys(cmdline, "Enter")
	if err != nil {
		return nil, err
	}

	// Print PID of new process in the pane.
	_, err = p.SendKeys("echo $!", "Enter")
	if err != nil {
		return nil, err
	}

	time.Sleep(100 * time.Millisecond)
	// Capture the text currently visible in the pane.
	//result, err := run(p, "capture-pane", "-p")
	result, err := p.Capture(NewCapturePaneOptions())
	if err != nil {
		return nil, err
	}

	// Split capture text into individual lines.
	lines := strings.Split(result, "\n")

	// Iterate backwards until we find a the line that could be a PID
	// and try to convert it.
	re := regexp.MustCompile(`^[\d]+$`)
	var line string
	for i := len(lines) - 1; i >= 0; i-- {
		if re.MatchString(lines[i]) {
			line = lines[i]
			break
		}
	}

	if line == "" {
		return nil, fmt.Errorf("couldn't get PID for new process")
	}

	pid, err := strconv.Atoi(line)
	if err != nil {
		return nil, err
	}

	return &Process{PID: int32(pid), Cmdline: cmdline, Pane: p}, nil
}

// Processes returns a list of processes owned by this pane.
func (p *Pane) Processes() ([]*Process, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	processes := []*Process{}
	for _, proc := range procs {
		ppid, err := proc.Ppid()
		if err != nil {
			return nil, err
		}
		if proc.Pid == p.PID || ppid == p.PID {
			cmdline, err := proc.Cmdline()
			if err != nil {
				return nil, err
			}
			processes = append(processes, &Process{
				PID:     proc.Pid,
				Cmdline: cmdline,
				Pane:    p,
			})
		}
	}

	return processes, nil
}

// Target returns the target name for the Pane. This is the value that would
// normally be passed to the -t <target> command line argument when running
// tmux commands.
func (p *Pane) Target() string {
	return fmt.Sprintf("%s:%s.%s", p.Window.Session.Name, p.Window.Name, p.ID)
}

// Process represents a running process on the system.
type Process struct {
	PID     int32
	Cmdline string
	Pane    *Pane
}

// Kill stops the process.
func (p *Process) Kill() error {
	proc, err := process.NewProcess(p.PID)
	if err != nil {
		return err
	}

	return proc.Kill()
}

// Start starts a process in a pane using Cmdline.
func (p *Process) Start() error {
	// See if we have a PID.
	if p.PID > 0 {
		// See if there is still a process with that PID.
		_, err := process.NewProcess(p.PID)
		if err == nil {
			return fmt.Errorf("process with PID %d already started", p.PID)
		}
		// Process for this PID no longer running so clear the PID and start
		// new process.
		p.PID = 0
	}

	if p.Cmdline == "" {
		return errors.New("no Cmdline set")
	}

	p2, err := p.Pane.StartProcess(p.Cmdline)
	if err != nil {
		return err
	}

	*p = *p2

	return nil
}

// Restart kills and restarts the process in th same pane.
func (p *Process) Restart() error {
	// Make sure we have either the PID of a running process or the command line
	// from a previous process.
	if p.PID > 0 && p.Cmdline == "" {
		return errors.New("process not running and no command line set")
	}

	// See if we have a PID.
	if p.PID > 0 {
		// See if there is still a process with that PID.
		proc, err := process.NewProcess(p.PID)
		if err == nil {
			// Grab the command line that started the process so we can reuse it.
			p.Cmdline, err = proc.Cmdline()
			if err != nil {
				return err
			}

			// Kill the process.
			if err := proc.Kill(); err != nil {
				return err
			}
		}

		// Clear the old PID.
		p.PID = 0
	}

	// Use send-keys to start a new process in the pane with the same command line.
	p2, err := p.Pane.StartProcess(p.Cmdline)
	if err != nil {
		return err
	}

	*p = *p2

	return nil
}

// targeter is any object that can generate a tmux target command line argument.
// I.e., many tmux commands take a -t <target> parameter.
type targeter interface {
	Target() string
}

// run runs a tmux command that is targeted at a specific session, window, or
// pane.
func run(t targeter, tmuxCmd string, args ...string) (string, error) {
	args = append([]string{"-t", t.Target()}, args...)
	return Run(tmuxCmd, args...)
}

// Run runs a tmux command with the provided arguments and returns the string
// read from the command's stdout.
func Run(tmuxCmd string, args ...string) (string, error) {
	args = append([]string{tmuxCmd}, args...)
	cmd := exec.Command("tmux", args...)
	//fmt.Printf("tmux %s \n", strings.Join(args, " "))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// toInts converts a slice of strings to ints. If there is an error converting
// any of the strins to ints, an error is returned.
func toInts(a []string) ([]int, error) {
	ints := make([]int, 0, len(a))
	for i, _ := range a {
		if a[i] == "" {
			continue
		}
		a[i] = strings.Trim(a[i], "'")
		n, err := strconv.Atoi(a[i])
		if err != nil {
			return nil, err
		}

		ints = append(ints, n)
	}
	return ints, nil
}

// parseWindow parses a string and returns a *Window.
func parseWindow(s string) (*Window, error) {
	s = strings.Trim(s, "'")
	fields := strings.Split(s, " ")

	const expFields = 4
	if len(fields) != expFields {
		return nil, fmt.Errorf("expected %d fields, got %d", expFields, len(fields))
	}

	index, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, err
	}

	window := &Window{
		ID:     fields[0],
		Index:  index,
		Active: fields[2] == "1",
		Name:   fields[3],
	}

	return window, nil
}

// parsePane parses a string and returns a pointer to a Pane.
func parsePane(s string) (*Pane, error) {
	s = strings.Trim(s, "'")
	fields := strings.Split(s, " ")

	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	index, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, err
	}

	pid, err := strconv.Atoi(fields[4])
	if err != nil {
		return nil, err
	}

	pane := &Pane{
		ID:     fields[0],
		Active: fields[3] == "1",
		Index:  index,
		PID:    int32(pid),
		Title:  fields[2],
	}

	return pane, nil
}
