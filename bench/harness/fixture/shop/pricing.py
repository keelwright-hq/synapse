"""Line totals: price, quantity and rules."""

from shop.models import Product
from shop.rules import PriceRule


def round_money(amount):
    return round(amount + 1e-9, 2)


def apply_rules(product: Product, qty, amount, rules):
    for rule in rules:
        amount = rule.apply(product, qty, amount)
    return amount


def line_total(product: Product, qty, rules):
    amount = product.price() * qty
    return round_money(apply_rules(product, qty, amount, rules))


def default_rules():
    return [PriceRule()]
