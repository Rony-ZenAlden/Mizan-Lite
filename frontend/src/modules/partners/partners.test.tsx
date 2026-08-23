import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { PartnersScreen } from "@/modules/partners/PartnersScreen";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    customers: vi.fn(),
    suppliers: vi.fn(),
    customer: vi.fn(),
    supplier: vi.fn(),
  };
});

const SHOP = {
  id: "pt1", code: "SHOP", name: "Corner Shop", legalName: "", partnerType: "company",
  isCustomer: true, isSupplier: false, taxNumber: "", isTaxExempt: false,
  currency: "SYP", paymentTermsDays: 30, creditLimitMinor: "0",
  phone: "", email: "", isActive: true, hasHistory: false,
};

// The case decision 9 exists for: one identity, both roles.
const MERCHANT = {
  ...SHOP, id: "pt2", code: "MERCHANT", name: "Steel Merchant",
  isCustomer: true, isSupplier: true, paymentTermsDays: 0,
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"], landing: "/",
  });
  vi.mocked(wails.customers).mockResolvedValue([SHOP, MERCHANT]);
  vi.mocked(wails.suppliers).mockResolvedValue([MERCHANT]);
  useSessionStore.getState().setSession({
    ...SIGNED_IN,
    permissions: [wails.PERMISSIONS.customerView, wails.PERMISSIONS.supplierView],
  });
});

afterEach(() => vi.clearAllMocks());

describe("PartnersScreen", () => {
  it("lists customers", async () => {
    renderApp(<PartnersScreen role="customer" />);

    const table = await screen.findByRole("table", { name: /customers/i });
    expect(within(table).getByText("Corner Shop")).toBeInTheDocument();
  });

  // Decision 9 on screen: the merchant we sell brackets to and buy steel from is ONE row that
  // appears in both lists, not two records nobody keeps in step.
  it("shows a partner who is both, in both lists, with both badges", async () => {
    const { unmount } = renderApp(<PartnersScreen role="customer" />);

    let table = await screen.findByRole("table", { name: /customers/i });
    let row = within(table).getByText("Steel Merchant").closest("tr");
    expect(within(row!).getByText("Customer")).toBeInTheDocument();
    expect(within(row!).getByText("Supplier")).toBeInTheDocument();
    unmount();

    renderApp(<PartnersScreen role="supplier" />);
    table = await screen.findByRole("table", { name: /suppliers/i });
    row = within(table).getByText("Steel Merchant").closest("tr");
    expect(row).not.toBeNull();
    expect(within(row!).getByText("Customer")).toBeInTheDocument();
  });

  it("reads Cash rather than 0 days for a partner with no payment terms", async () => {
    renderApp(<PartnersScreen role="customer" />);

    const table = await screen.findByRole("table", { name: /customers/i });
    const merchant = within(table).getByText("Steel Merchant").closest("tr");
    expect(within(merchant!).getByText("Cash")).toBeInTheDocument();

    const shop = within(table).getByText("Corner Shop").closest("tr");
    expect(within(shop!).getByText("30 days")).toBeInTheDocument();
  });

  // Searched on the SERVER, unlike the catalog: a partner list is unbounded, and tax-number
  // search matters at a counter.
  it("passes the search term to the backend", async () => {
    const user = userEvent.setup();
    renderApp(<PartnersScreen role="customer" />);

    await screen.findByRole("table", { name: /customers/i });
    await user.type(screen.getByLabelText(/search by name/i), "9911");

    expect(wails.customers).toHaveBeenCalledWith("9911");
  });

  it("opens a partner's detail when the code is clicked", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.customer).mockResolvedValue({
      partner: SHOP, addresses: [], contacts: [],
      customerRoleLocked: false, supplierRoleLocked: false,
    });

    renderApp(<PartnersScreen role="customer" />);
    const table = await screen.findByRole("table", { name: /customers/i });
    await user.click(within(table).getByRole("button", { name: "SHOP" }));

    expect(await screen.findByRole("heading", { name: "Corner Shop" })).toBeInTheDocument();
    expect(wails.customer).toHaveBeenCalledWith("SHOP");
  });
});

describe("PartnerDetail", () => {
  // Zero means NO LIMIT, not "no credit". Printing "0" would read as the opposite of the truth,
  // and the two readings differ by every sale the business makes.
  it("reads a zero credit limit as no limit", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.customer).mockResolvedValue({
      partner: SHOP, addresses: [], contacts: [],
      customerRoleLocked: false, supplierRoleLocked: false,
    });

    renderApp(<PartnersScreen role="customer" />);
    const table = await screen.findByRole("table", { name: /customers/i });
    await user.click(within(table).getByRole("button", { name: "SHOP" }));

    await screen.findByRole("heading", { name: "Corner Shop" });
    expect(screen.getByText("No limit")).toBeInTheDocument();
    // Not a bare zero anywhere near the credit limit label.
    const limit = screen.getByText("Credit limit").closest("div");
    expect(within(limit!).queryByText("0")).not.toBeInTheDocument();
  });

  it("states a real credit limit with its currency", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.customer).mockResolvedValue({
      partner: { ...SHOP, creditLimitMinor: "500000" }, addresses: [], contacts: [],
      customerRoleLocked: false, supplierRoleLocked: false,
    });

    renderApp(<PartnersScreen role="customer" />);
    const table = await screen.findByRole("table", { name: /customers/i });
    await user.click(within(table).getByRole("button", { name: "SHOP" }));

    expect(await screen.findByText("500000 SYP")).toBeInTheDocument();
  });

  // Explaining rather than offering a toggle that refuses.
  it("explains that a traded role cannot be removed", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.customer).mockResolvedValue({
      partner: MERCHANT, addresses: [], contacts: [],
      customerRoleLocked: false, supplierRoleLocked: true,
    });

    renderApp(<PartnersScreen role="customer" />);
    const table = await screen.findByRole("table", { name: /customers/i });
    await user.click(within(table).getByRole("button", { name: "MERCHANT" }));

    expect(await screen.findByText(/a role cannot be removed/i)).toBeInTheDocument();
    expect(screen.getByText(/their balance sits in the accounts/i)).toBeInTheDocument();
  });

  it("shows addresses and marks the default one", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.customer).mockResolvedValue({
      partner: SHOP,
      addresses: [
        { id: "a1", label: "Head office", addressType: "billing", line1: "1 Main Street", line2: "", city: "Damascus", region: "", postalCode: "", countryCode: "SY", isDefault: true },
        { id: "a2", label: "Branch north", addressType: "shipping", line1: "2 North Road", line2: "", city: "", region: "", postalCode: "", countryCode: "", isDefault: false },
      ],
      contacts: [
        { id: "k1", name: "Layla", role: "Buyer", phone: "", email: "", isPrimary: true },
      ],
      customerRoleLocked: false, supplierRoleLocked: false,
    });

    renderApp(<PartnersScreen role="customer" />);
    const table = await screen.findByRole("table", { name: /customers/i });
    await user.click(within(table).getByRole("button", { name: "SHOP" }));

    const addresses = await screen.findByRole("table", { name: /addresses/i });
    const head = within(addresses).getByText("Head office").closest("tr");
    expect(within(head!).getByText("Default")).toBeInTheDocument();
    expect(within(addresses).getByText("1 Main Street, Damascus")).toBeInTheDocument();

    const contacts = screen.getByRole("table", { name: /contacts/i });
    const layla = within(contacts).getByText("Layla").closest("tr");
    expect(within(layla!).getByText("Main")).toBeInTheDocument();
  });

  it("does not show a credit limit for a pure supplier", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.supplier).mockResolvedValue({
      partner: { ...SHOP, code: "MILL", name: "Steel Mill", isCustomer: false, isSupplier: true },
      addresses: [], contacts: [],
      customerRoleLocked: false, supplierRoleLocked: false,
    });
    vi.mocked(wails.suppliers).mockResolvedValue([
      { ...SHOP, id: "pt3", code: "MILL", name: "Steel Mill", isCustomer: false, isSupplier: true },
    ]);

    renderApp(<PartnersScreen role="supplier" />);
    const table = await screen.findByRole("table", { name: /suppliers/i });
    await user.click(within(table).getByRole("button", { name: "MILL" }));

    await screen.findByRole("heading", { name: "Steel Mill" });
    // A credit limit on a supplier is a field nothing reads; showing it would suggest a control
    // that does not exist.
    expect(screen.queryByText("Credit limit")).not.toBeInTheDocument();
  });
});
