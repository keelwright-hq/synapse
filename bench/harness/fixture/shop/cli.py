"""Command-line entry point."""

import sys

from shop.cart import Cart
from shop.checkout import checkout
from shop.models import Ebook, Magazine


def main(argv=None):
    region = (argv or sys.argv[1:] or ["north"])[0]
    cart = Cart()
    cart.add(Magazine("Monthly", 6.0), 2)
    cart.add(Ebook("Field Guide", 15.0))
    print(checkout(cart, region))


if __name__ == "__main__":
    main()
