// Copyright 2022 Robert S. Muhlestein.
// SPDX-License-Identifier: Apache-2.0

/*
Package Z (bonzai) provides a rooted node tree of commands and singular
parameters making tab completion a breeze and complicated applications
much easier to intuit without reading all the docs. Documentation is
embedded with each command removing the need for separate man pages and
such and can be viewed as text or a locally served web page.

Rooted Node Tree

Commands and parameters are linked to create a rooted node tree of the
following types of nodes:

    * Leaves with a method and optional parameters
		* Branches with leaves, other branches, and a optional method
		* Parameters, single words that are passed to a leaf command

*/
package Z

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rwxrob/bonzai"
	"github.com/rwxrob/term"
)

func init() {
	var err error
	// get the full path to current running process executable
	ExePath, err = os.Executable()
	if err != nil {
		log.Print(err)
		return
	}
	ExePath, err = filepath.EvalSymlinks(ExePath)
	if err != nil {
		log.Print(err)
	}
	ExeName = strings.TrimSuffix(
		filepath.Base(ExePath), filepath.Ext(ExePath))
}

// ExePath holds the full path to the current running process executable
// which is determined at init() time by calling os.Executable and
// passing it to path/filepath.EvalSymlinks to ensure it is the actual
// binary executable file. Errors are reported to stderr, but there
// should never be an error logged unless something is wrong with the Go
// runtime environment.
var ExePath string

// ExeName holds just the base name of the executable without any suffix
// (ex: .exe) and is set at init() time (see ExePath).
var ExeName string

// Commands contains the commands to lookup when Run-ing an executable
// in "multicall" mode. Each value must begin with a *Cmd and the rest
// will be assumed to be string arguments to prepend. See Run.
var Commands map[string][]any

// Conf may be optionally assigned any implementation of
// a bonzai.Configurer. Once assigned it should not be reassigned at any
// later time during runtime. Certain Bonzai branches and commands may
// require Z.Conf to be defined and those that do generally require the
// same implementation throughout all of runtime. Commands that require
// Z.Conf should set ReqConfig to true. Other than the exceptional case
// of configuration commands that fulfill bonzai.Configurer (and usually
// assign themselves to Z.Conf at init() time), commands must never
// require a specific implementation of bonzai.Configurer.  This
// encourages command creators and Bonzai tree composers to centralize
// on a single form of configuration without creating brittle
// dependencies and tight coupling. Configuration persistence can be
// implemented in any number of ways without a problem and Bonzai trees
// simply need to be recompiled with a different bonzai.Configurer
// implementation to switch everything that depends on configuration.
var Conf bonzai.Configurer

// UsageText is used for one-line UsageErrors. It's exported to allow
// for different languages.
var UsageText = `usage`

// UsageFunc is the default first-class function called if a Cmd that
// does not already define its own when usage information is needed (see
// bonzai.UsageFunc and Cmd.UsageError for more). By default,
// InferredUsage is assigned.
//
// It is used to return a usage summary. Generally, it should only
// return a single line (even if that line is very long).  Developers
// are encouraged to refer users to their chosen help command rather
// than producing usually long usage lines. If only the word "usage"
// needs to be changed (for a given language) consider UsageText
// instead. Note that most developers will simply change the Usage
// string when they do not want the default inferred usage string.
var UsageFunc = InferredUsage

// InferredUsage returns a single line of text summarizing only the
// Commands (less any Hidden commands), Params, and Aliases. If a Cmd
// is currently in an invalid state (Params without Call, no Call and no
// Commands) a string beginning with ERROR and wrapped in braces ({}) is
// returned instead. The string depends on the current language (see
// lang.go). Note that aliases does not include package Z.Aliases.
func InferredUsage(cmd bonzai.Command) string {

	x, iscmd := cmd.(*Cmd)
	if !iscmd {
		return "{ERROR: not a bonzai.Command}"
	}

	if x.Call == nil && x.Commands == nil {
		return "{ERROR: neither Call nor Commands defined}"
	}

	if x.Call == nil && x.Params != nil {
		return "{ERROR: Params without Call: " + strings.Join(x.Params, ", ") + "}"
	}

	params := UsageGroup(x.Params, x.MinParm, x.MaxParm)

	var names string
	if x.Commands != nil {
		var snames []string
		for _, x := range x.Commands {
			snames = append(snames, x.UsageNames())
		}
		if len(snames) > 0 {
			names = UsageGroup(snames, 1, 1)
		}
	}

	if params != "" && names != "" {
		return "(" + params + "|" + names + ")"
	}

	if params != "" {
		return params
	}

	return names
}

// Run infers the name of the command to run from the ExeName looked up
// in the Commands delegates accordingly, prepending any arguments
// provided in the Cmd.Run. Run produces an "unmapped multicall command"
// error if no match is found. This is an alternative to the simpler,
// direct Cmd.Run method from main where only one possible Cmd will ever
// be the root and allows for BusyBox (https://www.busybox.net)
// multicall binaries to be used for such things as very light-weight
// Linux distributions when used "FROM SCRATCH" in containers.
func Run() {
	if v, has := Commands[ExeName]; has {
		if len(v) < 1 {
			ExitError(fmt.Errorf("multicall command missing"))
		}
		cmd, iscmd := v[0].(*Cmd)
		if !iscmd {
			ExitError(fmt.Errorf("first value must be *Cmd"))
		}
		args := []string{cmd.Name}
		if len(v) > 1 {
			rest := os.Args[1:]
			for _, a := range v[1:] {
				s, isstring := a.(string)
				if !isstring {
					ExitError(fmt.Errorf("only string arguments allowed"))
				}
				args = append(args, s)
			}
			args = append(args, rest...)
		}
		os.Args = args
		cmd.Run()
		Exit()
	}
	ExitError(fmt.Errorf("unmapped multicall command: %v", ExeName))
}

// Method defines the main code to execute for a command (Cmd). By
// convention the parameter list should be named "args" if there are
// args expected and underscore (_) if not. Methods must never write
// error output to anything but standard error and should almost always
// use the log package to do so.
type Method func(caller *Cmd, args ...string) error

// DoNotExit effectively disables Exit and ExitError allowing the
// program to continue running, usually for test evaluation.
var DoNotExit bool

// ExitOff sets DoNotExit to false.
func ExitOff() { DoNotExit = true }

// ExitOn sets DoNotExit to true.
func ExitOn() { DoNotExit = false }

// Exit calls os.Exit(0) unless DoNotExit has been set to true. Cmds
// should never call Exit themselves returning a nil error from their
// Methods instead.
func Exit() {
	if !DoNotExit {
		os.Exit(0)
	}
}

// ExitError prints err and exits with 1 return value unless DoNotExit
// has been set to true. Commands should usually never call ExitError
// themselves returning an error from their Method instead.
func ExitError(err ...interface{}) {
	switch e := err[0].(type) {
	case string:
		if len(e) > 1 {
			log.Printf(e+"\n", err[1:]...)
		} else {
			log.Println(e)
		}
	case error:
		out := fmt.Sprintf("%v", e)
		if len(out) > 0 {
			log.Println(out)
		}
	}
	if !DoNotExit {
		os.Exit(1)
	}
}

// ArgsFrom returns a list of field strings split on space with an extra
// trailing special space item appended if the line has any trailing
// spaces at all signifying a definite word boundary and not a potential
// prefix.
func ArgsFrom(line string) []string {
	args := []string{}
	if line == "" {
		return args
	}
	args = strings.Fields(line)
	if line[len(line)-1] == ' ' {
		args = append(args, "")
	}
	return args
}

// ArgsOrIn takes an slice or nil as argument and if the slice has any
// length greater than 0 returns all the argument joined together with
// a single space between them. Otherwise, will read standard input
// until end of file reached (Cntl-D).
func ArgsOrIn(args []string) string {
	if args == nil || len(args) == 0 {
		return term.Read()
	}
	return strings.Join(args, " ")
}

// Aliases allows Bonzai tree developers to create aliases (similar to
// shell aliases) that are directly translated into arguments to the
// Bonzai tree executable by overriding the os.Args in a controlled way.
// The value of an alias is always a slice of strings that will replace
// the os.Args[2:]. A slice is used (instead of a string parsed with
// strings.Fields) to ensure that hard-coded arguments containing
// whitespace are properly handled.
var Aliases = make(map[string][]string)

// AllowPanic disables TrapPanic stopping it from cleaning panic errors.
var AllowPanic = false

// TrapPanic recovers from any panic and more gracefully displays the
// panic by logging it before exiting with a return value of 1.
var TrapPanic = func() {
	if !AllowPanic {
		if r := recover(); r != nil {
			log.Println(r)
			os.Exit(1)
		}
	}
}
