using System;

namespace Demo {
  class Greeter {
    public void Greet(string name) {
      Console.WriteLine(name);
      Helper();
    }

    void Helper() {}
  }
}
