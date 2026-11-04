"""Discount rules applied to line totals."""

from shop.models import GiftCard, Product


class PriceRule:
    """Base class for a rule that adjusts a line amount."""

    def apply(self, product: Product, qty, amount):
        return amount


class BulkDiscount(PriceRule):
    def apply(self, product: Product, qty, amount):
        if qty >= 10 and not isinstance(product, GiftCard):
            return amount * 0.9
        return amount


class MemberDiscount(PriceRule):
    def apply(self, product: Product, qty, amount):
        if isinstance(product, GiftCard):
            return amount
        return amount * 0.95
