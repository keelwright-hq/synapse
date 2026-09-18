#include <stdio.h>

struct Greeter {
  int x;
};

void greet(const char *name) {
  printf("%s", name);
  helper();
}

static void helper(void) {}
