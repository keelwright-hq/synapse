package main

import "fmt"

// helper prints a short greeting used by Greeter.
func helper() {
	fmt.Println("hi")
}

type Greeter struct{}

// Greet prints a personalized hello.
func (g *Greeter) Greet(name string) {
	helper()
	fmt.Printf("hello %s\n", name)
}

func main() {
	g := Greeter{}
	g.Greet("world")
	helper()
}
