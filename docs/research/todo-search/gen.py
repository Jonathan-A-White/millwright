#!/usr/bin/env python3
"""Generate a fixture of To do cards as bd JSONL (what `bd export` prints, `bd import` reads).

  gen.py N SEED > cards.jsonl

N cards (>= 2000 for the story's fixture, 30000 for the live-scale one), created from
2023-01 to 2026-10, about 70% closed, about 40% with one to three comments, each carrying
the labels hitl and hitl:todo and a todo_rank. Ten cards are planted with an "old address"
(the PLANTED table): five closed in 2023-2024, found only by their words, three of them by a
word in a comment. Everything else is seeded noise built from a fixed vocabulary, so the same
N and SEED give the same file. Standard library only.
"""
import json
import random
import sys
from datetime import datetime, timedelta, timezone

WORDS = (
    "buy call send book renew pay fix cancel return pick order check update ask write plan "
    "dentist passport insurance landlord plumber invoice receipt recipe flight hotel train "
    "garden kitchen garage bike car tyre filter battery printer router cable charger laptop "
    "birthday present card letter parcel stamp bank tax form refund warranty subscription "
    "doctor vet haircut gym swim run walk hike camp ticket concert museum film book podcast "
    "mom dad sister cousin neighbour friend colleague accountant lawyer teacher school class "
    "paint shelf lamp curtain carpet sofa table chair mattress pillow towel bucket ladder "
    "milk bread cheese coffee pasta rice apple lemon onion garlic pepper salt sugar flour "
    "morning evening weekend monday friday march june october december next last every soon "
    "quote estimate deposit contract lease deed policy claim appeal licence permit visa "
    "photo scan print copy sign post file label box shelf drawer closet attic basement"
).split()

FILLER = (
    "the a of to for with and about before after when if then also please remember maybe "
    "this that it is was be need want should could would not still again already just"
).split()

# (title, description, comment, closed-year-month). Distinct streets, so a query for one
# street finds exactly its card. Two share the word "address" with the noise cards.
PLANTED = [
    ("Mom's new address", "Mom moved: 14 Harbor Lane, Apt 3, Portland ME 04101. Send the birthday card there.", None, "2023-03"),
    ("Update the bank with my address", "Branch needs proof of address.", "New address on the utility bill: 88 Quarry Road, Unit B, Burlington VT 05401.", "2023-06"),
    ("Landlord forwarding address", "Ask the old landlord where to send the deposit cheque. Forwarding address: 7 Juniper Street, Albany NY 12207.", None, "2023-09"),
    ("Send the tax form to my accountant", "Her office is at 301 Birch Hollow Way, Suite 12, Concord NH 03301.", None, "2024-01"),
    ("Order the replacement passport photo", "Passport office address for the form: 45 Maple Court, Hartford CT 06103.", None, "2024-02"),
    ("Return the printer", "Warranty return label.", "Return to the depot at 920 Tanner Mill Road, Nashua NH 03060 before the 30th.", "2024-05"),
    ("Cousin's wedding gift", "Registry ships to the couple's old address.", "Their new place: 2 Lantern Wharf, Newport RI 02840.", "2024-08"),
    ("Pick up the parcel", "Held at the depot, ID needed.", "Depot address is 63 Salt Marsh Drive, Gloucester MA 01930.", "2024-11"),
    ("Dentist moved", "New surgery address: 19 Orchard Row, Keene NH 03431. Rebook the check-up.", None, "2025-02"),
    ("Neighbour's old address for the insurance claim", "The claim form wants the prior address: 5 Cobbler's Lane, Rutland VT 05701.", None, "2025-04"),
]


def sentence(rng, n):
    return " ".join(rng.choice(WORDS if rng.random() < 0.6 else FILLER) for _ in range(n))


def iso(dt):
    return dt.strftime("%Y-%m-%dT%H:%M:%SZ")


def main():
    n, seed = int(sys.argv[1]), int(sys.argv[2])
    rng = random.Random(seed)
    start = datetime(2023, 1, 5, tzinfo=timezone.utc)
    span = (datetime(2026, 10, 14, tzinfo=timezone.utc) - start).total_seconds()
    planted_at = dict(zip(rng.sample(range(n), len(PLANTED)), PLANTED))
    cards = []
    for i in range(n):
        created = start + timedelta(seconds=rng.random() * span)
        title = sentence(rng, rng.randint(3, 7)).capitalize()
        desc = sentence(rng, rng.randint(40, 110))
        comments = []
        if rng.random() < 0.4:
            for _ in range(rng.randint(1, 3)):
                comments.append(sentence(rng, rng.randint(20, 160)))
        if rng.random() < 0.3:
            desc += "\nOpen: https://example.com/" + "".join(rng.choice("abcdef0123456789") for _ in range(12))
        if rng.random() < 0.01:
            desc += " change of address"
        closed = rng.random() < 0.7
        if i in planted_at:
            ptitle, pdesc, pcomment, ym = planted_at[i]
            title, desc = ptitle, pdesc
            comments = [pcomment] if pcomment else []
            y, m = map(int, ym.split("-"))
            created = datetime(y, m, 3, 10, tzinfo=timezone.utc)
            closed = True
        updated = created
        card = {
            "_type": "issue",
            "id": f"fx-{i:05d}",
            "title": title,
            "description": desc,
            "status": "closed" if closed else "open",
            "priority": 2,
            "issue_type": "task",
            "created_at": iso(created),
            "created_by": "governor",
            "updated_at": iso(updated),
            "metadata": {"todo_rank": i % 97},
            "labels": ["hitl", "hitl:todo"],
        }
        if closed:
            closed_at = created + timedelta(days=rng.randint(1, 120))
            card["closed_at"] = iso(closed_at)
            card["close_reason"] = "done"
            card["updated_at"] = iso(closed_at)
        if comments:
            card["comments"] = [
                {"id": f"fx-{i:05d}-c{k}", "issue_id": f"fx-{i:05d}", "author": "governor",
                 "text": text, "created_at": iso(created + timedelta(hours=k + 1))}
                for k, text in enumerate(comments)
            ]
        cards.append(card)
    for card in cards:
        print(json.dumps(card, separators=(",", ":")))


main()
