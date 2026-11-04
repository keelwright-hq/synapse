"""Sales tax by region."""

from shop.pricing import round_money

RATES = {"north": 0.05, "south": 0.08}


def tax_for(amount, region):
    return round_money(amount * RATES.get(region, 0.0))
