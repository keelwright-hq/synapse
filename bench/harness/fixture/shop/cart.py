"""A shopping cart."""

from shop.models import Product
from shop.pricing import default_rules, line_total


class Cart:
    def __init__(self, rules=None):
        self.lines = []
        self.rules = rules if rules is not None else default_rules()

    def add(self, product: Product, qty=1):
        self.lines.append((product, qty))

    def total(self):
        return sum(line_total(p, q, self.rules) for p, q in self.lines)
