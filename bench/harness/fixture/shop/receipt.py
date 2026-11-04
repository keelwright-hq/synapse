"""Plain-text receipts."""

from shop.pricing import round_money


def format_receipt(lines, subtotal, tax):
    rows = [f"{p.name} x{q}" for p, q in lines]
    rows.append(f"subtotal {subtotal:.2f}")
    rows.append(f"tax {tax:.2f}")
    rows.append(f"total {round_money(subtotal + tax):.2f}")
    return "\n".join(rows)
