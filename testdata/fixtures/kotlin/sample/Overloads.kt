package demo

class Overloads {
    constructor()
    constructor(name: String)

    fun overloaded(x: Int) {
        overloaded(x.toString())
    }

    fun overloaded(s: String) {}
}

fun overloaded(x: Int) {}
fun overloaded(s: String) {}
