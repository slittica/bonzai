// Copyright 2022 Robert S. Muhlestein.
// SPDX-License-Identifier: Apache-2.0

package Z

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/rwxrob/bonzai"
	"github.com/rwxrob/bonzai/comp"
	"github.com/rwxrob/fn/each"
	"github.com/rwxrob/fn/maps"
	"github.com/rwxrob/fn/redu"
	"github.com/rwxrob/structs/qstack"
)

type Cmd struct {
	Name        string    `json:"name,omitempty"`
	Aliases     []string  `json:"aliases,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	Usage       string    `json:"usage,omitempty"`
	Version     string    `json:"version,omitempty"`
	Copyright   string    `json:"copyright,omitempty"`
	License     string    `json:"license,omitempty"`
	Description string    `json:"description,omitempty"`
	Site        string    `json:"site,omitempty"`
	Source      string    `json:"source,omitempty"`
	Issues      string    `json:"issues,omitempty"`
	Commands    []*Cmd    `json:"commands,omitempty"`
	Params      []string  `json:"params,omitempty"`
	Hidden      []string  `json:"hidden,omitempty"`
	Other       []Section `json:"other,omitempty"`

	Completer bonzai.Completer `json:"-"`
	UsageFunc bonzai.UsageFunc `json:"-"`

	Caller  *Cmd   `json:"-"`
	Call    Method `json:"-"`
	MinArgs int    `json:"-"` // minimum number of args required (including parms)
	MinParm int    `json:"-"` // minimum number of params required
	MaxParm int    `json:"-"` // maximum number of params required
	ReqConf bool   `json:"-"` // requires Z.Conf be assigned

	_aliases  map[string]*Cmd   // see cacheAliases called from Run
	_sections map[string]string // see cacheSections called from Run
}

// Section contains the Other sections of a command. Composition
// notation (without Title and Body) is not only supported but
// encouraged for clarity when reading the source for documentation.
type Section struct {
	Title string
	Body  string
}

func (s Section) GetTitle() string { return s.Title }
func (s Section) GetBody() string  { return s.Body }

// Names returns the Name and any Aliases grouped such that the Name is
// always last.
func (x *Cmd) Names() []string {
	var names []string
	names = append(names, x.Aliases...)
	names = append(names, x.Name)
	return names
}

// UsageNames returns single name, joined Names with bar (|) and wrapped
// in parentheses, or empty string if no names.
func (x *Cmd) UsageNames() string { return UsageGroup(x.Names(), 1, 1) }

// UsageParams returns the Params in UsageGroup notation.
func (x *Cmd) UsageParams() string {
	return UsageGroup(x.Params, x.MinParm, x.MaxParm)
}

// UsageCmdNames returns the Names for each of its Commands joined, if
// more than one, with usage regex notation.
func (x *Cmd) UsageCmdNames() string {
	var names []string
	for _, n := range x.Commands {
		names = append(names, n.UsageNames())
	}
	return UsageGroup(names, 1, 1)
}

// Title returns a dynamic field of Name and Summary combined (if
// exists). If the Name field of the commands is not defined will return
// a "{ERROR}".
func (x *Cmd) Title() string {
	if x.Name == "" {
		return "{ERROR: Name is empty}"
	}
	switch {
	case len(x.Summary) > 0:
		return x.Name + " - " + x.Summary
	default:
		return x.Name
	}
}

// Legal returns a single line with the combined values of the
// Name, Version, Copyright, and License. If Version is empty or nil an
// empty string is returned instead. Legal() is used by the
// version builtin command to aggregate all the version information into
// a single output.
func (x *Cmd) Legal() string {
	switch {
	case len(x.Copyright) > 0 && len(x.License) == 0 && len(x.Version) == 0:
		return x.Name + " " + x.Copyright
	case len(x.Copyright) > 0 && len(x.License) > 0 && len(x.Version) > 0:
		return x.Name + " (" + x.Version + ") " +
			x.Copyright + "\nLicense " + x.License
	case len(x.Copyright) > 0 && len(x.License) > 0:
		return x.Name + " " + x.Copyright + "\nLicense " + x.License
	case len(x.Copyright) > 0 && len(x.Version) > 0:
		return x.Name + " (" + x.Version + ") " + x.Copyright
	case len(x.Copyright) > 0:
		return x.Name + "\n" + x.Copyright
	default:
		return ""
	}
}

// OtherTitles returns just the ordered titles from Other.
func (x *Cmd) OtherTitles() []string { return maps.Keys(x._sections) }

func (x *Cmd) cacheAliases() {
	x._aliases = map[string]*Cmd{}
	if x.Commands == nil {
		return
	}
	for _, c := range x.Commands {
		if c.Aliases == nil {
			continue
		}
		for _, a := range c.Aliases {
			x._aliases[a] = c
		}
	}
}

func (x *Cmd) cacheSections() {
	x._sections = map[string]string{}
	if len(x.Other) == 0 {
		return
	}
	for _, s := range x.Other {
		x._sections[s.Title] = s.Body
	}
}

// Run is for running a command within a specific runtime (shell) and
// performs completion if completion context is detected.  Otherwise, it
// executes the leaf Cmd returned from Seek calling its Method, and then
// Exits. Normally, Run is called from within main() to convert the Cmd
// into an actual executable program and normally it exits the program.
// Exiting can be controlled, however, with ExitOn/ExitOff when testing
// or for other purposes requiring multiple Run calls. Using Call
// instead will also just call the Cmd's Call Method without exiting.
// Note: Only bash runtime ("COMP_LINE") is currently supported, but
// others such a zsh and shell-less REPLs are planned.
func (x *Cmd) Run() {
	defer TrapPanic()

	x.cacheAliases()
	x.cacheSections()

	// resolve Z.Aliases (if completion didn't replace them)
	if len(os.Args) > 1 {
		args := []string{os.Args[0]}
		alias := Aliases[os.Args[1]]
		if alias != nil {
			args = append(args, alias...)
			args = append(args, os.Args[2:]...)
			os.Args = args
		}
	}

	// bash completion context
	line := os.Getenv("COMP_LINE")
	if line != "" {
		var list []string
		lineargs := ArgsFrom(line)
		if len(lineargs) == 2 {
			list = append(list, maps.KeysWithPrefix(Aliases, lineargs[1])...)
		}
		cmd, args := x.Seek(lineargs[1:])
		if cmd.Completer == nil {
			list = append(list, comp.Standard(cmd, args...)...)
			if len(list) == 1 && len(lineargs) == 2 {
				if v, has := Aliases[list[0]]; has {
					fmt.Println(strings.Join(EscAll(v), " "))
					Exit()
				}
			}
			each.Println(list)
			Exit()
		}
		each.Println(cmd.Completer(cmd, args...))
		Exit()
	}

	// seek should never fail to return something, but ...
	cmd, args := x.Seek(os.Args[1:])
	if cmd == nil {
		ExitError(x.UsageError())
	}

	// default to first Command if no Call defined
	if cmd.Call == nil {
		if len(cmd.Commands) > 0 {
			fcmd := cmd.Commands[0]
			if fcmd.Call == nil {
				ExitError(fmt.Errorf("default commands require Call function"))
			}
			fcmd.Caller = cmd
			cmd = fcmd
		} else {
			ExitError(x.Unimplemented())
		}
	}

	if len(args) < cmd.MinArgs {
		ExitError(cmd.UsageError())
	}

	if x.ReqConf && Conf == nil {
		ExitError(cmd.ReqConfError())
	}

	// delegate
	if cmd.Caller == nil {
		cmd.Caller = x
	}
	if err := cmd.Call(cmd, args...); err != nil {
		ExitError(err)
	}
	Exit()
}

// UsageError returns an error with a single-line usage string. The word
// "usage" can be changed by assigning Z.UsageText to something else.
// The commands own UsageFunc will be used if defined. If undefined, the
// Z.UsageFunc will be used instead (which can also be assigned
// to something else if needed).
func (x *Cmd) UsageError() error {
	usage := x.UsageFunc
	if usage == nil {
		usage = UsageFunc
	}
	return fmt.Errorf("%v: %v %v", UsageText, x.Name, usage(x))
}

// ReqConfError returns stating that the given command requires that
// Z.Conf be set to something besides null. This is primarily for
// those composing commands that import a given command to help the
// develop know about the dependency.
func (x *Cmd) ReqConfError() error {
	return fmt.Errorf(
		"cmd %q requires a configurer (Z.Conf must be assigned)",
		x.Name,
	)
}

// Unimplemented returns an error with a single-line usage string.
func (x *Cmd) Unimplemented() error {
	return fmt.Errorf("%q has not yet been implemented", x.Name)
}

// MissingConfig returns an error showing the expected configuration
// entry that is missing from the given path.
func (x *Cmd) MissingConfig(path string) error {
	return fmt.Errorf("missing config: %v", x.PathString()+"."+path)
}

// Add creates a new Cmd and sets the name and aliases and adds to
// Commands returning a reference to the new Cmd. The name must be
// first.
func (x *Cmd) Add(name string, aliases ...string) *Cmd {
	c := &Cmd{
		Name:    name,
		Aliases: aliases,
	}
	x.Commands = append(x.Commands, c)
	return c
}

// Resolve looks up a given Command by name or name from Aliases.
func (x *Cmd) Resolve(name string) *Cmd {
	if x.Commands == nil {
		return nil
	}
	for _, c := range x.Commands {
		if name == c.Name {
			return c
		}
	}
	if c, has := x._aliases[name]; has {
		return c
	}
	return nil
}

// CmdNames returns the names of every Command.
func (x *Cmd) CmdNames() []string {
	list := []string{}
	for _, c := range x.Commands {
		if c.Name == "" {
			continue
		}
		list = append(list, c.Name)
	}
	return list
}

// UsageCmdTitles returns a single string with the titles of each
// subcommand indented and with a maximum title signature length for
// justification.  Hidden commands are not included. Note that the order
// of the Commands is preserved (not necessarily alphabetic).
func (x *Cmd) UsageCmdTitles() string {
	var set []string
	var summaries []string
	for _, c := range x.Commands {
		set = append(set, strings.Join(c.Names(), "|"))
		summaries = append(summaries, c.Summary)
	}
	longest := redu.Longest(set)
	var buf string
	for n := 0; n < len(set); n++ {
		if len(summaries[n]) > 0 {
			buf += fmt.Sprintf(`%-`+strconv.Itoa(longest)+"v - %v\n", set[n], summaries[n])
		} else {
			buf += fmt.Sprintf(`%-`+strconv.Itoa(longest)+"v\n", set[n])
		}
	}
	return buf
}

// Param returns Param matching name if found, empty string if not.
func (x *Cmd) Param(p string) string {
	if x.Params == nil {
		return ""
	}
	for _, c := range x.Params {
		if p == c {
			return c
		}
	}
	return ""
}

// IsHidden returns true if the specified name is in the list of
// Hidden commands.
func (x *Cmd) IsHidden(name string) bool {
	if x.Hidden == nil {
		return false
	}
	for _, h := range x.Hidden {
		if h == name {
			return true
		}
	}
	return false
}

func (x *Cmd) Seek(args []string) (*Cmd, []string) {
	if args == nil || x.Commands == nil {
		return x, args
	}
	cur := x
	n := 0
	for ; n < len(args); n++ {
		next := cur.Resolve(args[n])
		if next == nil {
			break
		}
		next.Caller = cur
		cur = next
	}
	return cur, args[n:]
}

// Path returns the path of command names used to arrive at this
// command. The path is determined by walking backward from current
// Caller up rather than depending on anything from the command line
// used to invoke the composing binary. Also see PathString.
func (x *Cmd) Path() []string {
	path := qstack.New[string]()
	path.Unshift(x.Name)
	for p := x.Caller; p != nil; p = p.Caller {
		path.Unshift(p.Name)
	}
	path.Shift()
	return path.Items()
}

// PathString returns a dotted notation of the Path. This is useful for
// associating configuration and other data specifically with this
// command.
func (x *Cmd) PathString() string {
	return strings.Join(x.Path(), ".")
}

// Log is currently short for log.Printf() but may be supplemented in
// the future to have more fine-grained control of logging.
func (x *Cmd) Log(format string, a ...any) {
	log.Printf(format, a...)
}

// Q is a shorter version of Z.Conf.Query(x.Path()+"."+q) for
// convenience. Logs the error and returns a blank string if Z.Conf is
// not defined (see ReqConf).
func (x *Cmd) Q(q string) string {
	if Conf == nil {
		log.Printf("cmd %q requires a configurer (Z.Conf must be assigned)", x.Name)
		return ""
	}
	return Conf.Query(x.PathString() + "." + q)
}

// --------------------- bonzai.Command interface ---------------------

// GetName fulfills the bonzai.Command interface.
func (x *Cmd) GetName() string { return x.Name }

// GetTitle fulfills the bonzai.Command interface.
func (x *Cmd) GetTitle() string { return x.Title() }

// GetAliases fulfills the bonzai.Command interface.
func (x *Cmd) GetAliases() []string { return x.Aliases }

// Summary fulfills the bonzai.Command interface.
func (x *Cmd) GetSummary() string { return x.Summary }

// Usage fulfills the bonzai.Command interface.
func (x *Cmd) GetUsage() string { return x.Usage }

// Version fulfills the bonzai.Command interface.
func (x *Cmd) GetVersion() string { return x.Version }

// Copyright fulfills the bonzai.Command interface.
func (x *Cmd) GetCopyright() string { return x.Copyright }

// License fulfills the bonzai.Command interface.
func (x *Cmd) GetLicense() string { return x.License }

// Description fulfills the bonzai.Command interface.
func (x *Cmd) GetDescription() string { return x.Description }

// Site fulfills the bonzai.Command interface.
func (x *Cmd) GetSite() string { return x.Site }

// Source fulfills the bonzai.Command interface.
func (x *Cmd) GetSource() string { return x.Source }

// Issues fulfills the bonzai.Command interface.
func (x *Cmd) GetIssues() string { return x.Issues }

// MinArgs fulfills the bonzai.Command interface.
func (x *Cmd) GetMinArgs() int { return x.MinArgs }

// MinParm fulfills the bonzai.Command interface.
func (x *Cmd) GetMinParm() int { return x.MinParm }

// MaxParm fulfills the bonzai.Command interface.
func (x *Cmd) GetMaxParm() int { return x.MaxParm }

// ReqConf fulfills the bonzai.Command interface.
func (x *Cmd) GetReqConf() bool { return x.ReqConf }

// UsageFunc fulfills the bonzai.Command interface.
func (x *Cmd) GetUsageFunc() bonzai.UsageFunc { return x.UsageFunc }

// GetCommands fulfills the bonzai.Command interface.
func (x *Cmd) GetCommands() []bonzai.Command {
	var commands []bonzai.Command
	for _, s := range x.Commands {
		commands = append(commands, bonzai.Command(s))
	}
	return commands
}

// GetCommandNames fulfills the bonzai.Command interface.
func (x *Cmd) GetCommandNames() []string { return x.CmdNames() }

// GetHidden fulfills the bonzai.Command interface.
func (x *Cmd) GetHidden() []string { return x.Hidden }

// GetParams fulfills the bonzai.Command interface.
func (x *Cmd) GetParams() []string { return x.Params }

// GetOther fulfills the bonzai.Command interface.
func (x *Cmd) GetOther() []bonzai.Section {
	var sections []bonzai.Section
	for _, s := range x.Other {
		sections = append(sections, bonzai.Section(s))
	}
	return sections
}

// GetOtherTitles fulfills the bonzai.Command interface.
func (x *Cmd) GetOtherTitles() []string { return x.OtherTitles() }

// GetCompleter fulfills the Command interface.
func (x *Cmd) GetCompleter() bonzai.Completer { return x.Completer }

// GetCaller fulfills the bonzai.Command interface.
func (x *Cmd) GetCaller() bonzai.Command { return x.Caller }
