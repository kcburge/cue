package main

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func main() {
	ctx := cuecontext.New()

	v := ctx.CompileString(`
x: b: 2
x: c: 3
e: *x.a | (*x.b | x.c)
`, cue.Filename("bogus"))
	fmt.Printf("%+v\n", v)
}

//e1: *x.a | x.b | x.c
