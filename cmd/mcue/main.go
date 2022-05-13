package main

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func main() {
	ctx := cuecontext.New()

	_ = ctx.CompileString(`
x: b: 2
x: c: 3
e: *x.a | (*x.b | x.c)
`, cue.Filename("bogus"))
}

//e1: *x.a | x.b | x.c
