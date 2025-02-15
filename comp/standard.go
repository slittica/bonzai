// Copyright 2022 Robert S. Muhlestein.
// SPDX-License-Identifier: Apache-2.0

package comp

import (
	"github.com/rwxrob/bonzai"
	"github.com/rwxrob/fn/filt"
	"github.com/rwxrob/structs/set/text/set"
)

// Standard completion is resolved as follows:
//
//     1. If leaf has Completer function, delegate to it
//
//     2. If leaf has no arguments, return all Commands and Params
//
//     3. If first argument is the name of a Command return it only even
//        if in the Hidden list
//
//     4. Otherwise, return every Command or Param that is not in the
//        Hidden list and HasPrefix matching the first arg
//
// See bonzai.Completer.
func Standard(x bonzai.Command, args ...string) []string {

	// if has completer, delegate
	if c := x.GetCompleter(); c != nil {
		return c(x, args...)
	}

	// not sure we've completed the command name itself yet
	if len(args) == 0 {
		return []string{x.GetName()}
	}

	// build list of visible commands and params
	list := []string{}
	list = append(list, x.GetCommandNames()...)
	list = append(list, x.GetParams()...)
	list = set.Minus[string, string](list, x.GetHidden())

	if len(args) == 0 {
		return list
	}

	return filt.HasPrefix(list, args[0])
}
