import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "@/app/session/session";
import { CatalogScreen } from "@/modules/catalog/CatalogScreen";
import { renderApp, SIGNED_IN } from "@/test/appRender";
import * as wails from "@/lib/wails";

vi.mock("@/lib/wails", async () => {
  const actual = await vi.importActual<typeof wails>("@/lib/wails");
  return {
    ...actual,
    preferences: vi.fn(),
    productCategories: vi.fn(),
    products: vi.fn(),
    product: vi.fn(),
  };
});

const CEMENT = {
  id: "p1", code: "CEMENT", name: "Bag of cement", nameKey: "", categoryCode: "BUILD",
  type: "goods", stockUnit: "PCS", tracking: "quantity", variantCount: 1, isActive: true,
};

const SHIRT = {
  id: "p2", code: "SHIRT", name: "Shirt", nameKey: "", categoryCode: "CLOTH",
  type: "goods", stockUnit: "PCS", tracking: "quantity", variantCount: 4, isActive: true,
};

beforeEach(() => {
  vi.mocked(wails.preferences).mockResolvedValue({
    locale: "en", theme: "system", availableLocales: ["en", "ar"],
  });
  vi.mocked(wails.productCategories).mockResolvedValue([
    { id: "c1", code: "BUILD", name: "Building", nameKey: "", path: "/BUILD/", depth: 0, isActive: true },
    { id: "c2", code: "CLOTH", name: "Clothing", nameKey: "", path: "/CLOTH/", depth: 0, isActive: true },
  ]);
  vi.mocked(wails.products).mockResolvedValue([CEMENT, SHIRT]);
  useSessionStore.getState().setSession({
    ...SIGNED_IN, permissions: [wails.PERMISSIONS.catalogView],
  });
});

afterEach(() => vi.clearAllMocks());

describe("CatalogScreen", () => {
  it("lists the products with their stock unit", async () => {
    renderApp(<CatalogScreen />);

    const table = await screen.findByRole("table", { name: /products/i });
    expect(within(table).getByText("CEMENT")).toBeInTheDocument();
    expect(within(table).getByText("Shirt")).toBeInTheDocument();
  });

  // §A.1 made invisible: every product has a default variant, and a shop selling bags of cement
  // must never encounter the word. Printing "1" here would expose the mechanism.
  it("shows a dash rather than 1 for a simple product's variant count", async () => {
    renderApp(<CatalogScreen />);

    const table = await screen.findByRole("table", { name: /products/i });
    const cementRow = within(table).getByText("CEMENT").closest("tr");
    expect(cementRow).not.toBeNull();
    expect(within(cementRow!).getByText("—")).toBeInTheDocument();

    // A product with real variants states the number.
    const shirtRow = within(table).getByText("SHIRT").closest("tr");
    expect(within(shirtRow!).getByText("4")).toBeInTheDocument();
  });

  it("filters the list by category", async () => {
    const user = userEvent.setup();
    renderApp(<CatalogScreen />);

    const table = await screen.findByRole("table", { name: /products/i });
    expect(within(table).getByText("CEMENT")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Clothing" }));

    expect(within(table).queryByText("CEMENT")).not.toBeInTheDocument();
    expect(within(table).getByText("SHIRT")).toBeInTheDocument();
  });

  it("filters the list by search term", async () => {
    const user = userEvent.setup();
    renderApp(<CatalogScreen />);

    await screen.findByRole("table", { name: /products/i });
    await user.type(screen.getByLabelText(/search products/i), "shirt");

    const table = screen.getByRole("table", { name: /products/i });
    expect(within(table).queryByText("CEMENT")).not.toBeInTheDocument();
    expect(within(table).getByText("SHIRT")).toBeInTheDocument();
  });

  // The whole reason the detail is a separate screen: the list is a list.
  it("opens a product's detail when its code is clicked", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.product).mockResolvedValue({
      product: CEMENT, salesUnit: "PCS", purchaseUnit: "PCS", stockUnitLocked: false,
      variants: [{
        id: "v1", sku: "CEMENT", name: "", combination: "",
        isDefault: true, hasHistory: false, isActive: true,
      }],
      attributes: [], isSimple: true,
    });

    renderApp(<CatalogScreen />);
    const table = await screen.findByRole("table", { name: /products/i });
    await user.click(within(table).getByRole("button", { name: "CEMENT" }));

    expect(await screen.findByRole("heading", { name: "Bag of cement" })).toBeInTheDocument();
    expect(wails.product).toHaveBeenCalledWith("CEMENT");
  });
});

describe("ProductDetail", () => {
  it("hides the variants section entirely for a simple product", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.product).mockResolvedValue({
      product: CEMENT, salesUnit: "PCS", purchaseUnit: "PCS", stockUnitLocked: false,
      variants: [{
        id: "v1", sku: "CEMENT", name: "", combination: "",
        isDefault: true, hasHistory: false, isActive: true,
      }],
      attributes: [], isSimple: true,
    });

    renderApp(<CatalogScreen />);
    const table = await screen.findByRole("table", { name: /products/i });
    await user.click(within(table).getByRole("button", { name: "CEMENT" }));

    await screen.findByRole("heading", { name: "Bag of cement" });

    // Asserted on the HEADING, not on the table.
    //
    // The first version of this test checked for the absence of a table named "Variants" — and
    // the mutation drill for the isSimple guard PASSED, because the row filter empties the list
    // anyway and Table renders an EmptyState rather than a <table> when it has no rows. The test
    // was passing on the filter while claiming to pin the guard. The heading renders
    // unconditionally inside the guarded block, so only the guard can remove it.
    //
    // Same lesson as 3.1, 3.3, and 3.5, now on the frontend: when two mechanisms produce the
    // same visible outcome, the test must name the one it is about.
    expect(screen.queryByRole("heading", { name: "Variants" })).not.toBeInTheDocument();
    expect(screen.queryByRole("table", { name: /variants/i })).not.toBeInTheDocument();
  });

  it("shows the variants of a product that has real ones, and renders the combination readably", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.product).mockResolvedValue({
      product: SHIRT, salesUnit: "PCS", purchaseUnit: "PCS", stockUnitLocked: false,
      variants: [
        { id: "v0", sku: "SHIRT", name: "", combination: "", isDefault: true, hasHistory: false, isActive: true },
        { id: "v1", sku: "SHIRT-RED-L", name: "", combination: "COLOUR:RED|SIZE:L", isDefault: false, hasHistory: false, isActive: true },
        { id: "v2", sku: "SHIRT-BLUE-S", name: "", combination: "COLOUR:BLUE|SIZE:S", isDefault: false, hasHistory: true, isActive: false },
      ],
      attributes: [{
        code: "COLOUR", name: "Colour", nameKey: "", isVariantDefining: true,
        values: [
          { code: "RED", name: "Red", nameKey: "", displayHint: "" },
          { code: "BLUE", name: "Blue", nameKey: "", displayHint: "" },
        ],
      }],
      isSimple: false,
    });

    renderApp(<CatalogScreen />);
    const table = await screen.findByRole("table", { name: /products/i });
    await user.click(within(table).getByRole("button", { name: "SHIRT" }));

    const variants = await screen.findByRole("table", { name: /variants/i });
    expect(within(variants).getByText("SHIRT-RED-L")).toBeInTheDocument();

    // 'COLOUR:RED|SIZE:L' is the backend's matching key, not something a person reads.
    expect(within(variants).getByText("RED · L")).toBeInTheDocument();
    expect(within(variants).queryByText(/COLOUR:/)).not.toBeInTheDocument();

    // The default with no combination is still not listed, even here.
    expect(within(variants).queryByText("SHIRT", { selector: "span" })).not.toBeInTheDocument();

    // A retired variant reads as retired rather than simply vanishing.
    expect(within(variants).getByText("Retired")).toBeInTheDocument();
  });

  // §A.2 on screen: the flag lives on the link, so the same dictionary attribute reads
  // differently for different products.
  it("labels an attribute as defining variants or as a specification", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.product).mockResolvedValue({
      product: SHIRT, salesUnit: "PCS", purchaseUnit: "PCS", stockUnitLocked: false,
      variants: [
        { id: "v0", sku: "SHIRT", name: "", combination: "", isDefault: true, hasHistory: false, isActive: true },
        { id: "v1", sku: "SHIRT-RED", name: "", combination: "COLOUR:RED", isDefault: false, hasHistory: false, isActive: true },
      ],
      attributes: [
        { code: "COLOUR", name: "Colour", nameKey: "", isVariantDefining: true, values: [] },
        { code: "MATERIAL", name: "Material", nameKey: "", isVariantDefining: false, values: [] },
      ],
      isSimple: false,
    });

    renderApp(<CatalogScreen />);
    const table = await screen.findByRole("table", { name: /products/i });
    await user.click(within(table).getByRole("button", { name: "SHIRT" }));

    const attributes = await screen.findByRole("table", { name: /attributes/i });
    const colour = within(attributes).getByText("Colour").closest("tr");
    const material = within(attributes).getByText("Material").closest("tr");
    expect(within(colour!).getByText("Defines variants")).toBeInTheDocument();
    expect(within(material!).getByText("Specification")).toBeInTheDocument();
  });

  // Explaining the lock rather than offering a control that always refuses.
  it("explains why a stock unit cannot be changed once stock has moved", async () => {
    const user = userEvent.setup();
    vi.mocked(wails.product).mockResolvedValue({
      product: CEMENT, salesUnit: "PCS", purchaseUnit: "PCS", stockUnitLocked: true,
      variants: [{
        id: "v1", sku: "CEMENT", name: "", combination: "",
        isDefault: true, hasHistory: true, isActive: true,
      }],
      attributes: [], isSimple: true,
    });

    renderApp(<CatalogScreen />);
    const table = await screen.findByRole("table", { name: /products/i });
    await user.click(within(table).getByRole("button", { name: "CEMENT" }));

    expect(await screen.findByText(/the stock unit is fixed/i)).toBeInTheDocument();
    expect(screen.getByText(/would restate every past quantity/i)).toBeInTheDocument();
  });
});
