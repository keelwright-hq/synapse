"""Turn a cart into a receipt."""

from shop.cart import Cart
from shop.receipt import format_receipt
from shop.tax import tax_for


def checkout(cart: Cart, region):
    subtotal = cart.total()
    tax = tax_for(subtotal, region)
    return format_receipt(cart.lines, subtotal, tax)
