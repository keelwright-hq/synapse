"""Products sold by the shop."""


class Product:
    """Something with a name and a base price."""

    def __init__(self, name, base_price):
        self.name = name
        self.base_price = base_price

    def price(self):
        return self.base_price


class Magazine(Product):
    """A printed issue, priced per copy."""


class Ebook(Product):
    """A download with no shipping cost."""

    def price(self):
        return self.base_price * 0.8


class GiftCard(Product):
    """Stored value; never discounted."""

    def price(self):
        return self.base_price
