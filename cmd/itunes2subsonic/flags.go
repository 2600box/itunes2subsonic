package main

import "flag"

func collectSetFlags() map[string]bool {
	setFlags := make(map[string]bool)
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})
	return setFlags
}
