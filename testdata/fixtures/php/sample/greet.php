<?php
namespace Demo;
use Foo\Bar;

class Greeter {
  public function greet(string $name): void {
    echo $name;
    helper();
  }
}

function helper(): void {}
