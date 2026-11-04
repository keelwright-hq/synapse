from shop.models import Magazine
from shop.pricing import line_total, round_money
from shop.rules import BulkDiscount


def test_round_money():
    assert round_money(1.005) == 1.01


def test_bulk_discount():
    assert line_total(Magazine("Weekly", 2.0), 10, [BulkDiscount()]) == 18.0
