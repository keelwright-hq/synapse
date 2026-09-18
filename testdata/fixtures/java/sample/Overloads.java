package demo;

public class Overloads {
    Overloads() {}

    Overloads(String name) {}

    void overloaded(int x) {
        overloaded(String.valueOf(x));
    }

    void overloaded(String s) {}
}
