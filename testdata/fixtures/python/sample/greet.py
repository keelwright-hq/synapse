import os
from pathlib import Path

class Greeter:
    def greet(self, name):
        print(name)

def helper():
    g = Greeter()
    g.greet("world")
