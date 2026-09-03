import Foundation

struct User {
    var name: String
}

class Greeter {
    func greet(_ name: String) {
        print(name)
    }
}

func helper() {
    let g = Greeter()
    g.greet("world")
}
