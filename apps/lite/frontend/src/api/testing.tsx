// TEST-ONLY. A fake client and a render helper.
//
// Nothing outside a test may import this file. Two gates hold that: a source gate (no non-test file
// imports it) and G5, which fails if the sentinel appears anywhere in the built bundle.
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { ClientProvider } from "./ClientContext";
import type { Client, Product } from "./client";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Locale } from "@/i18n/messages";
import { OwnerProvider } from "@/owner/OwnerProvider";
import { FAKE_CLIENT_SENTINEL } from "./sentinel";

type Overrides = { [G in keyof Client]?: Partial<Client[G]> };

/** A product for tests, with any field replaceable. */
export function aProduct(overrides: Partial<Product> = {}): Product {
  return {
    id: "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
    nameAr: "زيت زيتون",
    nameEn: "Olive oil",
    barcode: "",
    unitCode: "l",
    priceCurrency: "USD",
    price: "3.25",
    quickSlot: 0,
    active: true,
    rowVersion: 1,
    ...overrides,
  };
}

/** A client answering as a healthy, set-up application, with any method replaceable. */
export function fakeClient(overrides: Overrides = {}): Client {
  const base: Client = {
    app: {
      bootStatus: async () => ({ state: "ready", phase: "done", current: 0, total: 0 }),
      health: async () => ({ version: "1.0.0-test", schemaVersion: 2, platform: "darwin", dataDir: `/data/${FAKE_CLIENT_SENTINEL}` }),
      firstRunStatus: async () => ({ complete: true }),
      completeFirstRun: async () => ({ recoveryCode: "ABCD-EFGH-JKMN-PQRS" }),
    },
    settings: {
      get: async () => ({ locale: "ar", shopName: "بقالية المونة", direction: "rtl" }),
      update: async (input) => ({
        locale: input.locale ?? "ar",
        shopName: input.shopName ?? "بقالية المونة",
        direction: input.locale === "en" ? "ltr" : "rtl",
      }),
    },
    catalog: {
      units: async () => [
        { code: "kg", kind: "mass", inputDecimals: 3 },
        { code: "l", kind: "volume", inputDecimals: 3 },
        { code: "jar", kind: "count", inputDecimals: 0 },
      ],
      currencies: async () => [
        { code: "SYP", decimals: 0 },
        { code: "USD", decimals: 2 },
      ],
      products: async () => [aProduct()],
      product: async () => aProduct(),
      createProduct: async (input) => aProduct({ ...input, id: "new", rowVersion: 1 }),
      updateProduct: async (input) => aProduct({ ...input, rowVersion: input.rowVersion + 1 }),
      setPrice: async (input) => aProduct({ id: input.id, priceCurrency: input.priceCurrency, price: input.price, rowVersion: input.rowVersion + 1 }),
      setActive: async (input) => aProduct({ id: input.id, active: input.active, rowVersion: input.rowVersion + 1 }),
      setQuickSlot: async (input) => aProduct({ id: input.id, quickSlot: input.slot }),
    },
    owner: {
      status: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }),
      elevate: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 120 }),
      endElevation: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }),
      changePin: async () => ({ setUp: true, lockedSeconds: 0, elevatedSeconds: 0 }),
      recover: async () => ({ recoveryCode: "WXYZ-2345-6789-ABCD" }),
      events: async () => [],
    },
  };
  return {
    app: { ...base.app, ...overrides.app },
    settings: { ...base.settings, ...overrides.settings },
    catalog: { ...base.catalog, ...overrides.catalog },
    owner: { ...base.owner, ...overrides.owner },
  };
}

/** Renders inside the providers the application has, in a chosen locale and route. */
export function renderWithProviders(
  ui: ReactElement,
  { client = fakeClient(), locale = "ar", route = "/" }: { client?: Client; locale?: Locale; route?: string } = {},
) {
  return render(
    <ClientProvider client={client}>
      <LocaleProvider initial={locale}>
        <OwnerProvider>
          <MemoryRouter initialEntries={[route]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
            {ui}
          </MemoryRouter>
        </OwnerProvider>
      </LocaleProvider>
    </ClientProvider>,
  );
}
