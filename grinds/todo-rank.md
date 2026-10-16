# Ranking the Governor's To do board

You put every open card on the Governor's To do board in one of four rows, and
give each a why. His rule, in his words:

- **now** (Do now): important and due soon — past due, or due within two days.
  At most three cards.
- **make-time** (Make time): important and not urgent. Longest waiting first.
  At most three cards.
- **later** (Later): the rest.
- **let-go** (Let go): an unstarred card untouched for 30 days goes here, for
  him to keep or drop. A starred card never goes to let-go.

How to read the board:

- A card's `starred` is his own mark that it is important. A card he has not
  starred can still be important: read its title, its list and its Then line.
- `days_to_due` is how many days until it is due, less than zero when past due;
  a card with none has no due time. A card past due or due within two days
  (`days_to_due` at most 2) is due soon.
- `days_waiting` is how long since it was filed; `days_untouched` how long since
  it last changed. `row` is the row it stands in today, "" for none: keep a card
  where it is unless the rule or the day says otherwise, so the board does not
  churn from one night to the next.
- `size` is how big it is: quick, hour or half-day ("" when nobody said).
- `done` is what he closed in the last 30 days, with the day and hour he closed
  each: it shows when and what he gets done. `about` is his own words on his
  hours and his days.
- Prefer a quick card for a day his about file calls thin, and a big card for
  a day or an hour it says suits big things.
- When more cards fit now or make-time than three, put the rest in later.

The why: one line for him, under 80 characters, plain words, no bead ids,
saying why the card stands where it does ("Due Friday and starred", "Waiting
five weeks; fits a train ride", "Untouched since August").

Answer with every open card exactly once, by its bead id, and no other.
