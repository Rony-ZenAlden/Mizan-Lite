# Mizan — getting started

This guide is for the person who will use Mizan to run a shop. It covers installing it, setting it
up once, and knowing where your data lives.

It assumes nothing technical.

---

## 1. Installing

### Windows

1. Run **`Mizan <version> Setup.exe`**.
2. If Windows shows a blue "Windows protected your PC" box, choose **More info → Run anyway**.
   This appears because the installer is not signed by a paid certificate. It is expected.
3. Follow the installer. Mizan appears in the Start menu.

### macOS

1. Open **`Mizan <version>.dmg`**.
2. Drag **Mizan** into **Applications**.
3. The first time you open it, macOS may say the developer cannot be verified. Right-click the
   application and choose **Open**, then **Open** again. You only do this once.

Mizan needs no internet connection, before or after installing.

---

## 2. Setting up, once

The first time Mizan opens it asks for a few things. Take your time — these are difficult to
change later.

**Your business.** Its name, country, and currency. The country decides which chart of accounts
and tax rules Mizan starts with; the currency is the one your books are kept in and **cannot be
changed afterwards**.

**Your first branch and warehouse.** A single shop is one of each. Give them names you would say
out loud — "Front counter", "Back store".

**Your financial year.** The month it starts in. Most countries use January; some use April or
July. Ask whoever files your tax return if you are unsure.

**Your own account.** A username and a password. This account can do everything, so keep the
password somewhere safe — there is nobody to reset it for you.

---

## 3. Before you sell anything

Three things, in this order:

1. **Units** — pieces, kilograms, litres. Mizan comes with a standard set; add any you use.
2. **Products** — what you sell. Each needs a code and a name. If you have a list in a
   spreadsheet, **Import** on the settings screen will take a CSV with `code` and `name` columns.
3. **Stock** — how much you have. Either receive it against a purchase, or record an opening
   count.

Customers and suppliers can wait: a sale to a walk-in customer needs no record at all.

---

## 4. The screens you will use most

| Screen | What it answers |
|--------|-----------------|
| **Overview** | How is the business doing this month |
| **Till** | Selling |
| **Stock** | What is on the shelf, and what it is worth |
| **Purchases** | What was ordered, what arrived, what was invoiced |
| **Reports** | Profit and loss, balance sheet, what sells and what does not |
| **Needs attention** | Anything Mizan has noticed about itself |

---

## 5. Your data, and keeping it

**Where it lives.** One file on this computer. Mizan is not a website and does not send your data
anywhere.

**Backups happen daily, automatically.** Mizan takes a copy every day, checks that the copy can
actually be opened, and keeps the last seven. You do not need to do anything.

**Take one before anything risky.** Settings → Backups → *Back up now*. Before importing a large
file, before a big stock count, before anything you are unsure about.

**Copy backups off this computer.** This is the one thing Mizan cannot do for you. A backup on the
same disk does not survive that disk failing. Once a week, copy the newest file from the backups
folder onto a USB stick or another machine.

**Restoring** replaces everything with an older copy. Mizan takes a snapshot of your current data
first, so a restore can be undone, and asks you to close and reopen the application to finish.

---

## 6. When something looks wrong

Open **Needs attention**. Mizan checks itself and will tell you if:

- the books do not balance;
- the stock value and the accounts disagree;
- a daily task has failed;
- there has been no backup for several days.

Each notice explains what it found. None of them is decoration: they are conditions that are true
right now, and they disappear on their own when the condition does.

---

## 7. Getting help

When you ask for help, say:

- the **version**, from Settings → About;
- what you were doing;
- the exact wording of any message.

If you can, take a backup first and keep it. It is the fastest route to an answer.
