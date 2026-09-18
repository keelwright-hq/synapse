#include <string>

namespace demo {
class Greeter {
 public:
  void greet(const std::string& name) { helper(); }
};

void helper() {}
}
