import { useState } from "react";
import type { Customer, Entry } from "@/api/client";
import { CustomerPicker } from "@/screens/customers/CustomerPicker";
import { PaymentDialog } from "@/screens/customers/PaymentDialog";

/**
 * Taking a payment against a customer's debt without leaving the till (the owner's request, 2026-09-20).
 *
 * A customer walks in to settle what they owe, often while another is waiting to be served. Sending the cashier to
 * the Customers screen and back costs two navigations and loses whatever was in the cart.
 *
 * It is the Customers screen's own two dialogs, in order — the picker, then the payment — so the arithmetic, the
 * change, the rounding and the voucher are the ones that already exist rather than a second implementation of them
 * that could disagree.
 */
export function QuickRepayment({
  localCurrency,
  onDone,
  onClose,
}: {
  localCurrency: string;
  onDone: (e: Entry) => void;
  onClose: () => void;
}) {
  const [customer, setCustomer] = useState<Customer | null>(null);

  if (!customer) {
    return <CustomerPicker onPick={setCustomer} onClose={onClose} />;
  }
  return (
    <PaymentDialog
      customerId={customer.id}
      name={customer.name}
      currency={localCurrency}
      localCurrency={localCurrency}
      onDone={onDone}
      onClose={onClose}
    />
  );
}
