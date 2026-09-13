// The ONLY file that imports the generated bindings (gate G1, enforced by ESLint).
//
// Every method Go binds must be referenced here (gate G2, a Go test), and every function here must be
// called from the application (gate G3, a Vitest over the TypeScript AST). A method on either side with no
// partner fails a build, which is what "zero unconnected bindings" has to mean to hold.
//
// Types are not written by hand: each result type is inferred from the file Wails generated from the Go
// struct, so a field renamed in Go is a compile error here.
import * as App from "../../wailsjs/go/api/App";
import * as Catalog from "../../wailsjs/go/api/Catalog";
import * as Owner from "../../wailsjs/go/api/Owner";
import * as Settings from "../../wailsjs/go/api/Settings";
import * as Stock from "../../wailsjs/go/api/Stock";
import { api } from "../../wailsjs/go/models";
import { unwrap, type Plain } from "./envelope";

export type BootStatus = Plain<api.BootStatusDTO>;
export type Health = Plain<api.HealthDTO>;
export type SettingsState = Plain<api.SettingsDTO>;
export type SettingsInput = Plain<api.SettingsInput>;
export type FirstRunInput = Plain<api.FirstRunInput>;
export type Unit = Plain<api.UnitDTO>;
export type Currency = Plain<api.CurrencyDTO>;
export type Product = Plain<api.ProductDTO>;
export type ProductQuery = Plain<api.ProductQueryDTO>;
export type CreateProductInput = Plain<api.CreateProductInput>;
export type UpdateProductInput = Plain<api.UpdateProductInput>;
export type SetPriceInput = Plain<api.SetPriceInput>;
export type SetActiveInput = Plain<api.SetActiveInput>;
export type SetQuickSlotInput = Plain<api.SetQuickSlotInput>;
export type OwnerStatus = Plain<api.OwnerStatusDTO>;
export type OwnerEvent = Plain<api.OwnerEventDTO>;
export type ChangePINInput = Plain<api.ChangePINInput>;
export type RecoverInput = Plain<api.RecoverInput>;
export type SetPackageInput = Plain<api.SetPackageInput>;
export type StockLevel = Plain<api.StockLevelDTO>;
export type Valuation = Plain<api.ValuationDTO>;
export type ValuationLine = Plain<api.ValuationLineDTO>;
export type Movement = Plain<api.MovementDTO>;
export type History = Plain<api.HistoryDTO>;
export type ReceiveInput = Plain<api.ReceiveInput>;
export type CountInput = Plain<api.CountInput>;
export type AdjustInput = Plain<api.AdjustInput>;
export type OpenPackageInput = Plain<api.OpenPackageInput>;
export type ReverseReceiptInput = Plain<api.ReverseReceiptInput>;
export type CorrectCostInput = Plain<api.CorrectCostInput>;
export type Finding = Plain<api.FindingDTO>;

export function createClient() {
  return {
    app: {
      bootStatus: () => unwrap(App.BootStatus),
      health: () => unwrap(App.Health),
      firstRunStatus: () => unwrap(App.FirstRunStatus),
      completeFirstRun: (input: FirstRunInput) =>
        unwrap(() => App.CompleteFirstRun(api.FirstRunInput.createFrom(input))),
    },
    settings: {
      get: () => unwrap(Settings.Get),
      update: (input: SettingsInput) => unwrap(() => Settings.Update(api.SettingsInput.createFrom(input))),
    },
    catalog: {
      units: () => unwrap(Catalog.Units),
      currencies: () => unwrap(Catalog.Currencies),
      products: (query: ProductQuery) => unwrap(() => Catalog.Products(api.ProductQueryDTO.createFrom(query))),
      product: (id: string) => unwrap(() => Catalog.Product(id)),
      createProduct: (input: CreateProductInput) =>
        unwrap(() => Catalog.CreateProduct(api.CreateProductInput.createFrom(input))),
      updateProduct: (input: UpdateProductInput) =>
        unwrap(() => Catalog.UpdateProduct(api.UpdateProductInput.createFrom(input))),
      setPrice: (input: SetPriceInput) => unwrap(() => Catalog.SetPrice(api.SetPriceInput.createFrom(input))),
      setActive: (input: SetActiveInput) => unwrap(() => Catalog.SetActive(api.SetActiveInput.createFrom(input))),
      setQuickSlot: (input: SetQuickSlotInput) =>
        unwrap(() => Catalog.SetQuickSlot(api.SetQuickSlotInput.createFrom(input))),
      setPackage: (input: SetPackageInput) => unwrap(() => Catalog.SetPackage(api.SetPackageInput.createFrom(input))),
      clearPackage: (productId: string) => unwrap(() => Catalog.ClearPackage(productId)),
    },
    stock: {
      levels: () => unwrap(Stock.Levels),
      valuation: () => unwrap(Stock.Valuation),
      movements: (productId: string, limit: number) =>
        unwrap(() => Stock.Movements(api.MovementsQueryDTO.createFrom({ productId, limit }))),
      receive: (input: ReceiveInput) => unwrap(() => Stock.Receive(api.ReceiveInput.createFrom(input))),
      opening: (input: ReceiveInput) => unwrap(() => Stock.Opening(api.ReceiveInput.createFrom(input))),
      count: (input: CountInput) => unwrap(() => Stock.Count(api.CountInput.createFrom(input))),
      adjust: (input: AdjustInput) => unwrap(() => Stock.Adjust(api.AdjustInput.createFrom(input))),
      openPackage: (input: OpenPackageInput) => unwrap(() => Stock.OpenPackage(api.OpenPackageInput.createFrom(input))),
      reverseReceipt: (input: ReverseReceiptInput) =>
        unwrap(() => Stock.ReverseReceipt(api.ReverseReceiptInput.createFrom(input))),
      correctCost: (input: CorrectCostInput) => unwrap(() => Stock.CorrectCost(api.CorrectCostInput.createFrom(input))),
      verify: () => unwrap(Stock.Verify),
    },
    owner: {
      status: () => unwrap(Owner.Status),
      elevate: (pin: string) => unwrap(() => Owner.Elevate(api.PINInput.createFrom({ pin }))),
      endElevation: () => unwrap(Owner.EndElevation),
      changePin: (input: ChangePINInput) => unwrap(() => Owner.ChangePIN(api.ChangePINInput.createFrom(input))),
      recover: (input: RecoverInput) => unwrap(() => Owner.Recover(api.RecoverInput.createFrom(input))),
      events: (limit: number) => unwrap(() => Owner.Events(limit)),
    },
  };
}

export type Client = ReturnType<typeof createClient>;
