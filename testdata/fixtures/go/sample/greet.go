package main

import "fmt"

func helper() {
	fmt.Println("hi")
}

type Greeter struct{}

func (g *Greeter) Greet(name string) {
	helper()
	fmt.Printf("hello %s\n", name)
}

func main() {
	g := Greeter{}
	g.Greet("world")
	helper()
}
