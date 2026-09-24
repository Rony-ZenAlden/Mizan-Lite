# Shop guide — Mizan Lite

This guide is for whoever stands at the counter. Read it once and keep it near the computer. Everything works without the internet, and everything you record stays on this computer alone.

---

## The first day

1. Open the application. The setup screen appears once: the shop's name, the interface language, the owner PIN, and today's exchange rate.
2. **The owner PIN** is 6 to 12 digits. It is not asked for when selling — only for what belongs to the owner: voiding a sale, discounts, changing the rate, the reports, the backups.
3. After setup a **recovery code** is shown. Write it on paper and keep it away from the computer. If the owner PIN is forgotten, it is the only way back. It is not shown again.
4. Enter your products: one by one on the Products screen, or all at once from an Excel file (see "Products from Excel").
5. Enter the stock you have today: the quantity and the cost per unit of each item.
6. If you keep a paper debt book, enter the customers and their old balances on the Customers screen.

## Every morning

- Open the application and look at the exchange rate at the top. If the market rate has moved, open **Exchange rate** and write today's rate.
- If a backup warning is at the top of the screen, do what it says.

## Selling

1. Scan the barcode, or type the item's name and press Enter, or press the item's button in the quick grid.
2. The item is added to the sale. For something weighed, the application asks for the weight.
3. **Keys:** Enter or F9 to pay · F4 cash or credit · F2 the last line's quantity · + and − change the last line · Esc closes search results.
4. Choose the sale's currency: pounds or dollars. Type what the customer handed over, and the change is worked out.
5. Press **Pay**. The receipt appears on screen, and prints if a printer is set up.

**Rounding:** totals in pounds are rounded to the nearest note (500 pounds by default). The rounding is shown on the receipt.

**Discounts:** a discount on a line or on the whole sale needs the owner PIN.

## Credit sales and debts

- Press **On credit** (or F4) before paying, and choose the customer. The debt is recorded in the sale's currency.
- What the customer pays now is taken off, and the rest is added to their debt. The receipt shows the debt, the balance after it and a line to sign.
- **Taking a payment:** Customers → Statement → Take payment. A payment may be made in the other currency at today's rate, and the change is shown.
- Every payment prints a **voucher** with its own number. Give the customer a copy.
- Each currency has its own balance. Pounds and dollars are never added together; the figure beside a balance in the other currency is for reference only.

## Voiding a sale

Sales → the receipt → **Void sale**. Before the PIN, the application states the cash to hand back, then asks why. A void returns the goods to stock and reverses the debt if the sale was on credit.

## The cash drawer

- For each currency it shows what should be in the drawer today: the opening, cash taken, change given, debts repaid, money handed back.
- **Count the drawer** at the end of the day: type what is actually there, and the difference is recorded. Counting needs no PIN.
- **Expense**, **Take money out**, **Put money in** need the owner PIN, and appear in the cash book.

## Products and stock

- **Products:** the name, barcode, unit, price and its currency. Changing a price needs the owner PIN.
- **Stock:** receive a delivery with its cost, count what is on the shelf, write off damaged goods, open a large container into smaller units.
- An item's cost is a **weighted average** the application updates with every delivery.

## Suppliers and purchases

- **Suppliers** keeps what the shop owes the people it buys from, apart from what customers owe it: each supplier's balance in each currency, and every purchase and payment.
- **New purchase:** choose the supplier, add the items that arrived (type the name or scan the barcode), and for each one how many came, how many were **damaged**, the price of one and any discount (a percentage or an amount). A discount on the whole invoice goes below. The application works out every line and what one good unit really cost.
- Damaged units are **not received into stock and not charged**. If you already paid for them, the supplier owes the shop the difference.
- Pay what you can now — from the **drawer** or from **your own money**, chosen each time — and the rest goes on the supplier's account. Money paid from the drawer comes off what the drawer should hold.
- A purchase entered by mistake is **voided** from its page, with a reason. Purchases, payments and voids need the owner PIN when the PIN is switched on.

## Spoilage and losses

- **Spoilage & losses → Record spoilage:** find the item, type how much was lost, and say why — **damaged**, **expired** or **spoiled**. It comes off the shelf at its average cost and needs the owner PIN.
- The screen shows the period's losses at what the goods cost, by reason and line by line, and — apart — the goods that **arrived damaged** from suppliers, which the supplier did not charge for.

## US dollars only

- Settings → **How money is shown → US dollars only** converts the whole shop to dollars at today's rate: every price and cost, every customer's and supplier's balance in pounds, and the pounds in the drawer.
- The application **shows every figure before anything changes**. Check them, tick the box and confirm with the owner PIN. If anything changed in between — a sale, a payment — it shows the new figures and asks again.
- After that the till, the prices, the drawer and the reports are in dollars only. To go back, choose another way of showing money: everything stays in dollars.

## Products from Excel

Products → **Import from Excel**:

1. Save the template and open it in Excel.
2. Type your products: the Arabic name (required), the English name, the barcode, the unit, the price's currency, the price, and the quantity and cost if you have stock.
3. Save the file, then choose it in the application. It shows what will be created and what is wrong, with the row number.
4. Fix the rows in Excel and choose the file again. Nothing is imported until every row is right.
5. Press **Import** and enter the owner PIN.

## Reports

Reports (with the owner PIN): the day, the month, the products and the stock. Profit is in dollars at cost, and in pounds at each sale's own rate. Any report can be exported to Excel or PDF.

## Printing

Printer: choose the receipt printer, the paper width (80 or 58 mm) and how receipts reach it. Print a **test page** to be sure.

- Credit sales and vouchers print themselves; cash receipts print on the button.
- If a receipt does not print, the application says why and offers **Print again**. The sale is recorded either way.

## Backups

- A backup is taken every day and when the application closes, and before any update or restore.
- **Choose an outside folder** (a USB drive, say) and every backup is copied there and checked. If the drive is out, the application says the last outside copy is old.
- **Restoring** returns the shop to the moment of a backup: the application says what will be removed, takes a safety copy first, then restarts itself.

## What to do if…

| The situation | What to do |
|---|---|
| The owner PIN is forgotten | Use the recovery code written down on setup day; open the Owner screen |
| The printer does not print | A test page from the Printer screen; check paper and cable, then try "Straight to the printer" |
| The exchange rate is old | Open Exchange rate and write today's rate |
| The drawer count differs | The difference is recorded, never hidden; look at the cash book for the day |
| The computer went off suddenly | Open the application; everything recorded before it went off is there |
| You need help | About → Save a support file, and send it to whoever helps you |
